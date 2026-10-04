package platformserver

import (
	"fmt"
	"math"
	"platformserver/platform"
	"reflect"
	"testing"
	"time"
)

type windowRecord struct {
	platform.Record
	At        *time.Time `json:"at"`
	Value     *float64   `json:"value"`
	Integer   *int64     `json:"integer"`
	Owner     string     `json:"owner" field:"search"`
	Secret    *float64   `json:"secret" read:"manager"`
	PrivateAt *time.Time `json:"privateAt" read:"manager"`
}

func windowStore(t *testing.T) (*recordStore, *entityType) {
	t.Helper()
	info, err := platform.Describe("sample", platform.Entity{Type: "sample.window", Model: windowRecord{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	s := newRecordStore()
	if err := s.install(info); err != nil {
		t.Fatal(err)
	}
	return s, s.types[info.Type]
}
func windowPut(et *entityType, id string, at *time.Time, value *float64) {
	record := windowRecord{Record: platform.Record{ID: id, Revision: 7}, At: at, Value: value, Owner: "visible"}
	et.rows[id] = &row{value: reflect.ValueOf(&record).Elem()}
}
func TestWindowStatisticsCompleteThousandTenThousandHundredThousandRows(t *testing.T) {
	s, et := windowStore(t)
	base := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 100002; i++ {
		at := base.Add(time.Duration(i) * time.Nanosecond)
		value := float64(i)
		windowPut(et, fmt.Sprintf("R%06d", i), &at, &value)
	}
	s.generation = 1234
	for _, n := range []int{1000, 10000, 100000} {
		q := AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: n, Threshold: "100000"}}
		out, err := s.aggregate(et, q, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := out.Window
		lo := float64(100002 - n)
		hi := float64(100001)
		mean := (lo + hi) / 2
		if w.Count != n || w.Valid != n || w.Total != 100002 || w.Timed != 100002 || w.MissingTime != 0 || w.Above != 1 || *w.Min != lo || *w.Max != hi || *w.Mean != mean || w.First.ID != "R100001" || w.Last.ID != fmt.Sprintf("R%06d", 100002-n) || w.Generation != "1234" {
			t.Fatal(n, w)
		}
		if !w.First.Time.After(w.Last.Time) || w.First.Revision != 7 {
			t.Fatal("window edges lost exact times and opened revisions")
		}
	}
}
func TestWindowStatisticsOffsetsTiesMissingAndExactThreshold(t *testing.T) {
	s, et := windowStore(t)
	parse := func(text string) *time.Time {
		at, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			t.Fatal(err)
		}
		return &at
	}
	num := func(n float64) *float64 { return &n }
	windowPut(et, "B", parse("2026-10-03T01:00:00+01:00"), num(0.1))
	windowPut(et, "A", parse("2026-10-03T00:00:00Z"), nil)
	windowPut(et, "C", parse("2026-10-03T00:00:00.000000001Z"), num(0.2))
	windowPut(et, "D", nil, num(999))
	windowPut(et, "ZERO", new(time.Time), num(999))
	out, issue := s.aggregate(et, AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 3, Threshold: "0.1"}}, nil)
	if issue != nil {
		t.Fatal(issue)
	}
	w := out.Window
	if w.Total != 5 || w.Timed != 4 || w.MissingTime != 1 || w.Count != 3 || w.Valid != 2 || w.Missing != 1 || w.Above != 1 || *w.Mean != 0.15 || w.First.ID != "C" || w.Last.ID != "B" {
		t.Fatal(w)
	}
	out, issue = s.aggregate(et, AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 2, Threshold: "0.10000000000000001"}}, nil)
	if issue != nil || out.Window.Last.ID != "A" || out.Window.Missing != 1 || out.Window.Above != 1 {
		t.Fatal(out, issue)
	}
	empty := AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 1000, Threshold: "0"}, Domain: platform.Raw([]any{[]any{"owner", "=", "none"}})}
	out, issue = s.aggregate(et, empty, nil)
	if issue != nil || out.Window.Count != 0 || out.Window.Mean != nil || out.Window.Min != nil || out.Window.Max != nil || out.Window.First != nil || out.Window.Last != nil {
		t.Fatal(out, issue)
	}
	onlyMissing := AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 1, Threshold: "0"}, Domain: platform.Raw([]any{[]any{"id", "=", "A"}})}
	out, issue = s.aggregate(et, onlyMissing, nil)
	if issue != nil || out.Window.Valid != 0 || out.Window.Missing != 1 || out.Window.Mean != nil || out.Window.Above != 0 {
		t.Fatal(out, issue)
	}
}
func TestWindowStatisticsRefuseMixedModesTypesAndUnsafeValues(t *testing.T) {
	s, et := windowStore(t)
	at := time.Now()
	v := 1.0
	windowPut(et, "A", &at, &v)
	base := AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 1000, Threshold: "0"}}
	for name, change := range map[string]func(*AggregateQuery){"too many": func(q *AggregateQuery) { q.Window.Rows = 100001 }, "zero rows": func(q *AggregateQuery) { q.Window.Rows = 0 }, "wrong time": func(q *AggregateQuery) { q.Window.TimeField = "created" }, "text signal": func(q *AggregateQuery) { q.Window.Field = "owner" }, "hidden branch": func(q *AggregateQuery) {
		q.Set = setExpression("union", &platform.RecordSetPredicate{}, setTerm("unknown", "=", 1))
	}, "group": func(q *AggregateQuery) { q.Groups = []string{"owner"} }, "measure": func(q *AggregateQuery) { q.Measures = []string{"count"} }, "histogram": func(q *AggregateQuery) { q.Histogram = &HistogramQuery{Field: "value", Bins: 2} }, "row budget": func(q *AggregateQuery) { q.MaxRows = 1 }} {
		t.Run(name, func(t *testing.T) {
			q := base
			w := *base.Window
			q.Window = &w
			change(&q)
			if out, err := s.aggregate(et, q, nil); err == nil || out.Window != nil {
				t.Fatal("bad window accepted", out, err)
			}
		})
	}
	for _, threshold := range []string{"", " ", "1/3", "0x10", "NaN", "Infinity", "1e999999", "1e-999999", "1e-325", string(make([]byte, 81))} {
		q := base
		w := *base.Window
		w.Threshold = threshold
		q.Window = &w
		if _, err := s.aggregate(et, q, nil); err == nil {
			t.Fatal("bad threshold accepted", threshold)
		}
	}
	bad := math.Inf(1)
	windowPut(et, "A", &at, &bad)
	if _, err := s.aggregate(et, base, nil); err == nil {
		t.Fatal("non-finite numeric record accepted")
	}
	n := int64(9007199254740992)
	record := windowRecord{Record: platform.Record{ID: "A", Revision: 1}, At: &at, Integer: &n}
	et.rows["A"] = &row{value: reflect.ValueOf(&record).Elem()}
	base.Window.Field = "integer"
	if _, err := s.aggregate(et, base, nil); err == nil {
		t.Fatal("unsafe integer rounded")
	}
}
