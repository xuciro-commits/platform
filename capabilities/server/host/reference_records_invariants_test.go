package host

import (
	"strings"
	"testing"

	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK39ReferenceRecordsMatchTheirFieldTypesBeforeTheyShip(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k39")
	fields := []ontology.Field{{Name: "title", Title: "Title", Type: "text", Required: true}, {Name: "factor", Title: "Factor", Type: "decimal"}, {Name: "base", Title: "Base", Type: "boolean"}}
	unit := func(records ...map[string]any) map[string]any {
		return map[string]any{"name": "unit", "title": "Unit", "display": "title", "fields": fields, "records": records}
	}
	for name, record := range map[string]map[string]any{
		"a decimal written as text":  {"id": "kg", "title": "Kilogram", "factor": "1000"},
		"a boolean written as text":  {"id": "g", "title": "Gram", "factor": 1, "base": "true"},
		"a text written as a number": {"id": "t", "title": 7},
	} {
		sub := live.submission("build.object.create", build.ObjectType, "UNIT", unit(record))
		_, refusal := live.Submit(live.member(t), sub, invariantClock)
		if refusal == nil || !strings.Contains(refusal.Message, record["id"].(string)) {
			t.Fatalf("%s must be refused naming the record, got %#v", name, refusal)
		}
	}
	live.accept(t, "build.object.create", build.ObjectType, "UNIT", unit(
		map[string]any{"id": "g", "title": "Gram", "factor": 1, "base": true},
		map[string]any{"id": "kg", "title": "Kilogram", "factor": 1000, "base": false}))
	live.accept(t, "build.object.publish", build.ObjectType, "UNIT", nil)
	live.Work(invariantClock)
	for _, id := range []string{"g", "kg"} {
		if _, refusal := live.RecordOf(t.Context(), live.member(t), "invariantproject.unit", id, invariantClock); refusal != nil {
			t.Fatalf("reference record %s must be written on publish: %s", id, refusal.Message)
		}
	}
}
