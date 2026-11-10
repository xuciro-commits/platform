package host

import (
	"encoding/json"
	"testing"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantUnplacedResourceFollowsItsOnlyApplication(t *testing.T) {
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
	for i, state := range baseline.Records[build.ResourceType] {
		var resource build.Resource
		if json.Unmarshal(state.Value, &resource) != nil || resource.Type != build.ObjectType || resource.Draft != "OBJECT" {
			continue
		}
		resource.Project = build.WorkspaceProject
		baseline.Records[build.ResourceType][i].Value = platform.Raw(resource)
	}
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
		t.Fatalf("object project = %q; want %q", project, invariantProject)
	}
	before := resourceRecord(t, restored, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"})
	if err := restored.initializeResources(invariantClock); err != nil {
		t.Fatalf("repeat initialize resources: %v", err)
	}
	if after := resourceRecord(t, restored, before.Ref); after.Version != before.Version || after.Record.Revision != before.Record.Revision {
		t.Fatalf("resource migration is not idempotent: first=%+v second=%+v", before, after)
	}
}
