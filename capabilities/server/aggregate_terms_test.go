package platformserver

import (
	"fmt"
	"platformserver/platform"
	"reflect"
	"testing"
)

type optionalTermRecord struct {
	platform.Record
	Term *string `json:"term"`
}

func TestAggregateTermsOptionalIdentityAndCompleteness(t *testing.T) {
	info, err := platform.Describe("sample", platform.Entity{Type: "sample.optional", Model: optionalTermRecord{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	s := newRecordStore()
	if err = s.install(info); err != nil {
		t.Fatal(err)
	}
	et := s.types[info.Type]
	text := func(value string) *string { return &value }
	values := []*string{nil, nil, text(""), text("<nil>"), text("—"), text("constructor"), text("constructor")}
	for i, value := range values {
		id := fmt.Sprint(i)
		et.rows[id] = &row{value: reflect.ValueOf(&optionalTermRecord{Record: platform.Record{ID: id}, Term: value}).Elem()}
	}
	out, kerr := s.aggregate(et, AggregateQuery{Groups: []string{"term"}, Measures: []string{"count"}, MaxRows: 64}, nil)
	if kerr != nil || len(out.Rows) != 5 {
		t.Fatal(out, kerr)
	}
	got := map[any]int{}
	for _, r := range out.Rows {
		got[r["term"]] = r["count"].(int)
	}
	if got[nil] != 2 || got[""] != 1 || got["<nil>"] != 1 || got["—"] != 1 || got["constructor"] != 2 {
		t.Fatal("optional values merged or split by address", got)
	}
	out, kerr = s.aggregate(et, AggregateQuery{Groups: []string{"term"}, MaxRows: 4}, nil)
	if kerr == nil || len(out.Rows) != 0 {
		t.Fatal("partial groups returned", out, kerr)
	}
	out, kerr = s.aggregate(et, AggregateQuery{Groups: []string{"term"}, MaxRows: 64}, func(v reflect.Value) bool { return v.FieldByName("Term").IsNil() })
	if kerr != nil || len(out.Rows) != 1 || out.Rows[0]["term"] != nil || out.Rows[0]["count"] != 2 {
		t.Fatal("authorized matching scope changed", out, kerr)
	}
}
