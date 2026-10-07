package core

import (
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
)

// Every shared type describes cleanly, generates its three standard actions
// for the steward, references only other core types, and is translated.
func TestCoreDeclarations(t *testing.T) {
	entities := Entities()
	if len(entities) != 10 {
		t.Fatalf("expected 10 shared types, got %d", len(entities))
	}
	typeOf := func(rt reflect.Type) string {
		for _, e := range entities {
			if reflect.TypeOf(e.Model) == rt {
				return e.Type
			}
		}
		return "?"
	}
	zh := languages["zh-CN"]
	for _, e := range entities {
		info, err := platform.Describe(ID, e, typeOf)
		if err != nil {
			t.Fatalf("%s: %v", e.Type, err)
		}
		if !strings.HasPrefix(e.Type, ID+".") || len(info.Standard) != 3 && e.Type != JournalType { // books are append-only: a journal entry is created and reversed, never edited
			t.Fatalf("%s: expected core.* with create/edit/archive, got %v", e.Type, info.Standard)
		}
		if zh[e.Title] == "" || zh[e.Description] == "" {
			t.Errorf("%s: title or description not translated", e.Type)
		}
		for _, f := range info.Fields {
			if f.Type == "reference" && !strings.HasPrefix(f.Ref, ID+".") && f.Ref != "enterprise.element" { // the enterprise model is the one layer below (ADR-0067 D8)
				t.Errorf("%s.%s references %s outside core", e.Type, f.Name, f.Ref)
			}
			if zh[f.Title] == "" {
				t.Errorf("%s.%s: label %q not translated", e.Type, f.Name, f.Title)
			}
		}
		if len(e.Seed) > 0 {
			for _, s := range e.Seed {
				if reflect.TypeOf(s) != reflect.TypeOf(e.Model) {
					t.Errorf("%s: seed of type %T", e.Type, s)
				}
			}
		}
	}
	infos := map[string]platform.EntityInfo{}
	for _, e := range entities {
		infos[e.Type], _ = platform.Describe(ID, e, typeOf)
	}
	if err := platform.CheckInterfaces([]platform.Manifest{New("t").Manifest()}, infos); err != nil {
		t.Fatalf("core's own interfaces: %v", err)
	}
	app := New("t")
	for _, a := range app.Manifest().Actions.All() {
		if a.Title == "" || a.Description == "" || len(a.Roles) != 1 || a.Roles[0] != Steward && a.Roles[0] != Accountant {
			t.Errorf("action %s: incomplete or not the steward's or accountant's", a.Schema)
		}
	}
}
