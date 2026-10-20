package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"testing"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestMigrationPlanAssetsRetainVersionsAndUseTheOriginalExecutor(t *testing.T) {
	tn, plan, entries := migrationFixture(t, "migration-plan-asset", []string{"One", "Two", "Three"})
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member := memberOf(t, tn, "builder")
	decide(t, tn, "builder", construction.ID, build.MigrationPlanType+".create", build.MigrationPlanType, "P", map[string]any{"name": "evolution", "title": "Evolution", "plan": plan}, txnAt)
	if got := refuse(t, tn, "user", construction.ID, build.SchemaMigrationPlan, build.MigrationPlanType, "P", map[string]any{}, txnAt); got == "ok" {
		t.Fatal("an operator published a migration asset")
	}
	decide(t, tn, "builder", construction.ID, build.SchemaMigrationPlan, build.MigrationPlanType, "P", map[string]any{}, txnAt)
	registry, err := tn.assetRegistryLocked()
	if err != nil {
		t.Fatal(err)
	}
	ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetMigration, Name: "evolution"}
	first, err := registry.ResolvePinned(ref, "1.migration-1")
	if err != nil {
		t.Fatal(err)
	}
	if tn.records.types[platform.MigrationRunType].rows.len() != 0 {
		t.Fatal("publishing executed the migration")
	}
	versions := tn.records.types[build.MigrationPlanType].rows.get("P").value.Interface().(build.MigrationPlanRecord)
	if versions.DefinitionVersion != 1 || len(versions.Versions) != 1 {
		t.Fatal("publication did not retain its version")
	}
	second := plan
	second.Steps = append([]platform.MigrateStep{}, plan.Steps...)
	second.Steps[0].Batch = 1
	decide(t, tn, "builder", construction.ID, build.MigrationPlanType+".edit", build.MigrationPlanType, "P", map[string]any{"plan": second}, txnAt)
	preview, err := tn.PreviewRelease(member, platform.AssetMigration, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("migration candidate: %+v %v", preview, err)
	}
	candidate, err := tn.SaveReleaseCandidates(member, []construction.JointDraftRef{{Kind: platform.AssetMigration, ID: "P"}}, preview.CandidateID, "save-plan-candidate", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := tn.releases.candidate(candidate)
	if err != nil {
		t.Fatal(err)
	}
	installations, err := registry.PrepareReleaseAssets(sealed.Assets, platform.AssetReleaseContext{Member: member, At: txnAt})
	if err != nil || len(installations) != 1 {
		t.Fatalf("plan lifecycle installation: %+v %v", installations, err)
	}
	if installations[0].Ref != ref || installations[0].Record != build.MigrationPlanType+"/P" || registry.ApplyInstallation(installations[0], platform.AssetApplyContext{Private: true}) != nil {
		t.Fatal("migration plan bypassed its lifecycle owner")
	}
	decide(t, tn, "builder", construction.ID, build.SchemaMigrationPlan, build.MigrationPlanType, "P", map[string]any{}, txnAt)
	old, err := registry.ResolvePinned(ref, "1.migration-1")
	if err != nil || !bytes.Equal(old.Body, first.Body) {
		t.Fatal("editing changed retained migration bytes")
	}
	current, err := registry.ResolvePinned(ref, "1.migration-2")
	if err != nil {
		t.Fatal(err)
	}
	var declaration platform.MigrationDescriptor
	if json.Unmarshal(current.Body, &declaration) != nil || declaration.Plan.Steps[0].Batch != 1 {
		t.Fatal("new plan version was not frozen")
	}
	review, err := tn.ReviewMigrationPlan(member, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	binding := platform.AssetBinding{Ref: ref, SourceVersion: first.SourceVersion}
	if got := refuse(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "bad", map[string]any{"plan": second, "asset": binding, "review": review.ID, "reviewedAt": review.At}, txnAt); got == "ok" {
		t.Fatal("a pinned plan was silently replaced with the latest draft")
	}
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"asset": binding, "review": review.ID, "reviewedAt": review.At}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	run := migrationRun(t, tn, "M")
	if run.Asset == nil || *run.Asset != binding || run.Processed != 2 {
		t.Fatalf("start ignored the exact retained plan: %+v", run)
	}
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := tn.ActivateRelease(member, plan.To, "target-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	recovered := builderTenant(t, "migration-plan-asset")
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	registry, err = recovered.assetRegistryLocked()
	if err != nil {
		t.Fatal(err)
	}
	old, err = registry.ResolvePinned(ref, first.SourceVersion)
	if err != nil || !bytes.Equal(old.Body, first.Body) {
		t.Fatal("recovery lost an earlier plan version")
	}
	if got := migrationRun(t, recovered, "M"); got.Asset == nil || *got.Asset != binding || got.Migrated != 3 {
		t.Fatal("recovery reran a plan or lost its exact binding")
	}
}
