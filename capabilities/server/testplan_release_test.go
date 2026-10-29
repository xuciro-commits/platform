package platformserver

import "platformserver/studio/build"

import (
	"bytes"

	"testing"
	"time"

	"platformserver/platform"
)

func TestBusinessTestPlanRetainsVersionsAndInstalls(t *testing.T) {
	at := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	makeTarget := func(id string) *Tenant {
		tn := builderTenant(t, id)
		publishObject(t, tn, "builder", "O", map[string]any{"name": "item", "title": "Item", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}}, at)
		decide(t, tn, "builder", build.ID, build.TestPlanType+".create", build.TestPlanType, "T", map[string]any{"name": "createitem", "title": "Original scenario", "roots": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.item"}}, "at": at, "steps": []platform.TestStep{{Type: "build.item", ID: "test", Action: "build.item.create", Payload: `{"title":"Example"}`, Expect: "accepted"}}}, at)
		return tn
	}
	source := makeTarget("testplan-source")
	decide(t, source, "builder", build.ID, build.TestPlanType+".publish", build.TestPlanType, "T", map[string]any{}, at)
	owner := source.app(build.ID).(platform.AssetLifecycleOwner)
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetTestPlan, Name: "createitem"}
	first, err := owner.ResolvePinned(ref, "1.tests-1")
	if err != nil {
		t.Fatal(err)
	}
	decide(t, source, "builder", build.ID, build.TestPlanType+".edit", build.TestPlanType, "T", map[string]any{"title": "Later scenario"}, at)
	decide(t, source, "builder", build.ID, build.TestPlanType+".publish", build.TestPlanType, "T", map[string]any{}, at)
	retained, err := owner.ResolvePinned(ref, "1.tests-1")
	if err != nil || !bytes.Equal(retained.Body, first.Body) {
		t.Fatal("old scenario changed", err)
	}
	second, err := owner.ResolvePinned(ref, "1.tests-2")
	if err != nil || bytes.Equal(second.Body, first.Body) {
		t.Fatal("new scenario version is missing", err)
	}
	target := makeTarget("testplan-target")
	registry, err := target.assetRegistryLocked()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := registry.PrepareReleaseAssets([]platform.ReleaseAsset{first}, platform.AssetReleaseContext{Member: memberOf(t, target, "builder"), At: at})
	if err != nil || len(plans) != 1 {
		t.Fatalf("prepare scenario installation: %+v %v", plans, err)
	}
	before, err := target.installationPredecessor(plans[0].Record)
	if err != nil {
		t.Fatal(err)
	}
	trial, err := target.stageReleaseInstallationLocked([]platform.ReleaseInstallation{{ReleasePublication: platform.ReleasePublication{Schema: plans[0].Schema, Image: plans[0].Image}, Before: before, Ref: &plans[0].Ref, Record: plans[0].Record}}, true)
	if err != nil {
		t.Fatal(err)
	}
	installed, ok := platform.Get[build.TestPlan](trial.automation(build.ID, false), "T")
	if !ok || installed.Title != "Original scenario" || installed.DefinitionVersion != 1 || len(installed.Roots) != 1 || installed.Application != "" || installed.Object != "" {
		t.Fatalf("portable scenario installation: %+v", installed)
	}
}
