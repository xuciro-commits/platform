package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// States and actions a tenant gives its own object (ADR-0037 18a): lost
// property found at a desk, handed back to whoever claims it, with the rules
// the builder wrote checked in the decision and again in replay.
func TestTenantDefinedActions(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("dana", build.Builder), seat("eli", build.User)), build.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member {
		m, _ := tn.app(PlatformApp).(*Console).Member(id)
		return m
	}
	keys := 0
	do := func(who, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: build.ID, IdempotencyKey: fmt.Sprint("a", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return fmt.Sprintf("%s: %s", err.Code, err.Message)
		}
		return "ok"
	}
	fields := []map[string]any{
		{"name": "item", "title": "Item", "type": "text", "required": true, "search": true},
		{"name": "value", "title": "Value", "type": "integer"},
		{"name": "claimant", "title": "Handed to", "type": "text"},
		{"name": "clerk", "title": "Handed back by", "type": "text"},
		{"name": "returned", "title": "Returned on", "type": "date"},
		{"name": "grade", "title": "Grade", "type": "choice", "choices": "a,b"},
		{"name": "price", "title": "Price", "type": "money"},
	}
	states := []map[string]any{{"name": "found", "title": "Found", "tone": "warning"}, {"name": "returned", "title": "Returned", "tone": "success"}}
	handBack := map[string]any{"name": "handback", "title": "Hand it back", "from": []string{"found"}, "to": "returned",
		"inputs": []map[string]any{{"name": "to", "title": "Handed to", "type": "text", "required": true}},
		"sets":   []map[string]any{{"field": "claimant", "from": "to"}, {"field": "clerk", "from": "$me"}, {"field": "returned", "from": "$now"}},
		"conditions": []map[string]any{{"field": "value", "operator": "<", "value": "500",
			"message": "Something worth 500 or more goes back through the manager."}}}
	note := map[string]any{"name": "note", "title": "Note who asked", "from": []string{"found"},
		"inputs": []map[string]any{{"name": "who", "title": "Who asked", "type": "text"}}, "sets": []map[string]any{{"field": "claimant", "from": "who"}}}
	lost := build.TypeOf("lost")

	// What the host refuses while it is a draft, with the reason.
	for _, x := range []struct {
		why     string
		payload map[string]any
		want    string
	}{
		{"actions with no states", map[string]any{"actions": []any{handBack}}, "needs states before it has actions"},
		{"a state that is not there", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"to": "shelved"})}}, `leaves records in "shelved"`},
		{"a field that is not there", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"sets": []map[string]any{{"field": "colour", "from": "to"}}})}}, `sets "colour"`},
		{"an input that is not there", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"sets": []map[string]any{{"field": "claimant", "from": "nobody"}}})}}, `from "nobody"`},
		{"a condition with no message", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"conditions": []map[string]any{{"field": "value", "operator": "<", "value": "5"}}})}}, "says nothing to a person"},
		{"a numeric condition with text", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"conditions": []map[string]any{{"field": "value", "operator": "<", "value": "five", "message": "Too much"}}})}}, "does not fit integer"},
		{"a text input assigned to a number", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"sets": []map[string]any{{"field": "value", "from": "to"}}})}}, "which does not fit"},
		{"a choice outside the field", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"inputs": []map[string]any{{"name": "grade", "title": "Grade", "type": "choice", "choices": "a,c"}}, "sets": []map[string]any{{"field": "grade", "from": "grade"}}})}}, "not one of the field's choices"},
		{"money without currency", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"conditions": []map[string]any{{"field": "price", "operator": "=", "value": "10", "message": "Wrong amount"}}})}}, "currency-aware comparison"},
		{"the platform's own name", map[string]any{"states": states, "actions": []any{merge(handBack, map[string]any{"name": "edit"})}}, "the platform's own"},
	} {
		draft := "O-" + strings.ReplaceAll(x.why, " ", "-")
		payload := merge(map[string]any{"name": "lost", "title": "Lost item", "plural": "Lost property", "fields": fields}, x.payload)
		if got := do("dana", build.ObjectType+".create", build.ObjectType, draft, payload); got != "ok" {
			t.Fatalf("%s: draft: %s", x.why, got)
		}
		if got := do("dana", build.SchemaPublish, build.ObjectType, draft, map[string]any{}); !strings.Contains(got, x.want) {
			t.Errorf("%s: %s, want %q", x.why, got, x.want)
		}
		if got := do("dana", build.ObjectType+".archive", build.ObjectType, draft, map[string]any{}); got != "ok" {
			t.Fatalf("%s: archive the draft: %s", x.why, got)
		}
	}

	// Published with its states and actions.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1",
		map[string]any{"name": "lost", "title": "Lost item", "plural": "Lost property", "fields": fields, "states": states, "actions": []any{handBack, note}}); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish: %s", got)
	}
	record := func(id string) map[string]any {
		view, err := tn.RecordOf(member("eli"), lost, id, now)
		if err != nil {
			t.Fatalf("record %s: %v", id, err)
		}
		raw, _ := json.Marshal(view.Record)
		var out map[string]any
		json.Unmarshal(raw, &out)
		return out
	}
	if got := do("eli", lost+".create", lost, "L-1", map[string]any{"item": "Umbrella", "value": 20}); got != "ok" {
		t.Fatalf("a found item: %s", got)
	}
	if got := do("eli", lost+".create", lost, "L-2", map[string]any{"item": "Watch", "value": 900}); got != "ok" {
		t.Fatalf("a second found item: %s", got)
	}
	if state := record("L-1")["state"]; state != "found" {
		t.Fatalf("a new record starts in the first state: %v", state)
	}
	// The generated edit does not move it: only its actions do.
	if got := do("eli", lost+".edit", lost, "L-1", map[string]any{"state": "returned"}); got == "ok" && record("L-1")["state"] != "found" {
		t.Error("the edit form moved the record's state")
	}
	// What the builder wrote is what people are told.
	if got := do("eli", lost+".handback", lost, "L-1", map[string]any{}); !strings.Contains(got, "needs Handed to") {
		t.Errorf("a required input left out: %s", got)
	}
	if got := do("eli", lost+".handback", lost, "L-2", map[string]any{"to": "Ada"}); !strings.Contains(got, "Something worth 500 or more goes back through the manager.") {
		t.Errorf("a condition that does not hold: %s", got)
	}
	if got := do("eli", lost+".note", lost, "L-1", map[string]any{"who": "Grace"}); got != "ok" {
		t.Fatalf("an action that leaves the record where it was: %s", got)
	}
	if r := record("L-1"); r["state"] != "found" || r["claimant"] != "Grace" {
		t.Errorf("after noting who asked: %+v", r)
	}
	if got := do("eli", lost+".handback", lost, "L-1", map[string]any{"to": "Ada"}); got != "ok" {
		t.Fatalf("hand it back: %s", got)
	}
	if r := record("L-1"); r["state"] != "returned" || r["claimant"] != "Ada" || r["clerk"] != "eli" || r["returned"] != "2026-10-01" {
		t.Errorf("after handing it back: %+v", r)
	}
	if got := do("eli", lost+".handback", lost, "L-1", map[string]any{"to": "Ada"}); !strings.Contains(got, "takes it only from found") {
		t.Errorf("an action from a state the record is not in: %s", got)
	}
	// Every surface offers it as a coded transition: the catalog, the entity's lifecycle.
	offered := false
	for _, a := range tn.Catalog(member("eli")) {
		offered = offered || a.Schema == lost+".handback" && a.Title == "Hand it back" && len(a.Payload) == 1 && a.Payload[0].Required
	}
	if !offered {
		t.Error("the action is not in the member's catalog")
	}
	CheckReplay(t, tn, journal, compose)
}

// merge is a with b's keys laid over it.
func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
