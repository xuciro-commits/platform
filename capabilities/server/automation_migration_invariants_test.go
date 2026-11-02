package platformserver

import (
	"testing"
	"time"

	"encoding/json"
	"platformserver/engine/automation"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
)

func TestInvariantK1AlertMigrationRetainsHistoryAndRunsAsAutomation(t *testing.T) {
	live := newInvariantTenantWithApps(t, "alert-migration", []platform.App{automation.New("alert-migration")})
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alarmitem", "Alarm item"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alarmitem"}
	asset, err := platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetAlert, Name: "matching"}, ContractVersion: 1, SourceVersion: build.ValueDefinitionVersion + ".alert-1", Body: platform.Raw(platform.AlertRule{Name: "matching", Title: "Matched", Object: object, Field: "note", Operator: "=", Value: "match", Message: "Review item", Role: build.Builder, Active: true})})
	if err != nil {
		t.Fatal(err)
	}
	legacy := build.Alert{Record: platform.Record{ID: "LEGACY", Revision: 1, Created: platform.Stamp{By: "builder", At: invariantClock}, Changed: platform.Stamp{By: "builder", At: invariantClock}}, Name: "matching", Title: "Matched", Object: "build.alarmitem", Field: "note", Operator: "=", Value: "match", Message: "Review item", Role: build.Builder, Active: true, State: "published", Version: 1, DefinitionVersion: 1, Published: string(platform.Raw(asset)), Versions: []string{string(platform.Raw(asset))}}
	raw, _, err := live.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	var baseline tenantState
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	draft := legacy
	draft.ID, draft.Name, draft.State, draft.Published = "LEGACY-DRAFT", "matchingdraft", "draft", ""
	draft.Version, draft.DefinitionVersion, draft.Versions = 0, 0, nil
	baseline.Records[build.AlertType] = []records.StoredState{{Value: platform.Raw(legacy)}, {Value: platform.Raw(draft)}}
	baseline.Checkpoint = nil
	snapshot := platform.Raw(baseline)
	live = newInvariantTenantWithApps(t, live.ID, []platform.App{automation.New(live.ID)})
	if err := live.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	prefix := len(live.entries)
	live.Work(invariantClock.Add(time.Second))
	p, ok := platform.Get[build.Process](live.automation(build.ID, false), "alert-LEGACY")
	if !ok || p.Kind != "automation" || p.Trigger == nil || p.Version != 1 || p.Scheduler != "builder" {
		work, _ := live.works.Get("job:build/automations")
		t.Fatalf("legacy migration: %+v found=%v work=%+v", p, ok, work)
	}
	convertedDraft, ok := platform.Get[build.Process](live.automation(build.ID, false), "alert-LEGACY-DRAFT")
	if !ok || convertedDraft.State != "draft" || convertedDraft.Version != 0 || convertedDraft.Published != "" {
		t.Fatalf("draft migration: %+v", convertedDraft)
	}
	old, _ := platform.Get[build.Alert](live.automation(build.ID, false), "LEGACY")
	if old.Migrated != p.ID || old.Published != legacy.Published || old.Versions[0] != legacy.Versions[0] {
		t.Fatal("migration rewrote historical alert bytes")
	}
	live.accept(t, "build.alarmitem.create", "build.alarmitem", "ITEM", map[string]any{"note": "match"})
	live.Work(invariantClock.Add(2 * time.Second))
	if notices := live.notices.ForMember("builder"); len(notices) != 1 || notices[0].Key != "alert:LEGACY:ITEM" {
		t.Fatalf("migrated notification/dedup: %+v", notices)
	}
	live.accept(t, "build.alarmitem.edit", "build.alarmitem", "ITEM", map[string]any{"note": "match"})
	live.Work(invariantClock.Add(3 * time.Second))
	if notices := live.notices.ForMember("builder"); len(notices) != 1 {
		t.Fatalf("once-per-record legacy alert produced %d notices", len(notices))
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFlow, Name: "build." + p.Name}
	if _, err := live.ReleaseCandidate([]platform.AssetRef{ref}); err != nil {
		t.Fatalf("migrated automation cannot be delivered: %v", err)
	}
	live.accept(t, build.SchemaProcess, build.ProcessType, convertedDraft.ID, map[string]any{})
	live.accept(t, build.SchemaProcessPause, build.ProcessType, convertedDraft.ID, map[string]any{})
	want := live.recordImage(t)
	for range 2 {
		restored := newInvariantTenantWithApps(t, live.ID, []platform.App{automation.New(live.ID)})
		if err := restored.Restore(snapshot); err != nil {
			t.Fatal(err)
		}
		if err := restored.recoverEntries(live.entries[prefix:]); err != nil {
			t.Fatal(err)
		}
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("migration replay differs in %v", differing)
		}
		if len(restored.notices.ForMember("builder")) != 1 {
			t.Fatal("migration replay duplicated the notification")
		}
	}
}
