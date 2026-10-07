package platform

import (
	"reflect"
	"strings"
	"testing"
)

// #138/#132: a field bounded to another field's value is described, and a
// condition on nothing, on a text, on a conditional field or on a value the
// field never takes is refused at declaration.
func TestFieldConditions(t *testing.T) {
	type good struct {
		Record
		Kind   string `json:"kind" choices:"part,tool"`
		Urgent bool   `json:"urgent"`
		Serial string `json:"serial" field:"required" when:"kind=tool"`
		Why    string `json:"why" when:"urgent=true"`
	}
	info, err := Describe("x", Entity{Type: "x.item", Title: "Item", Model: good{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := info.Field("serial")
	if serial.When == nil || serial.When.Field != "kind" || !serial.Active(func(string) string { return "tool" }) || serial.Active(func(string) string { return "part" }) {
		t.Fatalf("serial: %+v", serial.When)
	}
	for name, model := range map[string]any{
		"nothing": struct {
			Record
			A string `json:"a" when:"b=1"`
		}{},
		"a text": struct {
			Record
			B string `json:"b"`
			A string `json:"a" when:"b=1"`
		}{},
		"a conditional": struct {
			Record
			K string `json:"k" choices:"x,y"`
			B string `json:"b" choices:"1,2" when:"k=x"`
			A string `json:"a" when:"b=1"`
		}{},
		"a value it never takes": struct {
			Record
			K string `json:"k" choices:"x,y"`
			A string `json:"a" when:"k=z"`
		}{},
		"itself": struct {
			Record
			K string `json:"k" choices:"x,y" when:"k=x"`
		}{},
	} {
		if _, err := Describe("x", Entity{Type: "x.bad", Title: "Bad", Model: model}, func(reflect.Type) string { return "" }); err == nil || !strings.Contains(err.Error(), "depends on") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
