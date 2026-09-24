package platformserver

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// A group with a subsidiary that owns a factory, a business group that manages
// the same factory, a temporary committee and an external partner; one person
// in three structures at once (ADR-0012).
func groupSeed() OrgSeed {
	return OrgSeed{
		Structures: []Structure{{ID: "legal", Name: "Legal", Kind: "legal"}, {ID: "mgmt", Name: "Management", Kind: "management"},
			{ID: "gov", Name: "Governance", Kind: "governance"}},
		Units: []Unit{{ID: "group", Name: "Group", Kind: "group", Legal: true}, {ID: "sub-a", Name: "Subsidiary A", Kind: "subsidiary", Legal: true},
			{ID: "bg-x", Name: "Business group X", Kind: "business group"}, {ID: "factory", Name: "Factory SZ", Kind: "factory"},
			{ID: "line-1", Name: "Line 1", Kind: "line"}, {ID: "safety", Name: "Safety committee", Kind: "committee", Until: "2027-01-01"},
			{ID: "acme", Name: "Acme", Kind: "partner", External: true, Legal: true}},
		Edges: []Edge{{Structure: "legal", Unit: "sub-a", Parent: "group", Relation: "owned by", Share: 1},
			{Structure: "legal", Unit: "factory", Parent: "sub-a", Relation: "owned by", Share: 1},
			{Structure: "mgmt", Unit: "factory", Parent: "bg-x", Relation: "reports to"},
			{Structure: "mgmt", Unit: "line-1", Parent: "factory", Relation: "part of"},
			{Structure: "gov", Unit: "safety", Parent: "group", Relation: "part of"}},
		Memberships: []Membership{{Party: "member:ana", Unit: "bg-x", Role: "director", Primary: true},
			{Party: "member:ana", Unit: "safety", Role: "chair"}, {Party: "member:ana", Unit: "sub-a", Role: "board member"},
			{Party: "unit:acme", Unit: "safety", Role: "observer"}, {Party: "member:bo", Unit: "acme", Role: "employee"}},
	}
}

func TestOrganizationStructures(t *testing.T) {
	o := NewOrganization("t", groupSeed())
	day := "2026-09-24"
	// Ana's scope differs by structure: the factory is hers by management, not by law.
	if got := o.units("member:ana", "mgmt", day); !slices.Equal(got, []string{"bg-x", "safety", "sub-a", "factory", "line-1"}) {
		t.Fatalf("mgmt units %v", got)
	}
	if got := o.units("member:ana", "legal", day); !slices.Contains(got, "factory") || slices.Contains(got, "line-1") {
		t.Fatalf("legal units %v", got)
	}
	// Bo belongs to the committee through his organisation's membership.
	if got := o.units("member:bo", "gov", day); !slices.Equal(got, []string{"acme", "safety"}) {
		t.Fatalf("bo's units %v", got)
	}
	// The temporary committee ends on its date, and with it the scope it gave.
	if got := o.units("member:ana", "gov", "2027-02-01"); slices.Contains(got, "safety") {
		t.Fatalf("a closed committee still gives scope: %v", got)
	}
}

func TestOrganizationDecisions(t *testing.T) {
	dir := NewDirectory("t-1", Seat{Subjects: []string{"ana"}, Member: Member{ID: "ana", Roles: map[string]string{OrgApp: OrgAdmin}}})
	o := NewOrganization("t-1", groupSeed())
	tn, err := NewTenant("t-1", dir, o)
	if err != nil {
		t.Fatal(err)
	}
	ana, _ := dir.Member("ana")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	do := func(schema, target, key string, payload any) string {
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: OrgApp, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: UnitType, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	for _, c := range []struct{ got, want string }{
		{do(SchemaUnitAdd, "proj", "k1", map[string]any{"name": "Offsite 2026", "kind": "project", "until": "2026-12-31"}), "ok"},
		{do(SchemaUnitAdd, "proj", "k2", map[string]any{"name": "Again", "kind": "project"}), "ERROR_CODE_CONFLICT"},
		{do(SchemaPlace, "proj", "k3", map[string]any{"structure": "mgmt", "parent": "bg-x"}), "ok"},
		{do(SchemaPlace, "bg-x", "k4", map[string]any{"structure": "mgmt", "parent": "line-1"}), "ERROR_CODE_INVALID_ARGUMENT"}, // a cycle
		// A reorganisation next month: the factory moves to the group directly.
		{do(SchemaPlace, "factory", "k5", map[string]any{"structure": "mgmt", "parent": "group", "from": "2026-10-01"}), "ok"},
		{do(SchemaJoin, "proj", "k6", map[string]any{"party": "member:bo", "role": "member"}), "ok"},
		{do(SchemaJoin, "proj", "k7", map[string]any{"party": "someone", "role": "member"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{do(SchemaLeave, "proj", "k8", map[string]any{"party": "member:bo", "role": "member", "until": "2026-11-01"}), "ok"},
	} {
		if c.got != c.want {
			t.Fatalf("got %s, want %s", c.got, c.want)
		}
	}
	if got := o.units("member:ana", "mgmt", "2026-09-30"); !slices.Contains(got, "factory") {
		t.Fatalf("before the move ana manages the factory: %v", got)
	}
	if got := o.units("member:ana", "mgmt", "2026-10-02"); slices.Contains(got, "factory") {
		t.Fatalf("after the move the factory is no longer under ana's business group: %v", got)
	}
	if got := o.units("member:bo", "mgmt", "2026-10-15"); !slices.Contains(got, "proj") {
		t.Fatalf("bo in the project: %v", got)
	}
	if got := o.units("member:bo", "mgmt", "2026-11-02"); slices.Contains(got, "proj") {
		t.Fatalf("bo left the project: %v", got)
	}
}
