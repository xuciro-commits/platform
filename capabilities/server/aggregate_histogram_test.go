package platformserver

import (
	"fmt"
	"math"
	"net/http/httptest"
	"platformserver/platform"
	"reflect"
	"strings"
	"testing"
	"time"
)

type histogramRecord struct {
	platform.Record
	Value   *float64 `json:"value"`
	Integer *int64   `json:"integer"`
}

func histogramStore(t *testing.T, values []*float64, integers []*int64) (*recordStore, *entityType) {
	t.Helper()
	info, err := platform.Describe("sample", platform.Entity{Type: "sample.histogram", Model: histogramRecord{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	s := newRecordStore()
	if err = s.install(info); err != nil {
		t.Fatal(err)
	}
	et := s.types[info.Type]
	n := max(len(values), len(integers))
	for i := 0; i < n; i++ {
		id := fmt.Sprint(i)
		record := histogramRecord{Record: platform.Record{ID: id}}
		if i < len(values) {
			record.Value = values[i]
		}
		if i < len(integers) {
			record.Integer = integers[i]
		}
		et.rows[id] = &row{value: reflect.ValueOf(&record).Elem()}
	}
	return s, et
}
func TestHistogramExactBoundsMissingAndEdges(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	s, et := histogramStore(t, []*float64{ptr(-1), ptr(0), ptr(1), ptr(2), ptr(3), nil}, nil)
	out, err := s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "value", Bins: 4}}, nil)
	if err != nil || out.Histogram == nil {
		t.Fatal(out, err)
	}
	h := out.Histogram
	if h.Valid != 5 || h.Missing != 1 || h.Minimum != "-1" || h.Maximum != "3" || len(h.Buckets) != 4 {
		t.Fatal(h)
	}
	for i, want := range []int{1, 1, 1, 2} {
		if h.Buckets[i].Count != want || h.Buckets[i].UpperInclusive != (i == 3) {
			t.Fatal(h)
		}
	}
	s, et = histogramStore(t, []*float64{ptr(0.1), ptr(0.2), ptr(0.3)}, nil)
	out, err = s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "value", Bins: 2}}, nil)
	if err != nil || out.Histogram.Buckets[0].Upper != "0.2" || out.Histogram.Buckets[0].Count != 1 || out.Histogram.Buckets[1].Count != 2 {
		t.Fatal(out, err)
	}
	s, et = histogramStore(t, []*float64{ptr(0), ptr(1)}, nil)
	out, err = s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "value", Bins: 3}}, nil)
	if err != nil || out.Histogram.Buckets[0].Upper != "1/3" || out.Histogram.Buckets[1].Upper != "2/3" {
		t.Fatal(out, err)
	}
}
func TestHistogramIntegerPrecisionEmptyConstantAndRejection(t *testing.T) {
	ip := func(v int64) *int64 { return &v }
	s, et := histogramStore(t, nil, []*int64{ip(9007199254740992), ip(9007199254740993), ip(9007199254740994)})
	out, err := s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "integer", Bins: 2}}, nil)
	if err != nil || out.Histogram.Buckets[0].Upper != "9007199254740993" || out.Histogram.Buckets[0].Count != 1 || out.Histogram.Buckets[1].Count != 2 {
		t.Fatal(out, err)
	}
	for _, values := range [][]*int64{{nil, nil}, {ip(7), ip(7)}, {}} {
		s, et = histogramStore(t, nil, values)
		out, err = s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "integer", Bins: 12}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		h := out.Histogram
		if len(values) == 2 && values[0] != nil {
			if len(h.Buckets) != 1 || h.Buckets[0].Lower != "7" || h.Buckets[0].Upper != "7" || h.Buckets[0].Count != 2 || !h.Buckets[0].UpperInclusive {
				t.Fatal(h)
			}
		} else if h.Valid != 0 || len(h.Buckets) != 0 || h.Minimum != "" || h.Maximum != "" || h.Missing != len(values) {
			t.Fatal(h)
		}
	}
	s, et = histogramStore(t, nil, []*int64{ip(0), ip(1)})
	for _, q := range []AggregateQuery{{Histogram: &HistogramQuery{Field: "integer", Bins: 0}}, {Histogram: &HistogramQuery{Field: "integer", Bins: 65}}, {Histogram: &HistogramQuery{Field: "integer", Bins: 2}, Groups: []string{"integer"}}, {Histogram: &HistogramQuery{Field: "integer", Bins: 2}, Measures: []string{"sum:integer"}}, {Histogram: &HistogramQuery{Field: "integer", Bins: 2}, MaxRows: 1}, {Histogram: &HistogramQuery{Field: "hidden", Bins: 2}}} {
		if _, err = s.aggregate(et, q, nil); err == nil {
			t.Fatal("invalid histogram accepted", q)
		}
	}
	bad := math.Inf(1)
	s, et = histogramStore(t, []*float64{&bad}, nil)
	if _, err = s.aggregate(et, AggregateQuery{Histogram: &HistogramQuery{Field: "value", Bins: 2}}, nil); err == nil {
		t.Fatal("nonfinite value silently dropped")
	}
}
func TestHistogramOriginalScopeAndPostContract(t *testing.T) {
	tn := setFixture(t)
	lead, _ := tn.Member("lead")
	ana, _ := tn.Member("ana")
	for _, tc := range []struct {
		m     platform.Member
		count int
	}{{lead, 620}, {ana, 310}} {
		out, err := tn.Aggregate(tc.m, "stock.item", AggregateQuery{Histogram: &HistogramQuery{Field: "qty", Bins: 12}}, time.Now())
		if err != nil || out.Histogram.Valid != tc.count {
			t.Fatal(out, err)
		}
	}
	if _, err := tn.Aggregate(ana, "stock.item", AggregateQuery{Histogram: &HistogramQuery{Field: "secret", Bins: 12}}, time.Now()); err == nil {
		t.Fatal("hidden field accepted")
	}
	handler := NewHost(Tokens(map[string]string{"lead": "lead"}), tn).Handler()
	for _, body := range []string{`{"histogram":{"field":"qty","bins":3}}`, `{"histogram":{"field":"qty","bins":3},"limit":1}`} {
		r := httptest.NewRequest("POST", "/v1/aggregates/stock.item/query", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer lead")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if strings.Contains(body, "limit") {
			if w.Code == 200 {
				t.Fatal("histogram silently accepted record window")
			}
		} else if w.Code != 200 || !strings.Contains(w.Body.String(), `"histogram"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
