package host

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK27CreateFillsDefaultsThenDerivesThenChecksRequired(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k27")
	const tier, order = "invariantproject.tier", "invariantproject.order"
	rate := &platform.Expr{Op: "path", Kind: "integer", Args: []platform.Expr{{Kind: "reference", Ref: "tier"}}, Path: []string{"rate"}}
	live.accept(t, "build.object.create", build.ObjectType, "TIER", map[string]any{"name": "tier", "title": "Tier", "fields": []ontology.Field{
		{Name: "title", Title: "Title", Type: "text", Required: true},
		{Name: "rate", Title: "Rate", Type: "integer"},
	}})
	live.accept(t, "build.object.create", build.ObjectType, "ORDER", map[string]any{"name": "order", "title": "Order", "fields": []ontology.Field{
		{Name: "title", Title: "Title", Type: "text", Required: true},
		{Name: "kind", Title: "Kind", Type: "choice", Choices: "standard,rush", Default: "standard", Required: true},
		{Name: "tier", Title: "Tier", Type: "reference", Ref: tier, Default: "T1"},
		{Name: "rate", Title: "Rate", Type: "integer", From: rate, Required: true},
	}})
	live.accept(t, "build.object.publish", build.ObjectType, "TIER", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "ORDER", nil)
	live.accept(t, tier+".create", tier, "T1", map[string]any{"title": "Standard", "rate": 5})
	live.accept(t, tier+".create", tier, "T2", map[string]any{"title": "Priority", "rate": 9})
	live.accept(t, tier+".create", tier, "T0", map[string]any{"title": "Unrated"})

	read := func(tb *invariantTenant, id string) map[string]any {
		t.Helper()
		view, refusal := tb.RecordOf(t.Context(), tb.member(t), order, id, invariantClock)
		if refusal != nil {
			t.Fatalf("read %s: %s", id, refusal.Message)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		return rec
	}
	live.accept(t, order+".create", order, "O1", map[string]any{"title": "Defaults only"})
	if got := read(live, "O1"); got["kind"] != "standard" || got["tier"] != "T1" || got["rate"] != float64(5) {
		t.Fatalf("a create without kind, tier and rate should take the defaults and derive the rate from the default tier: %v", got)
	}
	live.accept(t, order+".create", order, "O2", map[string]any{"title": "Chosen tier", "kind": "rush", "tier": "T2"})
	if got := read(live, "O2"); got["kind"] != "rush" || got["rate"] != float64(9) {
		t.Fatalf("supplied values win over defaults and the rate follows the chosen tier: %v", got)
	}
	live.accept(t, order+".create", order, "O3", map[string]any{"title": "Typed rate", "tier": "T2", "rate": 7})
	if got := read(live, "O3"); got["rate"] != float64(7) {
		t.Fatalf("a supplied value of a derived field is kept on creation: %v", got)
	}
	if _, refusal := live.Submit(live.member(t), live.submission(order+".create", order, "O4", map[string]any{"title": "Nothing to derive", "tier": "T0"}), invariantClock); refusal == nil {
		t.Fatal("a required field that stays empty after defaults and derivation must still be refused")
	}

	before := live.recordImage(t)
	replayed := live.restore(t, live.entries)
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must read the filled and derived values back, not compute different ones")
	}
}
