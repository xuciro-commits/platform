package enterprise

import (
	"encoding/json"
	"testing"
	"time"

	"platformkernel/kernel"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestTemplates(t *testing.T) {
	for _, scale := range []string{"S", "M", "L", "XL"} {
		m, err := Template(SeedParams{Scale: scale, Name: "Acme", Day: "2026-01-01"})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, e := range m.Elements {
			if ids[e.ID] {
				t.Fatalf("%s: duplicate id %s", scale, e.ID)
			}
			ids[e.ID] = true
		}
		for _, r := range m.Relationships {
			if !ids[r.Source] || !ids[r.Target] {
				t.Fatalf("%s: dangling relationship %+v", scale, r)
			}
		}
		if len(m.Views) < 2 || len(m.Kinds) < 2 {
			t.Fatalf("%s: %d views %d kinds", scale, len(m.Views), len(m.Kinds))
		}
		t.Logf("%s: %d elements, %d relationships", scale, len(m.Elements), len(m.Relationships))
	}
	if m, _ := Template(SeedParams{Scale: "M", Name: "Acme", Day: "2026-01-01"}); len(m.units("org-line-1-1", "management", "2026-02-01")) != 0 {
		// a line has nothing below it in management
		t.Fatal("line has children")
	}
}

func TestOrgProjection(t *testing.T) {
	seed := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}},
		Units:       []platform.Unit{{ID: "plant", Name: "Plant", Kind: "factory", From: "2020-01-01"}, {ID: "l1", Name: "Line 1", Kind: "line", From: "2020-01-01"}},
		Edges:       []platform.Edge{{Structure: "site", Unit: "l1", Parent: "plant", From: "2020-01-01"}},
		Memberships: []platform.Membership{{Party: "member:ann", Unit: "plant", Role: "manager", From: "2020-01-01"}, {Party: "member:bob", Unit: "l1", Role: "operator", From: "2020-01-01"}}}
	e := New("t-1", seed)
	if got := e.Units("member:ann", "site", "2026-01-01"); len(got) != 2 {
		t.Fatalf("ann's units %v", got)
	}
	if got := e.Units("member:bob", "", "2026-01-01"); len(got) != 1 || got[0] != "l1" {
		t.Fatalf("bob's units %v", got)
	}
	if got := e.Holders("site", "l1", "", "2026-01-01"); len(got) != 2 {
		t.Fatalf("holders %v", got)
	}
	back := e.Model().OrgSeed()
	if len(back.Units) != 2 || len(back.Edges) != 1 || len(back.Memberships) != 2 || back.Memberships[0].Party != "member:ann" {
		t.Fatalf("projection %+v", back)
	}
	// a decision: add a post and fill it
	c := platform.Caller{Tenant: "t-1", ID: "ann", App: ID, Roles: map[string]string{ID: Admin}}
	sub := func(schema, id string, raw []byte) *pb.Submission {
		typ := ElementType
		if schema == SchemaRelationshipAdd {
			typ = RelationshipType
		}
		return &pb.Submission{TenantId: "t-1", PrincipalId: "ann", Authority: ID, IdempotencyKey: schema + id, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw}
	}
	submit := func(schema, id string, body any) *pb.ChangeRecord {
		raw, _ := json.Marshal(body)
		rec, err := e.Submit(c, sub(schema, id, raw), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
		return rec
	}
	submit(SchemaElementAdd, "post-1", map[string]any{"stereotype": Post, "name": "Line lead"})
	submit(SchemaElementAdd, "cap-1", map[string]any{"stereotype": Capability, "name": "Assemble", "properties": map[string]any{"kind": "Operational"}})
	submit(SchemaRelationshipAdd, "r-1", map[string]any{"stereotype": Performs, "source": "l1", "target": "cap-1"})
	raw, _ := json.Marshal(map[string]any{"stereotype": Capability, "name": "Bad", "properties": map[string]any{"kind": "Nope"}})
	if _, err := e.Submit(c, sub(SchemaElementAdd, "cap-2", raw), time.Now()); err == nil {
		t.Fatal("a bad enumeration literal passed")
	}
	if got := e.Model().Of("l1", Performs, true, "2026-02-01"); len(got) != 1 || got[0] != "cap-1" {
		t.Fatalf("capabilities of l1 %v", got)
	}
	// what the host's Directory answers the apps (ADR-0067 D3)
	if info, ok := e.Element("l1", "2026-02-01"); !ok || info.Stereotype != Organization || info.Kind != "line" {
		t.Fatalf("element l1 %+v %v", info, ok)
	}
	if _, ok := e.Element("l1", "2019-01-01"); ok {
		t.Fatal("l1 is live before it starts")
	}
	if got := e.Related("cap-1", Performs, false, "2026-02-01"); len(got) != 1 || got[0] != "l1" {
		t.Fatalf("performers of cap-1 %v", got)
	}
}

// Federation (ADR-0067 D5): a subsidiary publishes, the group imports read-only.
func TestFederation(t *testing.T) {
	sub := New("sub", platform.OrgSeed{})
	grp := New("grp", platform.OrgSeed{})
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	do := func(e *Enterprise, tenant, schema, typ, id string, body any) *kernel.Error {
		raw, _ := json.Marshal(body)
		c := platform.Caller{Tenant: tenant, ID: "ann", App: ID, Roles: map[string]string{ID: Admin}}
		_, err := e.Submit(c, &pb.Submission{TenantId: tenant, PrincipalId: "ann", Authority: ID, IdempotencyKey: schema + id, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw}, at)
		return err
	}
	must := func(err *kernel.Error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%v: %s", err, err.Message)
		}
	}
	must(do(sub, "sub", SchemaElementAdd, ElementType, "co", map[string]any{"stereotype": Organization, "name": "Acme Industrial", "legal": true}))
	must(do(sub, "sub", SchemaElementAdd, ElementType, "hr", map[string]any{"stereotype": Organization, "name": "HR"}))
	must(do(sub, "sub", SchemaElementAdd, ElementType, "cap", map[string]any{"stereotype": Capability, "name": "Machining"}))
	must(do(sub, "sub", SchemaRelationshipAdd, RelationshipType, "r1", map[string]any{"stereotype": Performs, "source": "co", "target": "cap"}))
	published := true
	for _, id := range []string{"co", "cap"} {
		must(do(sub, "sub", SchemaElementEdit, ElementType, id, map[string]any{"published": published}))
	}
	slice, _ := sub.Read(platform.Caller{}, ReadPublished)
	sl := slice.(Slice)
	if len(sl.Elements) != 2 || len(sl.Relationships) != 1 || sl.Elements[0].Owner != "tenant:sub" {
		t.Fatalf("slice %+v", sl)
	}
	must(do(grp, "grp", SchemaElementAdd, ElementType, "group", map[string]any{"stereotype": Organization, "name": "Acme Group", "legal": true}))
	must(do(grp, "grp", SchemaKindAdd, ModelType, "legal", map[string]any{"name": "Ownership", "kind": "legal"}))
	must(do(grp, "grp", SchemaSliceImport, ModelType, "sub", map[string]any{"slice": sl}))
	must(do(grp, "grp", SchemaRelationshipAdd, RelationshipType, "own", map[string]any{"stereotype": Placement, "kind": "legal", "source": "co", "target": "group", "share": 1}))
	if err := do(grp, "grp", SchemaElementEdit, ElementType, "co", map[string]any{"name": "Renamed"}); err == nil {
		t.Fatal("a mirrored element was edited in the group")
	}
	if got := grp.Units("", "legal", "2026-02-01"); len(got) != 0 {
		t.Fatalf("units %v", got)
	}
	if !grp.Model().below("legal", "group", "co", "2026-02-01") {
		t.Fatal("the subsidiary is not below the group in the legal kind")
	}
	// a second import with the capability unpublished drops it, keeps the group's own ownership edge
	sl.Elements = sl.Elements[:1]
	sl.Relationships = nil
	must(do(grp, "grp", SchemaSliceImport, ModelType, "sub-2", map[string]any{"slice": sl}))
	if m := grp.Model(); len(m.Elements) != 2 || len(m.Relationships) != 1 || m.Relationships[0].ID != "own" {
		t.Fatalf("after re-import: %d elements %+v", len(m.Elements), m.Relationships)
	}
}

func TestPatternsGraft(t *testing.T) {
	m, err := Template(SeedParams{Scale: "M", Industry: "manufacturing", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	root := ""
	for _, el := range m.Elements {
		if el.Stereotype == Organization && len(m.Of(el.ID, Placement, true, "2026-10-06")) == 0 {
			root = el.ID
		}
	}
	if root == "" {
		t.Fatal("template has no root organisation")
	}
	before := len(m.Elements)
	for _, p := range Patterns() {
		if p.Params == nil {
			t.Fatalf("%s: catalogue must encode params as an array for the modeler", p.ID)
		}
		if _, err := m.Apply(p.ID, root, "New "+p.Title, nil, "2026-10-06"); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
	}
	if len(m.Elements) <= before {
		t.Fatal("patterns added nothing")
	}
	if _, err := m.Apply("nope", "", "x", nil, ""); err == nil {
		t.Fatal("unknown pattern accepted")
	}
	if pv, ok := PatternPreview("hotel", "", Params{"floors": 12}); !ok || len(pv.Outline) == 0 {
		t.Fatal("hotel preview empty")
	}
}
