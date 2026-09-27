package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Who may do what with an object a tenant defines (ADR-0037 18b): a front desk
// logs what it finds and sees only its own entries, a supervisor sees and
// hands back everything and reads the value, a cleaner does not see the
// object at all — in lists, reads, search, the catalog and generated actions.
func TestTenantDefinedAccess(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("dana", build.Builder), seat("ann", "desk"), seat("bea", "desk"),
			seat("sam", "supervisor"), seat("cy", "cleaner")), build.New("t-1"))
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
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: build.ID, IdempotencyKey: fmt.Sprint("x", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return fmt.Sprintf("%s: %s", err.Code, err.Message)
		}
		return "ok"
	}
	object := map[string]any{"name": "lost", "title": "Lost item", "plural": "Lost property",
		"fields": []map[string]any{
			{"name": "item", "title": "Item", "type": "text", "required": true, "search": true},
			{"name": "value", "title": "Value", "type": "integer", "read": []string{"supervisor"}, "write": []string{"supervisor"}},
			{"name": "claimant", "title": "Handed to", "type": "text"}},
		"states": []map[string]any{{"name": "found", "title": "Found"}, {"name": "returned", "title": "Returned"}},
		"actions": []map[string]any{{"name": "handback", "title": "Hand it back", "from": []string{"found"}, "to": "returned", "roles": []string{"supervisor"},
			"inputs": []map[string]any{{"name": "to", "title": "Handed to", "type": "text", "required": true}}, "sets": []map[string]any{{"field": "claimant", "from": "to"}}}},
		"access": []map[string]any{
			{"role": "desk", "read": "own", "create": true, "edit": true},
			{"role": "supervisor", "read": "all", "edit": true, "archive": true},
			{"role": "cleaner", "read": "none"}},
	}
	// What the host refuses, with the reason.
	for _, x := range []struct {
		why  string
		with map[string]any
		want string
	}{
		{"a role that writes what it may not read", map[string]any{"access": []map[string]any{{"role": "cleaner", "read": "none", "create": true}}}, "may not create, edit or archive it either"},
		{"an action for a role the object has not", map[string]any{"actions": []map[string]any{{"name": "handback", "title": "Hand it back", "from": []string{"found"}, "roles": []string{"porter"}}}}, `for "porter"`},
		{"a field set by one who may not read it", map[string]any{"fields": []map[string]any{{"name": "item", "title": "Item", "type": "text"},
			{"name": "claimant", "title": "Handed to", "type": "text", "read": []string{"supervisor"}, "write": []string{"desk"}}}}, "may not read it"},
		{"the builder's own role", map[string]any{"access": []map[string]any{{"role": "builder", "read": "own"}}}, "always does everything"},
	} {
		draft := "O-" + strings.ReplaceAll(x.why, " ", "-")
		if got := do("dana", build.ObjectType+".create", build.ObjectType, draft, merge(object, merge(x.with, map[string]any{"name": "bad" + fmt.Sprint(keys)}))); got != "ok" {
			t.Fatalf("%s: draft: %s", x.why, got)
		}
		if got := do("dana", build.SchemaPublish, build.ObjectType, draft, map[string]any{}); !strings.Contains(got, x.want) {
			t.Errorf("%s: %s, want %q", x.why, got, x.want)
		}
	}
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1", object); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish: %s", got)
	}
	lost := build.TypeOf("lost")

	// The roles an object names are the builder app's: the Console grants them.
	if roles := tn.app(build.ID).Manifest().AllRoles(); !slices.Contains(roles, "cleaner") || !slices.Contains(roles, "desk") {
		t.Errorf("the object's roles are not the app's: %v", roles)
	}
	// Each desk logs what it finds; a value is not theirs to set.
	if got := do("ann", lost+".create", lost, "L-1", map[string]any{"item": "Umbrella"}); got != "ok" {
		t.Fatalf("ann logs an item: %s", got)
	}
	if got := do("bea", lost+".create", lost, "L-2", map[string]any{"item": "Watch"}); got != "ok" {
		t.Fatalf("bea logs an item: %s", got)
	}
	if got := do("ann", lost+".create", lost, "L-3", map[string]any{"item": "Ring", "value": 900}); got == "ok" {
		t.Error("a desk set a field only a supervisor sets")
	}
	if got := do("sam", lost+".create", lost, "L-4", map[string]any{"item": "Hat"}); got == "ok" {
		t.Error("a supervisor created an item without create access")
	}
	if got := do("sam", lost+".edit", lost, "L-2", map[string]any{"value": 900}); got != "ok" {
		t.Fatalf("the supervisor values the watch: %s", got)
	}
	ids := func(who string) string {
		page, err := tn.Records(member(who), lost, platform.Query{Sort: []string{"id"}}, now)
		if err != nil {
			return err.Code.String()
		}
		var out []string
		for _, r := range page.Records {
			raw, _ := json.Marshal(r)
			var x struct{ ID string }
			json.Unmarshal(raw, &x)
			out = append(out, x.ID)
		}
		return strings.Join(out, ",")
	}
	// Reads: a desk its own, the supervisor all, the cleaner not the object at all.
	for who, want := range map[string]string{"ann": "L-1", "bea": "L-2", "sam": "L-1,L-2", "dana": "L-1,L-2"} {
		if got := ids(who); got != want {
			t.Errorf("%s reads %s, want %s", who, got, want)
		}
	}
	if got := ids("cy"); got != "" && !strings.Contains(got, "ERROR") {
		t.Errorf("the cleaner reads records of the object: %s", got)
	}
	if slices.ContainsFunc(tn.Entities(member("cy")), func(e platform.EntityInfo) bool { return e.Type == lost }) {
		t.Error("the object is among the cleaner's entities")
	}
	reader := member("ann")
	if found := tn.Search(&reader, "Watch", now); len(found) != 0 {
		t.Errorf("ann finds bea's item in search: %+v", found)
	}
	// Field security: only the supervisor (and the builder) read the value.
	value := func(who, id string) string {
		view, err := tn.RecordOf(member(who), lost, id, now)
		if err != nil {
			return err.Code.String()
		}
		raw, _ := json.Marshal(view.Record)
		return string(raw)
	}
	if strings.Contains(value("bea", "L-2"), `"value":900`) || !strings.Contains(value("sam", "L-2"), `"value":900`) {
		t.Errorf("the value is read by the wrong roles: bea %s, sam %s", value("bea", "L-2"), value("sam", "L-2"))
	}
	// Acting follows reading: a record outside your scope is not there to act on.
	if got := do("ann", lost+".edit", lost, "L-2", map[string]any{"claimant": "Mallory"}); !strings.Contains(got, "NOT_FOUND") {
		t.Errorf("ann edits bea's item: %s", got)
	}
	if got := do("ann", lost+".handback", lost, "L-1", map[string]any{"to": "Ada"}); !strings.Contains(got, "POLICY_DENIED") {
		t.Errorf("a desk takes an action that is the supervisor's: %s", got)
	}
	if got := do("sam", lost+".handback", lost, "L-1", map[string]any{"to": "Ada"}); got != "ok" {
		t.Errorf("the supervisor hands it back: %s", got)
	}
	if got := do("ann", lost+".archive", lost, "L-1", map[string]any{}); !strings.Contains(got, "POLICY_DENIED") {
		t.Errorf("a desk archives without archive access: %s", got)
	}
	// The catalog offers each role what it may take.
	offers := func(who, schema string) bool {
		return slices.ContainsFunc(tn.Catalog(member(who)), func(a platform.Action) bool { return a.Schema == schema })
	}
	for _, x := range []struct {
		who, schema string
		want        bool
	}{{"ann", lost + ".create", true}, {"ann", lost + ".handback", false}, {"ann", lost + ".archive", false},
		{"sam", lost + ".create", false}, {"sam", lost + ".handback", true}, {"cy", lost + ".create", false}, {"dana", lost + ".handback", true}} {
		if got := offers(x.who, x.schema); got != x.want {
			t.Errorf("%s is offered %s: %v, want %v", x.who, x.schema, got, x.want)
		}
	}
	CheckReplay(t, tn, journal, compose)
}
