package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"platformserver/platform"
)

func TestMigrationRenameAliasFollowsNewWritesAndPreservesHistory(t *testing.T) {
	tn, plan, entries := migrationFixture(t, "migration-alias", []string{"Original", "Second"})
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	fields := []map[string]any{
		{"name": "name", "title": "Old name", "type": "text", "readOnly": true, "aliasOf": "title", "sourceType": "user-input"},
		{"name": "title", "title": "New name", "type": "text", "sourceType": "user-input"},
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"fields": fields}, txnAt)
	builder := memberOf(t, tn, "builder")
	preview, err := tn.PreviewRelease(builder, platform.AssetObject, "asset")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	plan.To, err = tn.SaveReleaseCandidates(builder, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "asset"}}, preview.CandidateID, "rename-target", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = []platform.MigrateStep{{Kind: "rename", Types: []string{"build.asset"}, Batch: 1, Fields: []platform.MigrateField{{From: "name", To: "title"}}}}
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	originalRelease := bytes.Clone(tn.releases.raw(plan.From))
	originalHistory := copyHistory(tn.records.types["build.asset"].rows.get("A").history)
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	for i := 0; i < 2; i++ {
		decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(time.Hour))
	}
	if _, err := tn.ActivateRelease(builder, plan.To, "rename-activate", txnAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := refuse(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "Forbidden"}, txnAt.Add(2*time.Hour)); got == "ok" {
		t.Fatal("the old read alias remained writable")
	}
	decide(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"title": "Changed"}, txnAt.Add(2*time.Hour))
	for _, name := range []string{"name", "title"} {
		if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows.get("A").value, name); got != "Changed" {
			t.Fatalf("alias %s lost the current value: %s", name, got)
		}
	}
	if !bytes.Equal(originalRelease, tn.releases.raw(plan.From)) || string(platform.Raw(originalHistory)) != string(platform.Raw(tn.records.types["build.asset"].rows.get("A").history[:len(originalHistory)])) {
		t.Fatal("rename rewrote its frozen source or original history")
	}
	recovered := builderTenant(t, tn.ID)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	before, _ := tn.records.types["build.asset"].rows.get("A").image()
	after, _ := recovered.records.types["build.asset"].rows.get("A").image()
	if !bytes.Equal(before, after) {
		t.Fatal("recovery recalculated or changed the accepted alias facts")
	}

	// A retained public name cannot expose its now restricted target field.
	fields[1]["read"] = []string{construction.Builder}
	decide(t, recovered, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"fields": fields}, txnAt.Add(3*time.Hour))
	decide(t, recovered, "builder", construction.ID, construction.ObjectType+".publish", construction.ObjectType, "asset", map[string]any{}, txnAt.Add(3*time.Hour))
	view, refusal := recovered.RecordOf(memberOf(t, recovered, "user"), "build.asset", "A", txnAt.Add(3*time.Hour))
	var exposed map[string]json.RawMessage
	if refusal != nil || json.Unmarshal(platform.Raw(view.Record), &exposed) != nil || len(exposed["name"]) != 0 || len(exposed["title"]) != 0 {
		t.Fatalf("alias escaped its target field permissions: %s %v", platform.Raw(view.Record), refusal)
	}
	if _, refusal := recovered.Records(memberOf(t, recovered, "user"), "build.asset", platform.Query{Domain: json.RawMessage(`[["name","=","Changed"]]`)}, txnAt.Add(3*time.Hour)); refusal == nil {
		t.Fatal("filtering by an alias exposed its hidden target")
	}
}

func TestMigrationRetiredFieldStaysReadableWithoutNewWrites(t *testing.T) {
	tn, plan, entries := migrationFixture(t, "migration-retired-field", []string{"Historical value"})
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	fields := []map[string]any{{"name": "name", "title": "Retired name", "type": "text", "readOnly": true, "sourceType": "user-input"}}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"fields": fields}, txnAt)
	builder := memberOf(t, tn, "builder")
	preview, err := tn.PreviewRelease(builder, platform.AssetObject, "asset")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	plan.To, err = tn.SaveReleaseCandidates(builder, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "asset"}}, preview.CandidateID, "retire-target", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = []platform.MigrateStep{{Kind: "retire", Types: []string{"build.asset"}, Batch: 1, Fields: []platform.MigrateField{{From: "name", To: "name"}}}}
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := tn.records.types["build.asset"].rows.get("A").image()
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := tn.ActivateRelease(builder, plan.To, "retire-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	after, _ := tn.records.types["build.asset"].rows.get("A").image()
	if !bytes.Equal(before, after) {
		t.Fatal("retirement rewrote a retained record")
	}
	if got := refuse(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "Replace history"}, txnAt); got == "ok" {
		t.Fatal("retired field was writable")
	}
	decide(t, tn, "user", construction.ID, "build.asset.create", "build.asset", "B", map[string]any{}, txnAt)
	recovered := builderTenant(t, tn.ID)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if got := businessField(recovered.records.types["build.asset"].info, recovered.records.types["build.asset"].rows.get("A").value, "name"); got != "Historical value" {
		t.Fatal("retirement lost the old field's readable value")
	}
}
