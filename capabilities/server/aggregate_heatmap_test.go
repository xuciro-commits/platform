package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"reflect"
	"testing"
)

type optionalHeatmapRecord struct {
	platform.Record
	Row    *string `json:"row"`
	Column *string `json:"column"`
}

func TestHeatmapOriginalCompleteAxisIdentity(t *testing.T) {
	info, err := platform.Describe("sample", platform.Entity{Type: "sample.heatmap", Model: optionalHeatmapRecord{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	s := newRecordStore()
	if err = s.install(info); err != nil {
		t.Fatal(err)
	}
	et := s.types[info.Type]
	text := func(v string) *string { return &v }
	values := [][2]*string{{text("A"), text("X")}, {text("A"), text("X")}, {text("A|B"), text("C")}, {text("A"), text("B|C")}, {nil, text("Y")}, {nil, text("Y")}, {text(""), text("Y")}, {text("—"), text("Y")}}
	for i, value := range values {
		id := fmt.Sprint(i)
		et.rows[id] = &row{value: reflect.ValueOf(&optionalHeatmapRecord{Record: platform.Record{ID: id}, Row: value[0], Column: value[1]}).Elem()}
	}
	q := AggregateQuery{Groups: []string{"row", "column"}, Measures: []string{"count"}, MaxRows: 64}
	out, kerr := s.aggregate(et, q, nil)
	if kerr != nil || len(out.Rows) != 6 {
		t.Fatal(out, kerr)
	}
	got := map[string]int{}
	total := 0
	for _, r := range out.Rows {
		key, _ := json.Marshal([]any{r["row"], r["column"]})
		got[string(key)] = r["count"].(int)
		total += r["count"].(int)
	}
	if total != 8 || got[`["A","X"]`] != 2 || got[`["A|B","C"]`] != 1 || got[`["A","B|C"]`] != 1 || got[`[null,"Y"]`] != 2 || got[`["","Y"]`] != 1 || got[`["—","Y"]`] != 1 {
		t.Fatal("original grouped axis identities or complete count changed", got)
	}
	q.MaxRows = 5
	out, kerr = s.aggregate(et, q, nil)
	if kerr == nil || len(out.Rows) != 0 {
		t.Fatal("partial matrix accepted", out, kerr)
	}
	q.MaxRows = 64
	out, kerr = s.aggregate(et, q, func(v reflect.Value) bool { return v.FieldByName("Row").IsNil() })
	if kerr != nil || len(out.Rows) != 1 || out.Rows[0]["count"] != 2 {
		t.Fatal("original matching scope changed", out, kerr)
	}
}
