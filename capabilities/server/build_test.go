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

// ADR-0034: someone defines an object in the tenant, publishes it, and people
// work with its records on the machinery that is already there — generated
// actions, the record store's scope and search, aggregates, the journal. The
// object then gains a field and its records keep what they had.
func TestTenantDefinedObject(t *testing.T) {
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
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: build.ID, IdempotencyKey: fmt.Sprint("b", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			if err.Message != "" {
				return err.Error() + ": " + err.Message
			}
			return err.Error()
		}
		return "ok"
	}
	fields := []map[string]any{
		{"name": "guest", "title": "Guest", "type": "text", "required": true, "search": true},
		{"name": "visited", "title": "Visited on", "type": "date"},
		{"name": "spend", "title": "Spend", "type": "money"},
		{"name": "kind", "title": "Kind", "type": "choice", "choices": "tour, tasting"},
	}
	// A definition the host could not install is refused while it is a draft, with the reason.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1",
		map[string]any{"name": "Visit", "title": "Visit", "fields": fields}); got != "ERROR_CODE_INVALID_ARGUMENT: the name \"Visit\" is not lower-case letters and digits" {
		t.Fatalf("a name that is not a name: %s", got)
	}
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "colour"}}}); got != "ERROR_CODE_INVALID_ARGUMENT: the field \"guest\" has no type \"colour\"" {
		t.Fatalf("a field type the platform has not: %s", got)
	}
	// The draft, then published.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1",
		map[string]any{"name": "visit", "title": "Visit", "plural": "Visits", "description": "A guest's visit.", "fields": fields}); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	if _, err := tn.Records(member("eli"), build.TypeOf("visit"), platform.Query{}, now); err == nil {
		t.Error("a draft was already installed")
	}
	if got := do("eli", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got == "ok" {
		t.Error("someone without the builder role published an object")
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish: %s", got)
	}

	// It is now an ordinary type: declared, in the member's entities and catalog.
	visit := build.TypeOf("visit")
	if !slices.ContainsFunc(tn.Entities(member("eli")), func(info platform.EntityInfo) bool { return info.Type == visit }) {
		t.Fatalf("the published object is not among eli's entities: %+v", tn.Entities(member("eli")))
	}
	if !slices.ContainsFunc(tn.Catalog(member("eli")), func(a platform.Action) bool { return a.Schema == visit+".create" }) {
		t.Error("eli cannot create records of the published object")
	}
	// A user keeps records of it, through the generated actions.
	if got := do("eli", visit+".create", visit, "V-1",
		map[string]any{"guest": "Ada Lovelace", "visited": "2026-10-02", "kind": "tour", "spend": map[string]any{"amount": 12000, "currency": "EUR"}}); got != "ok" {
		t.Fatalf("a record of the defined object: %s", got)
	}
	if got := do("eli", visit+".create", visit, "V-2", map[string]any{"guest": "Alan Turing", "kind": "coffee"}); got == "ok" {
		t.Error("a value outside the declared choices was stored")
	}
	if got := do("eli", visit+".create", visit, "V-2", map[string]any{"guest": "Alan Turing", "kind": "tasting"}); got != "ok" {
		t.Fatalf("second record: %s", got)
	}
	page, err := tn.Records(member("eli"), visit, platform.Query{Search: "Ada", Sort: []string{"id"}}, now)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("search over the defined object: %+v, %v", page, err)
	}
	reader := member("eli")
	if found := tn.Search(&reader, "Turing", now); len(found) != 1 || found[0].Type != visit {
		t.Errorf("the tenant's own object is not in search: %+v", found)
	}
	// Its page is an asset like a code page's (ADR-0032).
	defs := tn.Definitions(member("eli"))
	if !slices.ContainsFunc(defs, func(d platform.Definition) bool {
		return d.Ref.Kind == platform.AssetPage && d.Ref.Name == "visit" && d.Page != nil && d.Page.Object.Name == visit && d.Source == "tenant"
	}) {
		t.Errorf("the defined object has no page in the registry: %+v", defs)
	}

	// Published again with another field: the records keep what they had.
	grown := append(slices.Clone(fields), map[string]any{"name": "note", "title": "Note", "type": "longtext"})
	if got := do("dana", build.ObjectType+".edit", build.ObjectType, "O-1", map[string]any{"fields": grown}); got != "ok" {
		t.Fatalf("edit: %s", got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish again: %s", got)
	}
	if got := do("eli", visit+".edit", visit, "V-1", map[string]any{"note": "Asked about the tasting."}); got != "ok" {
		t.Fatalf("the new field: %s", got)
	}
	view, err := tn.RecordOf(member("eli"), visit, "V-1", now)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	kept, _ := json.Marshal(view.Record)
	for _, want := range []string{`"guest":"Ada Lovelace"`, `"visited":"2026-10-02"`, `"note":"Asked about the tasting."`, `"amount":12000`} {
		if !strings.Contains(string(kept), want) {
			t.Errorf("after the object grew, the record lost %s: %s", want, kept)
		}
	}
	CheckReplay(t, tn, journal, compose)
}
