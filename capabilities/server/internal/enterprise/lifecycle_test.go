package enterprise

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
)

func TestLifecycleInvalidDatesLeavePeriodsUnchanged(t *testing.T) {
	closing := Element{ID: "closing", From: "2026-01-01"}
	reopening := Element{ID: "reopening", From: "2026-01-01", Until: "2026-02-01", Closed: "Paused"}
	ending := Relationship{ID: "ending", From: "2026-01-01"}
	for _, tc := range []struct {
		name   string
		value  any
		change func() error
	}{
		{"close", &closing, func() error { return closing.CloseOn("2026-13-01", "Retire") }},
		{"reopen", &reopening, func() error { return reopening.ReopenOn("2026-13-01") }},
		{"end relationship", &ending, func() error { return ending.EndOn("2026-13-01") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.value)
			if err := tc.change(); err == nil {
				t.Fatal("invalid calendar date was accepted")
			}
			after, _ := json.Marshal(tc.value)
			if string(before) != string(after) {
				t.Fatal("invalid date changed the effective periods")
			}
		})
	}
}

func TestLinkedImportIsPureNamespacedAndRetainsWithdrawnHistory(t *testing.T) {
	m := FromOrgSeed(platform.OrgSeed{Units: []platform.Unit{{ID: "local", Name: "Local"}}, Memberships: []platform.Membership{{Party: "member:ann", Unit: "local", Role: "employee"}}})
	m.TenantLinks = []TenantLink{{ID: "sub", Organization: "local", Tenant: "subtenant", Root: "root", Connection: "CONNECTION"}}
	slice := Slice{Tenant: "subtenant", UAF: "1.3", Elements: []Element{{ID: "root", Stereotype: Organization, Name: "Shared root"}, {ID: "cap", Stereotype: Capability, Name: "Shared capability"}}, Relationships: []Relationship{{ID: "ability", Stereotype: Performs, Source: "root", Target: "cap"}}}
	before, _ := json.Marshal(slice)
	next, err := m.PreviewImport(slice, "sub", slice.Fingerprint(), m.Fingerprint(), "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(slice)
	if string(before) != string(after) {
		t.Fatal("preview mutated source slice")
	}
	root := ImportedElementID(slice.Tenant, "root")
	cap := ImportedElementID(slice.Tenant, "cap")
	if next.ElementOf(root) == nil || next.TenantLinkOf("sub").Content == "" || len(next.Relationships) != 2 {
		t.Fatal("linked import lost namespace, metadata or local membership")
	}
	slice.Elements = slice.Elements[:1]
	slice.Relationships = nil
	withdrawn, err := next.PreviewImport(slice, "sub", slice.Fingerprint(), next.Fingerprint(), "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.ElementOf(cap) == nil || withdrawn.LiveOn(cap, "2026-02-15") || !withdrawn.LiveOn(cap, "2026-01-15") {
		t.Fatal("withdrawal erased earlier history")
	}
	if _, err := withdrawn.PreviewImport(slice, "sub", slice.Fingerprint(), m.Fingerprint(), "2026-03-01"); err == nil {
		t.Fatal("stale preview applied")
	}
}

func TestSourceSyncReopeningPreservesElementAndRelationshipGaps(t *testing.T) {
	m := FromOrgSeed(platform.OrgSeed{})
	elements := []Element{{ID: "team", Stereotype: Organization, Name: "Team"}, {ID: "cap", Stereotype: Capability, Name: "Capability"}}
	relationships := []Relationship{{ID: "ability", Stereotype: Performs, Source: "team", Target: "cap"}}
	m.Sync("hr", elements, relationships, "2026-01-01")
	m.Sync("hr", nil, nil, "2026-02-01")
	m.Sync("hr", elements, relationships, "2026-03-01")
	for day, want := range map[string]bool{"2026-01-15": true, "2026-02-15": false, "2026-03-15": true} {
		if m.LiveOn("team", day) != want || m.RelationshipOf("sync:hr:ability").ActiveOn(day) != want {
			t.Fatalf("source gap was lost on %s", day)
		}
	}
}
