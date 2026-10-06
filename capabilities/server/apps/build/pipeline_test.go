package build

import (
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func TestPipelineStepsAndExpectations(t *testing.T) {
	rows := []map[string]any{
		{"matnr": "A", "plant": "1000", "qty": 2.0, "price": "3.5", "changed": "2026-01-02T00:00:00Z"},
		{"matnr": "B", "plant": "2000", "qty": 1.0, "price": "x"},
		{"matnr": "A", "plant": "1000", "qty": 4.0, "price": "3.5"},
		{"matnr": "", "plant": "1000", "qty": 1.0, "price": "1"},
	}
	plants := []map[string]any{{"werks": "1000", "name": "Shanghai"}, {"werks": "2000", "name": "Munich"}}
	p := Pipeline{Name: "mat", Key: "matnr", Steps: []Step{
		{Kind: "filter", Column: "plant", Op: "=", Value: "1000"},
		{Kind: "cast", Column: "price", Type: "number"},
		{Kind: "compute", To: "value", Formula: "qty * price"},
		{Kind: "lookup", Dataset: "plants", Column: "plant", Match: "werks", Columns: []string{"name"}, As: "plant_"},
		{Kind: "aggregate", Columns: []string{"matnr", "plant_name"}, Measures: []Measure{{Fn: "sum", Column: "value", To: "total"}, {Fn: "count", To: "lines"}}},
		{Kind: "rename", From: "total", To: "amount"},
		{Kind: "sort", Column: "amount", Desc: true},
	}, Expectations: []Expectation{{Column: "matnr", Rule: "notnull"}, {Column: "amount", Rule: "range", Value: "0..100"}}}
	kept, bad, err := p.Execute(rows, func(string) ([]map[string]any, error) { return plants, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0]["amount"] != 21.0 || kept[0]["lines"] != 2.0 || kept[0]["plant_name"] != "Shanghai" {
		t.Fatalf("kept %+v", kept)
	}
	if len(bad) != 1 || !strings.Contains(bad[0].Reason, "matnr is empty") {
		t.Fatalf("quarantine %+v", bad)
	}
	out := p.ObjectRows(kept)
	if out[0].ID != "A" || !strings.HasPrefix(out[0].Key, "pipeline:mat:A:") || !strings.Contains(string(out[0].Payload), `"amount":21`) {
		t.Fatalf("object rows %+v", out)
	}
	for _, s := range []Step{{Kind: "filter"}, {Kind: "compute", To: "x", Formula: "qty *"}, {Kind: "nope"}} {
		if checkStep(platform.Caller{}, s) == nil {
			t.Fatalf("step %+v accepted", s)
		}
	}
	if !(Pipeline{State: "published"}).Due(time.Now(), 1) || (Pipeline{State: "published", Last: &PipelineRun{Input: 1}}).Due(time.Now(), 1) {
		t.Fatal("due")
	}
	schema := DatasetSchema(rows)
	if len(schema) != 5 || schema[0].Name != "changed" || schema[0].Type != "date" {
		t.Fatalf("schema %+v", schema)
	}
}
