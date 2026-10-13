package platformserver

import (
	"bytes"
	"encoding/json"
	"testing"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestMigrationReviewUsesSealedShapesWithoutChangingLiveRows(t *testing.T) {
	tn := builderTenant(t, "migration-review")
	builder := memberOf(t, tn, "builder")
	fields := []map[string]any{{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}}
	decide(t, tn, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "asset", map[string]any{"name": "asset", "title": "Asset", "fields": fields}, txnAt)
	save := func(key string) string {
		t.Helper()
		preview, err := tn.PreviewRelease(builder, platform.AssetObject, "asset")
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		id, err := tn.SaveReleaseCandidates(builder, []build.JointDraftRef{{Kind: platform.AssetObject, ID: "asset"}}, preview.CandidateID, key, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	from := save("source")
	if _, err := tn.ActivateRelease(builder, from, "source-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", build.ID, "build.asset.create", "build.asset", "A", map[string]any{"name": "Old value"}, txnAt)
	fields = append(fields, map[string]any{"name": "enabled", "title": "Enabled", "type": "boolean", "required": true})
	decide(t, tn, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "asset", map[string]any{"fields": fields}, txnAt)
	to := save("target")
	literal := platform.ExprLiteralValue("boolean", true)
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "default", Types: []string{"build.asset"}, Batch: 2, Fields: []platform.MigrateField{{To: "enabled", Expr: &literal}}}}}
	before, err := tn.records.types["build.asset"].rows["A"].image()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil || answer.ID == "" || len(answer.Types) != 1 || answer.Types[0].Records != 1 || answer.Types[0].SourceShape == answer.Types[0].TargetShape {
		t.Fatalf("migration review: %+v %v", answer, err)
	}
	after, _ := tn.records.types["build.asset"].rows["A"].image()
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
