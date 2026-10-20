package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestMigrationReviewUsesSealedShapesWithoutChangingLiveRows(t *testing.T) {
	tn := builderTenant(t, "migration-review")
	builder := memberOf(t, tn, "builder")
	fields := []map[string]any{{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "asset", map[string]any{"name": "asset", "title": "Asset", "fields": fields}, txnAt)
	save := func(key string) string {
		t.Helper()
		preview, err := tn.PreviewRelease(builder, platform.AssetObject, "asset")
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		id, err := tn.SaveReleaseCandidates(builder, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "asset"}}, preview.CandidateID, key, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	from := save("source")
	if _, err := tn.ActivateRelease(builder, from, "source-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", construction.ID, "build.asset.create", "build.asset", "A", map[string]any{"name": "Old value"}, txnAt)
	fields = append(fields, map[string]any{"name": "enabled", "title": "Enabled", "type": "boolean", "required": true})
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"fields": fields}, txnAt)
	to := save("target")
	literal := platform.ExprLiteralValue("boolean", true)
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "default", Types: []string{"build.asset"}, Batch: 2, Fields: []platform.MigrateField{{To: "enabled", Expr: &literal}}}}}
	before, err := tn.records.types["build.asset"].rows.get("A").image()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil || answer.ID == "" || len(answer.Types) != 1 || answer.Types[0].Records != 1 || answer.Types[0].SourceShape == answer.Types[0].TargetShape {
		t.Fatalf("migration review: %+v %v", answer, err)
	}
	after, _ := tn.records.types["build.asset"].rows.get("A").image()
	if !bytes.Equal(before, after) || tn.releases.active != from {
		t.Fatal("read-only review changed a row or activated its target")
	}
	if _, fieldInstalled := tn.records.types["build.asset"].info.Field("enabled"); fieldInstalled {
		t.Fatal("review installed its target shape")
	}
	if _, err := tn.ReviewMigrationPlan(memberOf(t, tn, "user"), plan, txnAt); err == nil {
		t.Fatal("ordinary member inspected unfiltered migration scope")
	}
	copy := plan
	copy.To = from
	if _, err := tn.ReviewMigrationPlan(builder, copy, txnAt); err == nil {
		t.Fatal("identical releases were accepted")
	}
	var row map[string]json.RawMessage
	if json.Unmarshal(after, &row) != nil || string(row["name"]) != `"Old value"` {
		t.Fatal("original value was lost")
	}
}

func TestMigrationRetypeRequiresQuarantineForReferencesOutsideItsRelease(t *testing.T) {
	tenant := builderTenant(t, "migration-referrer")
	builder := memberOf(t, tenant, "builder")
	fields := []construction.Field{{Name: "value", Title: "Value", Type: "text"}}
	publishObject(t, tenant, "builder", "source", map[string]any{"name": "refasset", "title": "Source", "fields": fields}, txnAt)
	save := func(key string) string {
		t.Helper()
		preview, err := tenant.PreviewRelease(builder, platform.AssetObject, "source")
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("candidate: %+v %v", preview, err)
		}
		id, err := tenant.SaveReleaseCandidates(builder, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "source"}}, preview.CandidateID, key, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	from := save("source")
	if _, err := tenant.ActivateRelease(builder, from, "activate-source", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tenant, "user", construction.ID, "build.refasset.create", "build.refasset", "A", map[string]any{"value": "12"}, txnAt)
	publishObject(t, tenant, "builder", "consumer", map[string]any{"name": "consumer", "title": "Consumer", "fields": []construction.Field{{Name: "source", Title: "Source", Type: "reference", Ref: "build.refasset"}}}, txnAt)
	decide(t, tenant, "user", construction.ID, "build.consumer.create", "build.consumer", "B", map[string]any{"source": "A"}, txnAt)
	fields[0].Type = "decimal"
	decide(t, tenant, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "source", map[string]any{"fields": fields}, txnAt)
	to := save("target")
	parse := platform.Expr{Kind: "decimal", Op: "parse-decimal", Args: []platform.Expr{platform.ExprReference("text", "value")}}
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.refasset"}, Steps: []platform.MigrateStep{{Kind: "retype", Types: []string{"build.refasset"}, Batch: 1, OnFailure: "reject", Fields: []platform.MigrateField{{From: "value", To: "value", Expr: &parse}}}}}
	before, _, err := tenant.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.ReviewMigrationPlan(builder, plan, txnAt); err == nil || !strings.Contains(err.Error(), "quarantine") {
		t.Fatalf("outside reference passed conversion admission: %v", err)
	}
	plan.Steps[0].OnFailure = "quarantine"
	if review, err := tenant.ReviewMigrationPlan(builder, plan, txnAt); err != nil || len(review.Types) != 1 || review.Types[0].Type != "build.refasset" {
		t.Fatalf("reference check expanded the migration or refused its explicit strategy: %+v %v", review, err)
	}
	after, _, err := tenant.Snapshot(func() int64 { return 0 })
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("conversion review changed live data", err)
	}
}
