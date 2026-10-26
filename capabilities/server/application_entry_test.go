package platformserver

import (
	"slices"
	"testing"

	"platformserver/apps/build"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestApplicationEntryWithdrawsAndRestoresRetainedDefinitions(t *testing.T) {
	compose := func() *Tenant { return builderTenant(t, "entry-lifecycle") }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "entryitem", "title": "Item", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": "entrydesk", "title": "Original application", "pages": []string{"entryitem"}}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.SchemaHandOver, construction.AppType, "APP", map[string]any{}, txnAt)
	user := memberOf(t, tn, "user")
	entry := func() []ApplicationView {
		return slices.DeleteFunc(tn.Applications(user), func(app ApplicationView) bool { return app.Name != "entrydesk" })
	}
	if apps := entry(); len(apps) != 1 {
		t.Fatalf("published application unavailable: %+v", apps)
	}
	original, _ := platform.Get[build.Application](tn.automation(construction.ID, false), "APP")
	decide(t, tn, "builder", construction.ID, construction.AppType+".edit", construction.AppType, "APP", map[string]any{"title": "Unpublished title"}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.AppType+".withdraw", construction.AppType, "APP", map[string]any{}, txnAt)
	withdrawn, _ := platform.Get[build.Application](tn.automation(construction.ID, false), "APP")
	if withdrawn.State != "withdrawn" || !tn.applicationEntryWithdrawn(platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "entrydesk"}) {
		t.Fatalf("entry withdrawal was not projected from its owner: %+v", withdrawn)
	}
	if apps := entry(); len(apps) != 0 {
		t.Fatalf("withdrawn entry still offered: %+v", apps)
	}
	if _, refusal := tn.PageView(user, "entrydesk", construction.ID, "entryitem"); refusal == nil {
		t.Fatal("old application deep link remains open")
	}
	decide(t, tn, "builder", construction.ID, construction.AppType+".restore", construction.AppType, "APP", map[string]any{}, txnAt)
	if apps := entry(); len(apps) != 1 || apps[0].Title != "Original application" {
		t.Fatalf("restore published the mutable draft: %+v", apps)
	}
	restored, _ := platform.Get[build.Application](tn.automation(construction.ID, false), "APP")
	if restored.Published != original.Published || restored.Title != "Unpublished title" {
		t.Fatal("entry lifecycle rewrote retained content or discarded the draft")
	}
	CheckReplay(t, tn, entries, compose)
}
