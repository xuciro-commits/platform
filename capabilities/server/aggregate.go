package platformserver

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
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
	Domain   json.RawMessage
	Search   string
	Archived bool
	Groups   []string
	Measures []string
}

// Aggregate is the answer: the columns it has, then one row per group, keyed
// by column name, in the order of the groups' values.
type Aggregate struct {
	Columns []Column         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
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
	value  func(reflect.Value) float64 // money in minor units
}

var buckets = map[string]func(time.Time) string{
	"day":   func(t time.Time) string { return t.Format(time.DateOnly) },
	"week":  func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%04d-W%02d", y, w) },
	"month": func(t time.Time) string { return t.Format("2006-01") },
	"year":  func(t time.Time) string { return t.Format("2006") },
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
				return grouping{column: col, key: func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface() }}, true
			}
			date = func(v reflect.Value) (time.Time, bool) { // text buckets by the date it starts with ("2026-10-01T09:00")
				switch x := v.FieldByIndex(f.Index).Interface().(type) {
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
			return grouping{column: col, key: func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface() }}, true
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
	if !ok || !slices.Contains([]string{"sum", "avg", "min", "max"}, op) || !slices.Contains([]string{"integer", "decimal", "money"}, f.Type) {
		return measure{}, false
	}
	col := Column{Name: name, Title: fmt.Sprintf("%s (%s)", f.Title, op), Kind: "measure", Type: "quantitative", Field: field, Money: f.Type == "money"}
	return measure{column: col, op: op, value: func(v reflect.Value) float64 {
		n, _ := comparable(f, v.FieldByIndex(f.Index).Interface()).(float64)
		return n
	}}, true
}

// aggregate groups and measures the matching records; the caller holds s.mu.
func (s *recordStore) aggregate(et *entityType, q AggregateQuery, visible func(reflect.Value) bool) (Aggregate, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
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
	rows, err := s.matching(et, q.Domain, q.Search, q.Archived, visible)
	if err != nil {
		return Aggregate{}, err
	}
	type acc struct {
		keys  []any
		count int
		sums  []float64
		mins  []float64
		maxs  []float64
		seen  []int
	}
	byKey := map[string]*acc{}
	var order []*acc
	for _, v := range rows {
		keys := make([]any, len(groups))
		for i, g := range groups {
			keys[i] = g.key(v)
		}
		k := fmt.Sprint(keys...)
		if len(keys) > 1 {
			k = fmt.Sprintf("%q", keys)
		}
		a := byKey[k]
		if a == nil {
			a = &acc{keys: keys, sums: make([]float64, len(measures)), mins: make([]float64, len(measures)), maxs: make([]float64, len(measures)), seen: make([]int, len(measures))}
			for i := range measures {
				a.mins[i], a.maxs[i] = math.Inf(1), math.Inf(-1)
			}
			byKey[k] = a
			order = append(order, a)
		}
		a.count++
		for i, m := range measures {
			if m.value == nil {
				continue
			}
			n := m.value(v)
			a.sums[i] += n
			a.mins[i], a.maxs[i] = min(a.mins[i], n), max(a.maxs[i], n)
			a.seen[i]++
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
		for i, m := range measures {
			switch {
			case m.op == "count":
				row[m.column.Name] = a.count
			case a.seen[i] == 0:
				row[m.column.Name] = nil
			case m.op == "sum":
				row[m.column.Name] = a.sums[i]
			case m.op == "avg":
				row[m.column.Name] = a.sums[i] / float64(a.seen[i])
			case m.op == "min":
				row[m.column.Name] = a.mins[i]
			case m.op == "max":
				row[m.column.Name] = a.maxs[i]
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// Aggregate serves a member's aggregate over a type, within the member's
// scope: it never counts a record the member could not list.
func (t *Tenant) Aggregate(m platform.Member, typ string, q AggregateQuery, now time.Time) (Aggregate, *kernel.Error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aggregate(et, q, visible)
}
