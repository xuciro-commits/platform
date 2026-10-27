package enterprise

import (
	"platformserver/tools/uaf"
	"testing"
)

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
