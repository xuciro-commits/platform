package enterprise

import (
	"platformserver/internal/enterprise/uaf"
	"testing"
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
	if m, _ := Template(SeedParams{Scale: "M", Name: "Acme", Day: "2026-01-01"}); len(m.UnitsIn("org-line-1-1", "management", "2026-02-01")) != 0 {
		// a line has nothing below it in management
		t.Fatal("line has children")
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

// TestViews pins the five description views: the profile's whole
// palette is answered by exactly one view's slice, and every view offers only
// relationships the contracts admit among its own elements.
func TestViews(t *testing.T) {
	mm := uaf.Current()
	views := Views(mm)
	if len(views) != 5 {
		t.Fatalf("got %d views, want 5", len(views))
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
