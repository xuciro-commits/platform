package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// AggregateQuery groups and measures the records of one type (ADR-0019 D1),
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

func (et *entityType) grouping(name string) (grouping, bool) {
	field, bucket, bucketed := strings.Cut(name, ":")
	date := func(v reflect.Value) (time.Time, bool) { return time.Time{}, false }
	col := Column{Name: name, Kind: "group", Type: "nominal", Field: field}
	switch field {
	case "created", "changed":
		col.Title = map[string]string{"created": "Created", "changed": "Changed"}[field]
		date = func(v reflect.Value) (time.Time, bool) {
			s := recordOf(v).Created
			if field == "changed" {
				s = recordOf(v).Changed
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

func (et *entityType) measure(name string) (measure, bool) {
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
func (s *recordStore) aggregate(et *entityType, q AggregateQuery, visible func(reflect.Value) bool, selected ...[]reflect.Value) (Aggregate, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if q.MaxRows < 0 || q.MaxRows > AggregateSetMaxRows {
		return Aggregate{}, invalid
	}
	if q.Window != nil {
		return s.aggregateWindow(et, q, visible)
	}
	if q.Histogram != nil {
		return s.histogram(et, q, visible)
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
		g, ok := et.grouping(name)
		if !ok {
			return Aggregate{}, invalid
		}
		groups = append(groups, g)
	}
	var measures []measure
	currencies := map[string]bool{} // money fields: their currency is a group
	for _, name := range q.Measures {
		m, ok := et.measure(name)
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
		rows, err = s.matchingQuery(et, platform.Query{Set: q.Set, Domain: q.Domain, Search: q.Search, Archived: q.Archived, BusinessTime: q.BusinessTime}, visible)
		if err != nil {
			return Aggregate{}, err
		}
	}
	type acc struct {
		keys  []any
		folds []*platform.ExprNumericAggregate
	}
	byKey := map[string]*acc{}
	var order []*acc
	// Exact arithmetic is deterministic; sorted identities also make budget
	// refusal independent of the record store's map iteration order.
	slices.SortFunc(rows, func(a, b reflect.Value) int { return strings.Compare(recordOf(a).ID, recordOf(b).ID) })
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
			group = &acc{keys: keys, folds: make([]*platform.ExprNumericAggregate, len(measures))}
			for i, measure := range measures {
				kind := measure.kind
				if measure.op == "count" {
					kind = "integer"
				}
				group.folds[i], err = platform.NewExprNumericAggregate(measure.op, kind, "")
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
			if c := compareValues(comparable(platform.FieldInfo{}, a.keys[i]), comparable(platform.FieldInfo{}, b.keys[i])); c != 0 {
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

// Aggregate serves a member's aggregate over a type, within the member's
// scope: it never counts a record the member could not list.
func (t *Tenant) Aggregate(m platform.Member, typ string, q AggregateQuery, now time.Time) (Aggregate, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return Aggregate{}, err
	}
	if info, ok := t.entity(typ); ok {
		q.BusinessTime = businessQuery(info, platform.Query{BusinessTime: q.BusinessTime}, m, now).BusinessTime
	}
	if q.Collection != nil {
		if q.Window != nil || q.Histogram != nil || q.Traversal != nil || q.Set != nil || len(q.Domain) > 0 || q.Search != "" || q.Archived {
			return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A declared collection cannot be combined with another selection")
		}
		root, ok := t.entity(q.Collection.Object)
		if !ok {
			return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection owner is unavailable")
		}
		caller := platform.NewCaller(runtime{t}, m, root.App, false, false)
		page, err := caller.ReadRecordSet(root.Type, q.Collection.ID, q.Collection.Name, now)
		if err != nil {
			return Aggregate{}, err
		}
		if page.Object != typ {
			return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection element differs from aggregate object")
		}
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		et := t.records.types[typ]
		if et == nil {
			return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
		}
		view := t.aggregateView(m, et)
		rows := make([]reflect.Value, len(page.Records))
		for i, raw := range page.Records {
			value := reflect.New(et.info.Go).Elem()
			if json.Unmarshal(raw, value.Addr().Interface()) != nil {
				return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Collection row is invalid")
			}
			rows[i] = value
		}
		out, err := t.records.aggregate(view, q, nil, rows)
		out.Partial = page.Partial
		return out, err
	}
	if q.Traversal != nil {
		domain, err := t.linkTraversalDomain(m, typ, *q.Traversal, q.Domain, now)
		if err != nil {
			return Aggregate{}, err
		}
		q.Domain = domain
		q.Traversal = nil
	}
	s := t.records
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return Aggregate{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return Aggregate{}, err
	}
	view := t.aggregateView(m, et)
	s.mu.Lock()
	defer s.mu.Unlock()
	out, refusal := s.aggregate(view, q, visible)
	if refusal == nil && q.Window == nil && q.Histogram == nil {
		query := platform.Query{Set: q.Set, Domain: q.Domain, Search: q.Search, Archived: q.Archived, BusinessTime: q.BusinessTime}
		all, err := s.matchingQuery(et, query, nil)
		if err != nil {
			return Aggregate{}, err
		}
		shown, err := s.matchingQuery(view, query, visible)
		if err != nil {
			return Aggregate{}, err
		}
		out.Partial = len(all) != len(shown)
	}
	return out, refusal
}

func (t *Tenant) aggregateView(member platform.Member, et *entityType) *entityType {
	view, _ := viewOf(member, et)
	if fields := t.narrowable(et); len(fields) > 0 {
		info := view.info
		info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(field platform.FieldInfo) bool { return slices.Contains(fields, field.Name) })
		view = &entityType{info: info, rows: view.rows}
	}
	return view
}

// Definitions and installation consult the original aggregate compiler so
// grouping/measure semantics do not diverge into a second UI validator.
func checkAggregateSection(section platform.Section, info platform.EntityInfo) bool {
	if section.CheckHeatmap(info) != nil {
		return false
	}
	et := entityType{info: info}
	if _, ok := et.measure(section.Measure); !ok {
		return false
	}
	if section.Widget == "chart" || (section.Widget == "pivot" || section.Widget == "heatmap") {
		if _, ok := et.grouping(section.Group); !ok {
			return false
		}
	}
	if (section.Widget == "pivot" || section.Widget == "heatmap") && section.ColumnGroup != "" {
		if section.ColumnGroup == section.Group {
			return false
		}
		if _, ok := et.grouping(section.ColumnGroup); !ok {
			return false
		}
	}
	if section.Widget == "chart" {
		mark := section.Mark
		if mark == "" {
			mark = "bar"
		}
		switch mark {
		case "bar":
		case "line", "area":
			name, unit, ok := strings.Cut(section.Group, ":")
			f, exists := info.Field(name)
			if !ok || !slices.Contains([]string{"day", "week", "month", "year"}, unit) || name != "created" && name != "changed" && (!exists || f.Type != "date" && f.Type != "datetime") {
				return false
			}
		case "arc":
			op, name, _ := strings.Cut(section.Measure, ":")
			f, _ := info.Field(name)
			if op != "count" && op != "sum" || f.Type == "money" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
