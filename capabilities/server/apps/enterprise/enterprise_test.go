package enterprise

import (
	"encoding/json"
	"testing"
	"time"

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
}
