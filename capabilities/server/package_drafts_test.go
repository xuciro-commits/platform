package platformserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestPackageBlueprintCreatesOnlyDraftsAndRecovers(t *testing.T) {
	at := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	source := builderTenant(t, "blueprint-source")
	decide(t, source, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "object", map[string]any{"name": "item", "title": "Item", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}}, at)
	decide(t, source, "builder", build.ID, build.TestPlanType+".create", build.TestPlanType, "scenario", map[string]any{"name": "createitem", "title": "Create item", "roots": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.item"}}, "at": at, "steps": []platform.TestStep{{Type: "build.item", ID: "sample", Action: "build.item.create", Payload: `{"title":"Example"}`, Expect: "accepted"}}}, at)
	decide(t, source, "builder", build.ID, build.AppType+".create", build.AppType, "application", map[string]any{"name": "inventory", "title": "Inventory", "pages": []string{"item"}, "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetTestPlan, Name: "createitem"}}}, at)
	source.mu.Lock()
	preview, candidate, err := source.previewReleaseLocked([]build.JointDraftRef{{Kind: platform.AssetObject, ID: "object"}, {Kind: platform.AssetApp, ID: "application"}, {Kind: platform.AssetTestPlan, ID: "scenario"}}, memberOf(t, source, "builder"))
	source.mu.Unlock()
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("owner candidate: %+v %v", preview, err)
	}
	descriptor := PackageDescriptor{ID: "example.inventory", Version: "1.0.0", Namespace: "example.inventory", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "inventory"}}, Assets: candidate.Assets}}
	directory := t.TempDir()
	raw, _ := json.Marshal(descriptor)
	path := filepath.Join(directory, "inventory.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant {
		tn := composeTenant(t, "blueprint-target", []Seat{seatOf("admin", PlatformApp+":"+Admin), seatOf("builder", build.ID+":"+build.Builder), seatOf("user", build.ID+":"+build.User)}, build.New("blueprint-target"))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	target := compose()
	var entries []Entry
	fail := false
	target.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, target, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	builder := memberOf(t, target, "builder")
	if _, err = target.CreatePackageDrafts(memberOf(t, target, "admin"), descriptor.ID, descriptor.Version, "no-builder", at); err == nil {
		t.Fatal("package admin created Build drafts without Build permission")
	}
	// A directory edit cannot replace the installed, sealed blueprint.
	for i := range descriptor.Blueprint.Assets {
		if descriptor.Blueprint.Assets[i].Ref.Kind == platform.AssetApp {
			var body map[string]any
			_ = json.Unmarshal(descriptor.Blueprint.Assets[i].Body, &body)
			body["title"] = "Uninstalled change"
			descriptor.Blueprint.Assets[i].Body = platform.Raw(body)
		}
	}
	raw, _ = json.Marshal(descriptor)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err = target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at); err == nil {
		t.Fatal("failed append created an application")
	}
	if _, ok := platform.Get[build.Application](target.automation(build.ID, false), "inventory"); ok {
		t.Fatal("append failure leaked an application draft")
	}
	fail = false
	limited := builder
	limited.Scopes = []string{build.AppType + ".create"}
	if _, err = target.CreatePackageDrafts(limited, descriptor.ID, descriptor.Version, "limited", at); err == nil {
		t.Fatal("one allowed create bypassed another draft's scope")
	}
	answer, err := target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at)
	if err != nil {
		t.Fatal(err)
	}
	if answer.ApplicationID != "inventory" || len(answer.Drafts) != 3 || len(entries) != 2 {
		t.Fatalf("created drafts: %+v entries=%d", answer, len(entries))
	}
	app, ok := platform.Get[build.Application](target.automation(build.ID, false), answer.ApplicationID)
	if !ok || app.Title != "Inventory" || app.Published != "" || app.State != "draft" {
		t.Fatalf("created application is not the sealed draft: %+v", app)
	}
	object, ok := platform.Get[build.Object](target.automation(build.ID, false), "item")
	if !ok || object.Published != "" || object.State != "draft" {
		t.Fatalf("created object is not a draft: %+v", object)
	}
	if _, ok := target.entity("build.item"); ok || target.ActiveRelease() != "" {
		t.Fatal("package creation published definitions")
	}
	plan, ok := platform.Get[build.TestPlan](target.automation(build.ID, false), "createitem")
	if !ok || len(plan.Roots) != 1 || plan.Object != "" || plan.Application != "" || plan.Published != "" {
		t.Fatalf("portable scenario draft: %+v", plan)
	}
	if got, refusal := target.Read(builder, "packages"); refusal != nil || len(got.([]PackageView)) != 1 {
		t.Fatalf("builder cannot discover installed blueprint: %v", refusal)
	}
	if _, refusal := target.Read(memberOf(t, target, "user"), "packages"); refusal == nil {
		t.Fatal("business user discovered builder packages")
	}
	if roles := memberOf(t, target, "builder").Roles; len(roles) != 1 || roles[build.ID] != build.Builder {
		t.Fatalf("package granted roles: %v", roles)
	}
	retried, err := target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(retried, answer) || len(entries) != 2 {
		t.Fatalf("same key did not reuse created IDs: %+v %v", retried, err)
	}
	if _, err = target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "another", at); err == nil || !strings.Contains(err.Error(), "already has a draft") {
		t.Fatalf("existing asset was not reported: %v", err)
	}
	recovered := compose()
	recovered.Files = target.files()
	if err = recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	again, err := recovered.CreatePackageDrafts(memberOf(t, recovered, "builder"), descriptor.ID, descriptor.Version, "create", at.Add(2*time.Hour))
	if err != nil || !reflect.DeepEqual(again, answer) {
		t.Fatalf("recovered package retry changed IDs: %+v %v", again, err)
	}
	if _, ok := recovered.entity("build.item"); ok {
		t.Fatal("recovery published a package draft")
	}
	if _, problem := recovered.SimulateCandidate(memberOf(t, recovered, "builder"), CandidateSimulationRequest{Roots: plan.Roots, At: plan.At, Steps: []SimulationStep{{Type: "build.item", ID: "sample", Action: "build.item.create", Payload: json.RawMessage(`{"title":"Example"}`), Expect: "accepted"}}}); problem != nil {
		t.Fatalf("portable roots cannot use original simulation: %v", problem)
	}
}
