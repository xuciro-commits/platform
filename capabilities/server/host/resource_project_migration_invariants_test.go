package host

import (
	"encoding/json"
	"testing"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantLegacyActionTypesFollowMigratedObjectProject(t *testing.T) {
	live := newInvariantTenant(t, "invariant-resource-project-migration")
	live.accept(t, "build.app.edit", build.AppType, invariantProject, map[string]any{
		"name": "invariantproject", "title": "Invariant project",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}},
	})
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	raw, _, err := live.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	var baseline tenantState
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	for i, state := range baseline.Records[build.ObjectType] {
		var object build.Object
		if json.Unmarshal(state.Value, &object) != nil || object.ID != "OBJECT" {
			continue
		}
		object.Actions = []build.Action{{Name: "approve", Title: "Approve", From: []string{"open"}}}
		object.ActionTypes = nil
		baseline.Records[build.ObjectType][i].Value = platform.Raw(object)
	}
	for i, state := range baseline.Records[build.ResourceType] {
		var resource build.Resource
		if json.Unmarshal(state.Value, &resource) != nil || resource.Type != build.ObjectType || resource.Draft != "OBJECT" {
			continue
		}
		resource.Project = build.WorkspaceProject
		baseline.Records[build.ResourceType][i].Value = platform.Raw(resource)
	}
	baseline.Records[build.ActionTypeType] = nil
	baseline.Checkpoint = nil
	snapshot := platform.Raw(baseline)

	restored := newInvariantTenantReplayWithApps(t, live.ID, nil)
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.initializeResources(invariantClock); err != nil {
		t.Fatalf("initialize resources: %#v", err)
	}
	if project := restored.resourceProject(build.ObjectType, "OBJECT"); project != invariantProject {
		t.Fatalf("legacy object project = %q; want %q", project, invariantProject)
	}
	action, found := platform.Get[build.ActionType](restored.branchCaller(restored.member(t)), "build.alpha.approve")
	if !found || action.Version != 1 {
		t.Fatalf("legacy action type was not extracted: %+v found=%v", action, found)
	}
	if project := restored.resourceProject(build.ActionTypeType, action.ID); project != invariantProject {
		t.Fatalf("extracted action type project = %q; want %q", project, invariantProject)
	}
	if err := restored.initializeResources(invariantClock); err != nil {
		t.Fatalf("repeat initialize resources: %v", err)
	}
	again, _ := platform.Get[build.ActionType](restored.branchCaller(restored.member(t)), "build.alpha.approve")
	if again.Version != action.Version || again.Record.Revision != action.Record.Revision {
		t.Fatalf("resource migration is not idempotent: first=%+v second=%+v", action, again)
	}
}
