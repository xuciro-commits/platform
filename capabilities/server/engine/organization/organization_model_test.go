package organization

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
)

// TestViews pins the five description views: the profile's whole
// palette is answered by exactly one view's slice, and every view offers only
// relationships the contracts admit among its own elements.
func TestViews(t *testing.T) {
	mm := directory()
	views := Views(mm)
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	seen := map[string]string{}
	for _, v := range views {
		if v.ID == "" || v.Note == "" || len(v.Elements) == 0 {
			t.Fatalf("view %+v is missing its identity or question", v)
		}
		for _, st := range append(v.Elements, v.Context...) {
			if other, dup := seen[st]; dup && st != Organization {
				t.Fatalf("%s appears in both %s and %s", st, other, v.ID)
			}
			seen[st] = v.ID
		}
		if len(v.Relationships) == 0 {
			t.Fatalf("%s derives no relationships", v.ID)
		}
		for _, rel := range v.Relationships {
			if !Vocabulary[rel] {
				t.Fatalf("%s offers %s outside the profile's vocabulary", v.ID, rel)
			}
		}
	}
	for _, p := range Profile() {
		if _, ok := seen[p.Stereotype]; !ok {
			t.Fatalf("%s answers to no view", p.Stereotype)
		}
	}
}

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

func TestRetiredImportPreviewCannotChangeModelOrSource(t *testing.T) {
	m := FromOrgSeed(platform.OrgSeed{Units: []platform.Unit{{ID: "local", Name: "Local"}}})
	source := Slice{Tenant: "source", UAF: "1.3", Elements: []Element{{ID: "root", Stereotype: Organization, Name: "Root"}}}
	before, _ := json.Marshal([]any{m, source})
	if _, err := m.PreviewImport(source, "", source.Fingerprint(), m.Fingerprint(), "2026-01-01"); err == nil {
		t.Fatal("retired slice preview was accepted")
	}
	after, _ := json.Marshal([]any{m, source})
	if string(before) != string(after) {
		t.Fatal("retired preview changed retained model or original source")
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
