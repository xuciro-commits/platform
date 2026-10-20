package platformserver

import "platformserver/internal/construction"

import (
	"reflect"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestSavedDraftChangesRetainPublicationsAndIncludeApplication(t *testing.T) {
	tn := composeTenant(t, "saved-changes", []Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user"), seatOf("publisher", "build:publisher")}, build.New("saved-changes"))
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	typ := publishObject(t, tn, "builder", "O", map[string]any{"name": "visit", "title": "Visit", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": "desk", "title": "Desk", "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: typ}}}, at)
	decide(t, tn, "builder", construction.ID, construction.AppType+".publish", construction.AppType, "APP", map[string]any{}, at)
	member := memberOf(t, tn, "builder")
	initial, err := tn.SavedDraftChanges(member, "APP", "", "")
	if err != nil || len(initial.Changes) != 0 {
		t.Fatalf("unchanged published records reported changes: %+v %v", initial, err)
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"title": "Visit updated"}, at.Add(time.Minute))
	decide(t, tn, "builder", construction.ID, construction.AppType+".edit", construction.AppType, "APP", map[string]any{"description": "Saved application update"}, at.Add(time.Minute))
	before, _ := platform.Get[construction.Object](tn.automation(construction.ID, false), "O")
	if before.State != "published" {
		t.Fatal("test no longer exercises an edited published draft")
	}
	changes, err := tn.SavedDraftChanges(member, "APP", "", "")
	if err != nil || len(changes.Changes) != 2 {
		t.Fatalf("application or edited object missing: %+v %v", changes, err)
	}
	for _, change := range changes.Changes {
		if !change.Published || len(change.Fields) != 1 || change.Changed.By != "builder" {
			t.Fatalf("wrong change metadata: %+v", change)
		}
		if len(change.Fields[0].Before) != 0 || len(change.Fields[0].After) != 0 {
			t.Fatal("inventory copied detailed definition bodies")
		}
	}
	detail, err := tn.SavedDraftChanges(member, "APP", construction.ObjectType, "O")
	if err != nil || len(detail.Changes) != 1 || string(detail.Changes[0].Fields[0].Before) != `"Visit"` || string(detail.Changes[0].Fields[0].After) != `"Visit updated"` {
		t.Fatalf("original field difference: %+v %v", detail, err)
	}
	after, _ := platform.Get[construction.Object](tn.automation(construction.ID, false), "O")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("change discovery changed a draft or its retained publication")
	}
	if _, err := tn.SavedDraftChanges(memberOf(t, tn, "user"), "APP", "", ""); err == nil {
		t.Fatal("business user read builder drafts")
	}
	if _, err := tn.SavedDraftChanges(memberOf(t, tn, "publisher"), "APP", "", ""); err == nil {
		t.Fatal("publisher read unsealed builder drafts instead of saved candidates")
	}
	if _, err := tn.SavedDraftChanges(member, "missing", "", ""); err == nil {
		t.Fatal("missing application silently widened discovery")
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"title": "Visit"}, at.Add(2*time.Minute))
	decide(t, tn, "builder", construction.ID, construction.AppType+".edit", construction.AppType, "APP", map[string]any{"description": ""}, at.Add(2*time.Minute))
	undone, err := tn.SavedDraftChanges(member, "APP", "", "")
	if err != nil || len(undone.Changes) != 0 {
		t.Fatalf("record timestamps were mistaken for definition changes: %+v %v", undone, err)
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"name": "renamedvisit"}, at.Add(3*time.Minute))
	renamed, err := tn.SavedDraftChanges(member, "APP", "", "")
	if err != nil || len(renamed.Changes) != 1 || renamed.Changes[0].Ref.Name != "build.renamedvisit" {
		t.Fatalf("a renamed draft disappeared from its original application: %+v %v", renamed, err)
	}
}
