package platformserver

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestApplicationStudioGroupingPreservesDefinitionsAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "application-studio") }
	tn := compose()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "case", "title": "Case", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.case"}
	for _, name := range []string{"desk", "other"} {
		decide(t, tn, "builder", build.ID, "build.app.create", build.AppType, name, map[string]any{"name": name, "title": name, "resources": []platform.AssetRef{object}}, at)
		decide(t, tn, "builder", build.ID, "build.app.publish", build.AppType, name, map[string]any{}, at)
	}
	member := memberOf(t, tn, "builder")
	leafBytes := func() []byte {
		var definitions []platform.Definition
		for _, definition := range tn.Definitions(member) {
			if definition.Ref.Kind != platform.AssetApp {
				definitions = append(definitions, definition)
			}
		}
		return platform.Raw(definitions)
	}
	before := leafBytes()
	groups := []platform.AppConcept{{ID: "work", Title: "Customer work", Assets: []platform.AssetRef{object}}, {ID: "master", Title: "Master data", Assets: []platform.AssetRef{}}}
	decide(t, tn, "builder", build.ID, "build.app.edit", build.AppType, "desk", map[string]any{"concepts": groups}, at)
	groups[0].Title = "Service cases"
	groups[0].Assets = []platform.AssetRef{}
	groups[1].Assets = []platform.AssetRef{object}
	decide(t, tn, "builder", build.ID, "build.app.edit", build.AppType, "desk", map[string]any{"concepts": groups}, at)
	decide(t, tn, "builder", build.ID, "build.app.publish", build.AppType, "desk", map[string]any{}, at)
	if !bytes.Equal(before, leafBytes()) {
		t.Fatal("moving or renaming a business group changed asset definition bytes")
	}
	for _, definition := range tn.Definitions(member) {
		if definition.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "desk"}) {
			if !reflect.DeepEqual(definition.Application.Concepts, groups) || !reflect.DeepEqual(definition.Requires, []platform.AssetRef{object}) {
				t.Fatalf("application metadata or semantic membership changed: %+v", definition)
			}
		}
	}
	if got := refuse(t, tn, "user", build.ID, "build.app.edit", build.AppType, "desk", map[string]any{"concepts": groups}, at); got == "ok" {
		t.Fatal("grouping granted a business user builder permissions")
	}
	for _, name := range []string{"desk", "other"} {
		preview, err := tn.PreviewRelease(member, platform.AssetApp, name)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview %s: %+v %v", name, preview, err)
		}
		if _, err := tn.SaveReleaseCandidate(member, platform.AssetApp, name, preview.CandidateID, "save-"+name, at); err != nil {
			t.Fatal(err)
		}
	}
	page, err := tn.SavedReleases(member, 0, 1, "desk")
	if err != nil || page.Total != 1 || len(page.Candidates) != 1 || page.Candidates[0].Title != "desk" {
		t.Fatalf("application candidate filter: %+v %v", page, err)
	}
	page, err = tn.SavedReleases(member, 1, 1, "desk")
	if err != nil || page.Total != 1 || len(page.Candidates) != 0 {
		t.Fatalf("filter must precede pagination: %+v %v", page, err)
	}
	CheckReplay(t, tn, entries, compose)
}
