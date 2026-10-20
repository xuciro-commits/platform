package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"platformserver/internal/records"
	"testing"

	"platformserver/platform"
)

func TestMigrationRetainsEarlierTypeImagesThroughWritesAndCancellation(t *testing.T) {
	tn, initial, entries := migrationFixture(t, "migration-prior-images", []string{"12"})
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	builder := memberOf(t, tn, "builder")
	fields := []map[string]any{{"name": "name", "title": "Amount", "type": "decimal", "sourceType": "user-input"}}
	save := func(title, key string) string {
		t.Helper()
		decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "asset", map[string]any{"title": title, "fields": fields}, txnAt)
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
	start := func(plan platform.MigrationPlan, id string) {
		t.Helper()
		review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
		if err != nil {
			t.Fatal(err)
		}
		decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, id, map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
		decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, id, map[string]any{}, txnAt)
	}
	second := save("Amount version", "second")
	parse := platform.Expr{Kind: "decimal", Op: "parse-decimal", Args: []platform.Expr{platform.ExprReference("text", "name")}}
	plan := platform.MigrationPlan{From: initial.From, To: second, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "retype", Types: []string{"build.asset"}, Batch: 1, OnFailure: "reject", Fields: []platform.MigrateField{{From: "name", To: "name", Expr: &parse}}}}}
	start(plan, "M1")
	if _, err := tn.ActivateRelease(builder, second, "second-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": 21}, txnAt)
	check := func(release string, want string) {
		t.Helper()
		row := tn.records.types["build.asset"].rows.Get("A")
		source := records.ProjectionFor(row.Projection, release)
		if source == nil {
			t.Fatalf("lost source release %s", release)
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(source.Value, &values) != nil || string(values["name"]) != want {
			t.Fatalf("source value changed: %s", source.Value)
		}
		actual, _ := row.Image()
		if _, err := records.DecodeImage(tn.records.types["build.asset"].info, actual, row.Projection, tn.releases.active); err != nil {
			t.Fatal(err)
		}
	}
	check(initial.From, `"12"`)
	fields = append(fields, map[string]any{"name": "enabled", "title": "Enabled", "type": "boolean", "required": true, "sourceType": "user-input"})
	third := save("Enabled amount", "third")
	enabled := platform.ExprLiteralValue("boolean", true)
	plan = platform.MigrationPlan{From: second, To: third, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "default", Types: []string{"build.asset"}, Batch: 1, Fields: []platform.MigrateField{{To: "enabled", Expr: &enabled}}}}}
	start(plan, "M2")
	check(initial.From, `"12"`)
	check(second, `21`)
	decide(t, tn, "builder", construction.ID, "build.migration.cancel", platform.MigrationRunType, "M2", map[string]any{}, txnAt)
	check(initial.From, `"12"`)
	if records.ProjectionFor(tn.records.types["build.asset"].rows.Get("A").Projection, second) != nil {
		t.Fatal("cancel kept the cancelled migration's source layer")
	}
	start(plan, "M3")
	if _, err := tn.ActivateRelease(builder, third, "third-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": 34}, txnAt)
	check(initial.From, `"12"`)
	check(second, `21`)
	before, _ := records.FullImage("build.asset", "A", tn.records.types["build.asset"].rows.Get("A"))
	recovered := builderTenant(t, tn.ID)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	after, _ := records.FullImage("build.asset", "A", recovered.records.types["build.asset"].rows.Get("A"))
	if !bytes.Equal(jsonOf(before), jsonOf(after)) {
		t.Fatal("recovery changed the versioned row's saved images or history")
	}
	corrupt := records.CloneProjection(tn.records.types["build.asset"].rows.Get("A").Projection)
	corrupt.Prior = append(corrupt.Prior, corrupt.Prior[0])
	actual, _ := tn.records.types["build.asset"].rows.Get("A").Image()
	if _, err := records.DecodeImage(tn.records.types["build.asset"].info, actual, corrupt, third); err == nil {
		t.Fatal("recovery accepted duplicate source projections")
	}
}
