package platform

import (
	"reflect"
	"testing"
)

type jsonTraceStep struct {
	Arguments string   `json:"arguments"`
	Sources   []string `json:"sources"`
	Calls     []struct {
		Payload map[string]any `json:"payload"`
	} `json:"calls"`
}
type jsonTraceRecord struct {
	Record
	Steps    []jsonTraceStep  `json:"steps" type:"json" field:"readonly"`
	Plain    []jsonTraceStep  `json:"plain" field:"readonly"`
	Array    [2]jsonTraceStep `json:"array" type:"json" field:"readonly"`
	Withheld bool             `json:"withheld"`
}

func TestExplicitJSONCollectionsKeepTheirFieldAndOriginalDerivation(t *testing.T) {
	info, err := Describe("probe", Entity{Type: "probe.trace", Model: jsonTraceRecord{}, Withheld: "withheld", Derived: []Derivation{{From: "steps.sources", Fields: []string{"arguments", "calls"}}}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"steps", "array"} {
		field, found := info.Field(name)
		if !found || field.Type != "json" {
			t.Fatalf("%s=%#v, found=%t", name, field, found)
		}
	}
	plain, _ := info.Field("plain")
	if plain.Type != "lines" {
		t.Fatalf("unannotated child collection changed kind: %s", plain.Type)
	}
	if len(info.Derived) != 1 || len(info.Derived[0].List) == 0 || len(info.Derived[0].Refs) != 1 || len(info.Derived[0].Fields) != 2 {
		t.Fatalf("JSON list lost source derivation: %#v", info.Derived)
	}
}

func TestExplicitJSONDoesNotTurnAMapIntoADerivationList(t *testing.T) {
	type record struct {
		Record
		Steps    map[string]jsonTraceStep `json:"steps" type:"json"`
		Withheld bool                     `json:"withheld"`
	}
	if _, err := Describe("probe", Entity{Type: "probe.trace", Model: record{}, Withheld: "withheld", Derived: []Derivation{{From: "steps.sources", Fields: []string{"arguments"}}}}, func(reflect.Type) string { return "" }); err == nil {
		t.Fatal("a JSON map was accepted as a record list")
	}
}
