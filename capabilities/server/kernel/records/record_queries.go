package records

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// find selects records of a type; visible, when set, is the caller's scope.
func (s *Store) Find(et *Type, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, int, *kernel.Error) {
	order, err := CompileOrder(et.info, q.Sort)
	if err != nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	collector := order.Page(q.Offset, q.Limit)
	if refusal := s.VisitMatching(et, q, visible, collector.Add); refusal != nil {
		return nil, 0, refusal
	}
	page, total := collector.Result()
	return page, total, nil
}

func (s *Store) MatchingQuery(et *Type, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, *kernel.Error) {
	var out []reflect.Value
	refusal := s.VisitMatching(et, q, visible, func(value reflect.Value) { out = append(out, value) })
	return out, refusal
}

func (s *Store) VisitMatching(et *Type, q platform.Query, visible func(reflect.Value) bool, accept func(reflect.Value)) *kernel.Error {

	if err := q.CheckSet(); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	match, err := CompileSet(et.info, platform.RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set})
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	effective, refusal := s.EffectivePredicate(et, q)
	if refusal != nil {
		return refusal
	}

	image := et.IndexImage()
	image.Current = func() *CurrentScope { return s.current }
	writes := func(yield func(string) bool) {
		for overlay := s; overlay != nil; overlay = overlay.parent {
			for ref := range overlay.writes {
				typ, id, ok := strings.Cut(ref, "/")
				if ok && typ == et.info.Type && !yield(id) {
					return
				}
			}
		}
	}
	VisitMatching(image, s.IndexImage, q, visible, effective, match, writes, accept)

	return nil
}

// AggregateQuery groups and measures the records of one type,
// like Odoo's read_group: the list's domain, search and scope, then groups and
// measures.
//   - Groups: a field ("stage"), or a date's bucket ("checkIn:month"; day, week,
//     month, year), also on the stamps "created" and "changed" and on text
//     that starts with a date.
//   - Measures: "count", or "sum:", "avg:", "min:", "max:" with a number or
//     money field. Money is summed per currency: its currency becomes a group
//     ("amount.currency"), and amounts stay in minor units.
type AggregateQuery struct {
	BusinessTime *platform.BusinessTime  `json:"businessTime,omitempty"`
	Collection   *platform.RecordSetRef  `json:"collection,omitempty"`
	Window       *AggregateWindowQuery   `json:"window,omitempty"`
	Histogram    *HistogramQuery         `json:"histogram,omitempty"`
	MaxRows      int                     `json:"maxRows,omitempty"`
	Traversal    *platform.LinkTraversal `json:"traversal,omitempty"`
	Set          *platform.QuerySet      `json:"set,omitempty"`
	Domain       json.RawMessage         `json:"domain,omitempty"`
	Search       string                  `json:"search,omitempty"`
	Archived     bool                    `json:"archived,omitempty"`
	Groups       []string                `json:"groups,omitempty"`
	Measures     []string                `json:"measures,omitempty"`
}

const AggregateSetMaxGroups = 4

const AggregateSetMaxMeasures = 8

const AggregateSetMaxRows = 4096

// Aggregate is the answer: the columns it has, then one row per group, keyed
// by column name, in the order of the groups' values.
type Aggregate struct {
	Partial   bool                   `json:"partial,omitempty"`
	Window    *AggregateWindowResult `json:"window,omitempty"`
	Histogram *HistogramResult       `json:"histogram,omitempty"`
	Columns   []Column               `json:"columns"`
	Rows      []map[string]any       `json:"rows"`
}

// Column describes a group or a measure, so a chart or pivot knows its type:
// nominal (text, choices, references, booleans), temporal (buckets of dates),
// quantitative (measures).
type Column struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // group, measure
	Type  string `json:"type"` // nominal, temporal, quantitative
	Field string `json:"field,omitempty"`
	Money bool   `json:"money,omitempty"`
}

type grouping struct {
	column Column
	key    func(reflect.Value) any
}

type measure struct {
	column Column
	op     string
	field  platform.FieldInfo
	kind   string
}

var buckets = map[string]func(time.Time) string{
	"day":   func(t time.Time) string { return t.Format(time.DateOnly) },
	"week":  func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%04d-W%02d", y, w) },
	"month": func(t time.Time) string { return t.Format("2006-01") },
	"year":  func(t time.Time) string { return t.Format("2006") },
}

func aggregateGroupValue(v reflect.Value) any {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}

func (et *Type) Grouping(name string) (grouping, bool) {
	field, bucket, bucketed := strings.Cut(name, ":")
	date := func(v reflect.Value) (time.Time, bool) { return time.Time{}, false }
	col := Column{Name: name, Kind: "group", Type: "nominal", Field: field}
	switch field {
	case "created", "changed":
		col.Title = map[string]string{"created": "Created", "changed": "Changed"}[field]
		date = func(v reflect.Value) (time.Time, bool) {
			s := Header(v).Created
			if field == "changed" {
				s = Header(v).Changed
			}
			return s.At, !s.At.IsZero()
		}
		if !bucketed {
			bucket, bucketed = "day", true
		}
	default:
		f, ok := et.info.Field(field)
		if !ok {
			return grouping{}, false
		}
		col.Title = f.Title
		switch f.Type {
		case "date", "datetime", "text":
			if f.Type == "text" && !bucketed {
				return grouping{column: col, key: func(v reflect.Value) any { return aggregateGroupValue(v.FieldByIndex(f.Index)) }}, true
			}
			date = func(v reflect.Value) (time.Time, bool) { // text buckets by the date it starts with ("2026-10-01T09:00")
				switch x := aggregateGroupValue(v.FieldByIndex(f.Index)).(type) {
				case time.Time:
					return x, !x.IsZero()
				case string:
					t, err := time.Parse(time.DateOnly, x[:min(len(x), len(time.DateOnly))])
					return t, err == nil
				}
				return time.Time{}, false
			}
			if !bucketed {
				bucket, bucketed = "day", true
			}
		case "choice", "reference", "boolean", "integer":
			if bucketed {
				return grouping{}, false
			}
			return grouping{column: col, key: func(v reflect.Value) any { return aggregateGroupValue(v.FieldByIndex(f.Index)) }}, true
		default:
			return grouping{}, false
		}
	}
	format, ok := buckets[bucket]
	if !ok {
		return grouping{}, false
	}
	col.Type = "temporal"
	if col.Title != "" {
		col.Title += " (" + bucket + ")"
	}
	return grouping{column: col, key: func(v reflect.Value) any {
		if t, ok := date(v); ok {
			return format(t)
		}
		return ""
	}}, true
}

func (et *Type) Measure(name string) (measure, bool) {
	if name == "count" {
		return measure{column: Column{Name: name, Title: "Count", Kind: "measure", Type: "quantitative"}, op: "count"}, true
	}
	op, field, _ := strings.Cut(name, ":")
	f, ok := et.info.Field(field)
	if !ok || !slices.Contains([]string{"sum", "avg", "min", "max"}, op) || !slices.Contains([]string{"integer", "decimal", "number", "money"}, f.Type) {
		return measure{}, false
	}
	col := Column{Name: name, Title: fmt.Sprintf("%s (%s)", f.Title, op), Kind: "measure", Type: "quantitative", Field: field, Money: f.Type == "money"}
	kind := platform.RuleFieldKind(f.Type)
	if f.Type == "money" {
		kind = "integer"
	} // The original group retains currency; measures expose minor units.
	if op == "avg" {
		kind = "decimal"
	}
	return measure{column: col, op: op, field: f, kind: kind}, true
}

// aggregate groups and measures the matching records; the caller holds s.mu.
func (s *Store) Aggregate(et *Type, q AggregateQuery, visible func(reflect.Value) bool, selected ...[]reflect.Value) (Aggregate, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if q.MaxRows < 0 || q.MaxRows > AggregateSetMaxRows {
		return Aggregate{}, invalid
	}
	if q.Window != nil {
		return s.AggregateWindow(et, q, visible)
	}
	if q.Histogram != nil {
		return s.Histogram(et, q, visible)
	}
	maxRows := q.MaxRows
	if q.Set != nil && (maxRows == 0 || maxRows > AggregateSetMaxRows) {
		maxRows = AggregateSetMaxRows
	}
	if q.Set != nil && (len(q.Groups) > AggregateSetMaxGroups || len(q.Measures) > AggregateSetMaxMeasures) {
		return Aggregate{}, invalid
	}
	if len(q.Measures) == 0 {
		q.Measures = []string{"count"}
	}
	var groups []grouping
	for _, name := range q.Groups {
		g, ok := et.Grouping(name)
		if !ok {
			return Aggregate{}, invalid
		}
		groups = append(groups, g)
	}
	var measures []measure
	currencies := map[string]bool{} // money fields: their currency is a group
	for _, name := range q.Measures {
		m, ok := et.Measure(name)
		if !ok {
			return Aggregate{}, invalid
		}
		measures = append(measures, m)
		if m.column.Money && !currencies[m.column.Field] {
			currencies[m.column.Field] = true
			f, _ := et.info.Field(m.column.Field)
			groups = append(groups, grouping{column: Column{Name: f.Name + ".currency", Title: f.Title + " currency", Kind: "group", Type: "nominal", Field: f.Name},
				key: func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface().(platform.Money).Currency }})
		}
	}
	if q.Set != nil && len(groups) > AggregateSetMaxGroups {
		return Aggregate{}, invalid
	}
	var rows []reflect.Value
	if len(selected) > 0 {
		rows = selected[0]
	} else {
		var err *kernel.Error
		rows, err = s.MatchingQuery(et, platform.Query{Set: q.Set, Domain: q.Domain, Search: q.Search, Archived: q.Archived, BusinessTime: q.BusinessTime}, visible)
		if err != nil {
			return Aggregate{}, err
		}
	}
	type acc struct {
		keys  []any
		folds []platform.ExprNumericAggregate
	}
	byKey := map[string]*acc{}
	var order []*acc
	// Exact arithmetic is deterministic; sorted identities also make budget
	// refusal independent of the record store's map iteration order.
	slices.SortFunc(rows, func(a, b reflect.Value) int { return strings.Compare(Header(a).ID, Header(b).ID) })
	for _, value := range rows {
		keys := make([]any, len(groups))
		for i, group := range groups {
			keys[i] = group.key(value)
		}
		encoded, err := json.Marshal(keys)
		if err != nil {
			return Aggregate{}, invalid
		}
		key := string(encoded)
		group := byKey[key]
		if group == nil {
			if maxRows > 0 && len(order) >= maxRows {
				return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Aggregate has {count} groups; limit is {limit}", len(order)+1, maxRows)
			}
			group = &acc{keys: keys, folds: make([]platform.ExprNumericAggregate, len(measures))}
			for i, measure := range measures {
				kind := measure.kind
				if measure.op == "count" {
					kind = "integer"
				}
				group.folds[i], err = s.rules.NewExprNumericAggregate(measure.op, kind, "")
				if err != nil {
					return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
			}
			byKey[key] = group
			order = append(order, group)
		}
		for i, measure := range measures {
			input := platform.ExprValue{}
			if measure.op != "count" {
				operand := aggregateGroupValue(value.FieldByIndex(measure.field.Index))
				if measure.field.Type == "money" {
					if money, ok := operand.(platform.Money); ok {
						operand = money.Amount
					}
				}
				kind := platform.RuleFieldKind(measure.field.Type)
				if measure.field.Type == "money" {
					kind = "integer"
				}
				input = platform.ExprPresent(kind, operand)
			}
			if err := group.folds[i].Add(input); err != nil {
				return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Aggregate {measure}: {reason}", measure.column.Name, err.Error())
			}
		}
	}
	slices.SortFunc(order, func(a, b *acc) int {
		for i := range a.keys {
			if c := CompareValues(Comparable(a.keys[i]), Comparable(b.keys[i])); c != 0 {
				return c
			}
		}
		return 0
	})
	out := Aggregate{Rows: make([]map[string]any, 0, len(order))}
	for _, g := range groups {
		out.Columns = append(out.Columns, g.column)
	}
	for _, m := range measures {
		out.Columns = append(out.Columns, m.column)
	}
	for _, a := range order {
		row := map[string]any{}
		for i, g := range groups {
			row[g.column.Name] = a.keys[i]
		}
		for i, measure := range measures {
			result, err := a.folds[i].Result()
			if err != nil {
				return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Aggregate {measure}: {reason}", measure.column.Name, err.Error())
			}
			value, err := platform.ExprGoValue(result)
			if err != nil {
				return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if measure.op == "count" {
				number, ok := value.(json.Number)
				if !ok {
					return Aggregate{}, invalid
				}
				count, err := strconv.Atoi(number.String())
				if err != nil {
					return Aggregate{}, invalid
				}
				value = count
			}
			row[measure.column.Name] = value
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

const HistogramMaxBins = 64

type HistogramQuery struct {
	Field string `json:"field"`
	Bins  int    `json:"bins"`
}

type HistogramBucket struct {
	Lower          string `json:"lower"`
	Upper          string `json:"upper"`
	UpperInclusive bool   `json:"upperInclusive"`
	Count          int    `json:"count"`
}

type HistogramResult struct {
	Field         string            `json:"field"`
	RequestedBins int               `json:"requestedBins"`
	Valid         int               `json:"valid"`
	Missing       int               `json:"missing"`
	Minimum       string            `json:"minimum"`
	Maximum       string            `json:"maximum"`
	Buckets       []HistogramBucket `json:"buckets"`
}

func histogramNumber(v reflect.Value) (*big.Rat, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, true
		}
		v = v.Elem()
	}
	var text string
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		text = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		text = strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		x := v.Float()
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		text = strconv.FormatFloat(x, 'g', -1, v.Type().Bits())
	default:
		return nil, false
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}

func histogramBound(value *big.Rat) string {
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	a, b := 0, 0
	for new(big.Int).Mod(denominator, two).Sign() == 0 {
		denominator.Div(denominator, two)
		a++
	}
	for new(big.Int).Mod(denominator, five).Sign() == 0 {
		denominator.Div(denominator, five)
		b++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return value.RatString()
	}
	text := value.FloatString(max(a, b))
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}

func (s *Store) Histogram(et *Type, q AggregateQuery, visible func(reflect.Value) bool) (Aggregate, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	h := q.Histogram
	field, ok := et.info.Field(h.Field)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, field.Type) || h.Bins < 1 || h.Bins > HistogramMaxBins || len(q.Groups) > 0 || len(q.Measures) > 0 && !slices.Equal(q.Measures, []string{"count"}) || q.MaxRows > 0 && q.MaxRows < h.Bins {
		return Aggregate{}, invalid
	}
	rows, err := s.MatchingQuery(et, platform.Query{Domain: q.Domain, Search: q.Search, Set: q.Set, Archived: q.Archived, BusinessTime: q.BusinessTime}, visible)
	if err != nil {
		return Aggregate{}, err
	}
	result := &HistogramResult{Field: h.Field, RequestedBins: h.Bins, Buckets: []HistogramBucket{}}
	values := make([]*big.Rat, 0, len(rows))
	var minimum, maximum *big.Rat
	for _, row := range rows {
		value, ok := histogramNumber(row.FieldByIndex(field.Index))
		if !ok {
			return Aggregate{}, invalid
		}
		if value == nil {
			result.Missing++
			continue
		}
		values = append(values, value)
		if minimum == nil || value.Cmp(minimum) < 0 {
			minimum = new(big.Rat).Set(value)
		}
		if maximum == nil || value.Cmp(maximum) > 0 {
			maximum = new(big.Rat).Set(value)
		}
	}
	result.Valid = len(values)
	out := Aggregate{Columns: []Column{}, Rows: []map[string]any{}, Histogram: result}
	if minimum == nil {
		return out, nil
	}
	result.Minimum = histogramBound(minimum)
	result.Maximum = histogramBound(maximum)
	if minimum.Cmp(maximum) == 0 {
		result.Buckets = []HistogramBucket{{Lower: result.Minimum, Upper: result.Maximum, UpperInclusive: true, Count: len(values)}}
		return out, nil
	}
	span := new(big.Rat).Sub(maximum, minimum)
	step := new(big.Rat).Quo(span, new(big.Rat).SetInt64(int64(h.Bins)))
	edges := make([]*big.Rat, h.Bins+1)
	for i := range edges {
		edges[i] = new(big.Rat).Add(minimum, new(big.Rat).Mul(step, new(big.Rat).SetInt64(int64(i))))
	}
	result.Buckets = make([]HistogramBucket, h.Bins)
	for i := range result.Buckets {
		result.Buckets[i] = HistogramBucket{Lower: histogramBound(edges[i]), Upper: histogramBound(edges[i+1]), UpperInclusive: i == h.Bins-1}
	}
	for _, value := range values {
		ratio := new(big.Rat).Quo(new(big.Rat).Sub(value, minimum), step)
		index := new(big.Int).Quo(ratio.Num(), ratio.Denom()).Int64()
		if index == int64(h.Bins) {
			index--
		}
		if index < 0 || index >= int64(h.Bins) {
			return Aggregate{}, invalid
		}
		result.Buckets[index].Count++
	}
	return out, nil
}

const AggregateWindowMaxRows = 100000

type AggregateWindowQuery struct {
	TimeField string `json:"timeField"`
	Field     string `json:"field"`
	Rows      int    `json:"rows"`
	Threshold string `json:"threshold"`
}

type AggregateWindowEdge struct {
	ID       string    `json:"id"`
	Revision uint32    `json:"revision"`
	Time     time.Time `json:"time"`
}

type AggregateWindowResult struct {
	TimeField     string               `json:"timeField"`
	Field         string               `json:"field"`
	RequestedRows int                  `json:"requestedRows"`
	Threshold     string               `json:"threshold"`
	Generation    string               `json:"generation"`
	Total         int                  `json:"total"`
	Timed         int                  `json:"timed"`
	MissingTime   int                  `json:"missingTime"`
	Count         int                  `json:"count"`
	Valid         int                  `json:"valid"`
	Missing       int                  `json:"missing"`
	Above         int                  `json:"above"`
	Min           *float64             `json:"min,omitempty"`
	Mean          *float64             `json:"mean,omitempty"`
	Max           *float64             `json:"max,omitempty"`
	First         *AggregateWindowEdge `json:"first,omitempty"`
	Last          *AggregateWindowEdge `json:"last,omitempty"`
}

var windowDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func windowThreshold(text string) (*big.Rat, bool) {
	if len(text) < 1 || len(text) > 80 || !windowDecimal.MatchString(text) {
		return nil, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, false
	}
	// Check the exponent before constructing a rational with an unbounded denominator.
	if _, exponent, ok := strings.Cut(strings.ToLower(text), "e"); ok {
		n, err := strconv.Atoi(exponent)
		if err != nil || n < -324 || n > 308 {
			return nil, false
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || number == 0 && value.Sign() != 0 {
		return nil, false
	}
	return value, true
}

func windowTime(value reflect.Value) (time.Time, bool, bool) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return time.Time{}, false, true
		}
		value = value.Elem()
	}
	instant, ok := value.Interface().(time.Time)
	if !ok {
		return time.Time{}, false, false
	}
	return instant, true, true
}

// The original member projection/predicate compiler has already run. The caller holds s.mu.
func (s *Store) AggregateWindow(et *Type, q AggregateQuery, visible func(reflect.Value) bool) (Aggregate, *kernel.Error) {
	invalid := func(message string) (Aggregate, *kernel.Error) {
		return Aggregate{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: message}
	}
	w := q.Window
	when, hasTime := et.info.Field(w.TimeField)
	field, hasField := et.info.Field(w.Field)
	threshold, valid := windowThreshold(w.Threshold)
	if !hasTime || when.Type != "datetime" || !hasField || !slices.Contains([]string{"integer", "decimal"}, field.Type) || w.Rows < 1 || w.Rows > AggregateWindowMaxRows || !valid || len(q.Groups) > 0 || len(q.Measures) > 0 || q.Histogram != nil || q.MaxRows != 0 {
		return invalid("Window statistics need readable event time and numeric fields, a bounded row count and decimal threshold; other aggregate modes cannot be mixed.")
	}
	rows, issue := s.MatchingQuery(et, platform.Query{Domain: q.Domain, Search: q.Search, Set: q.Set, Archived: q.Archived, BusinessTime: q.BusinessTime}, visible)
	if issue != nil {
		return Aggregate{}, issue
	}
	type timedRow struct {
		value reflect.Value
		at    time.Time
		id    string
	}
	timed := make([]timedRow, 0, len(rows))
	out := &AggregateWindowResult{TimeField: w.TimeField, Field: w.Field, RequestedRows: w.Rows, Threshold: w.Threshold, Generation: strconv.FormatUint(s.generation, 10), Total: len(rows)}
	for _, row := range rows {
		at, hasTime, valid := windowTime(row.FieldByIndex(when.Index))
		if !valid {
			return invalid("The original event time has an incompatible runtime type.")
		}
		if !hasTime {
			out.MissingTime++
			continue
		}
		timed = append(timed, timedRow{row, at, Header(row).ID})
	}
	slices.SortFunc(timed, func(a, b timedRow) int {
		if a.at.Before(b.at) {
			return 1
		}
		if a.at.After(b.at) {
			return -1
		}
		return strings.Compare(a.id, b.id)
	})
	out.Timed = len(timed)
	timed = timed[:min(len(timed), w.Rows)]
	out.Count = len(timed)
	edge := func(row timedRow) *AggregateWindowEdge {
		record := Header(row.value)
		return &AggregateWindowEdge{ID: record.ID, Revision: record.Revision, Time: row.at}
	}
	if len(timed) > 0 {
		out.First = edge(timed[0])
		out.Last = edge(timed[len(timed)-1])
	}
	sum := new(big.Rat)
	var minimum, maximum *big.Rat
	safe := new(big.Rat).SetInt64(9007199254740991)
	for _, row := range timed {
		value, valid := histogramNumber(row.value.FieldByIndex(field.Index))
		if !valid {
			return invalid("The original numeric window has an incompatible or non-finite value.")
		}
		if value == nil {
			out.Missing++
			continue
		}
		if field.Type == "integer" && (!value.IsInt() || new(big.Rat).Abs(value).Cmp(safe) > 0) {
			return invalid("Window integer values exceed the exact JSON number range.")
		}
		out.Valid++
		if value.Cmp(threshold) > 0 {
			out.Above++
		}
		sum.Add(sum, value)
		if minimum == nil || value.Cmp(minimum) < 0 {
			minimum = new(big.Rat).Set(value)
		}
		if maximum == nil || value.Cmp(maximum) > 0 {
			maximum = new(big.Rat).Set(value)
		}
	}
	if out.Valid > 0 {
		mean := new(big.Rat).Quo(sum, new(big.Rat).SetInt64(int64(out.Valid)))
		low, _ := minimum.Float64()
		avg, _ := mean.Float64()
		high, _ := maximum.Float64()
		if math.IsInf(low, 0) || math.IsInf(avg, 0) || math.IsInf(high, 0) || math.IsNaN(avg) {
			return invalid("Window statistics cannot be represented as finite JSON numbers.")
		}
		out.Min = &low
		out.Mean = &avg
		out.Max = &high
	}
	return Aggregate{Window: out, Columns: []Column{}, Rows: []map[string]any{}}, nil
}

func BusinessField(info platform.EntityInfo, value reflect.Value, name string) string {
	field, ok := info.Field(name)
	if !ok {
		return ""
	}
	v := value.FieldByIndex(field.Index)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).Format(time.RFC3339Nano)
	}
	return fmt.Sprint(v.Interface())
}

func (s *Store) EffectivePredicate(et *Type, q platform.Query) (func(reflect.Value) bool, *kernel.Error) {
	v := et.info.Versioning
	if q.BusinessTime != nil && q.BusinessTime.All && q.BusinessTime.At != "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business time selects either an instant or all versions")
	}
	if v == nil || q.BusinessTime != nil && q.BusinessTime.All {
		return nil, nil
	}
	if q.BusinessTime == nil || q.BusinessTime.At == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business time must be captured before reading versioned data")
	}
	info := s.types[et.info.Type].info
	field, _ := info.Field(v.ValidFrom)
	if _, err := platform.EffectiveAt(field.Type, q.BusinessTime.At, "", q.BusinessTime.At); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return func(value reflect.Value) bool {
		active, err := platform.EffectiveAt(field.Type, BusinessField(info, value, v.ValidFrom), BusinessField(info, value, v.ValidUntil), q.BusinessTime.At)
		return err == nil && active
	}, nil
}
