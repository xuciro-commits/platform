package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	resultformat "platformserver/internal/acceptance/result"
	"testing"
	"time"

	"platformserver/platform"
)

func migrationFixture(t *testing.T, name string, values []string, durable ...Journals) (*Tenant, platform.MigrationPlan, []Entry) {
	t.Helper()
	tn := builderTenant(t, name)
	entries := []Entry{}
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if len(durable) > 0 {
			raw, err := durable[0].AppendAccepted(context.Background(), name, e, key, hash)
			if err != nil {
				return nil, err
			}
			e.Body = raw
		}
		entries = append(entries, e)
		return e.Body, nil
	}
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
	for index, value := range values {
		id := string(rune('A' + index))
		decide(t, tn, "user", construction.ID, "build.asset.create", "build.asset", id, map[string]any{"name": value}, txnAt)
	}
	fields = append(fields, map[string]any{"name": "enabled", "title": "Enabled", "type": "boolean", "required": true})
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"fields": fields}, txnAt)
	to := save("target")
	literal := platform.ExprLiteralValue("boolean", true)
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "default", Types: []string{"build.asset"}, Batch: 2, Fields: []platform.MigrateField{{To: "enabled", Expr: &literal}}}}}
	return tn, plan, entries
}

func TestMigrationReopensItsDurableJournal(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			id := fmt.Sprintf("migration-durable-%d", time.Now().UnixNano())
			defer backend.cleanup(t, id)
			journal := backend.open(t, id)
			if _, err := journal.Entries(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
			live, plan, _ := migrationFixture(t, id, []string{"One", "Two", "Three"}, journal)
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				return journal.AppendAccepted(context.Background(), id, e, key, hash)
			}
			builder := memberOf(t, live, "builder")
			review, err := live.ReviewMigrationPlan(builder, plan, txnAt)
			if err != nil {
				t.Fatal(err)
			}
			decide(t, live, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
			request := submission(live, builder, construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{})
			first, refusal := live.Submit(builder, request, txnAt)
			if refusal != nil {
				t.Fatal(refusal)
			}
			position := journal.Position(id)
			journal.Close()
			journal = backend.reopen(t, id)
			defer journal.Close()
			entries, err := journal.Entries(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			recovered := builderTenant(t, id)
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				return journal.AppendAccepted(context.Background(), id, e, key, hash)
			}
			again, refusal := recovered.Submit(memberOf(t, recovered, "builder"), request, txnAt.Add(time.Hour))
			if refusal != nil || first.GetChangeId() != again.GetChangeId() || journal.Position(id) != position {
				t.Fatal("reopen repeated an accepted batch")
			}
			decide(t, recovered, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(time.Hour))
			if _, err := recovered.ActivateRelease(memberOf(t, recovered, "builder"), plan.To, "target", txnAt.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			entries, err = journal.Entries(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			final := builderTenant(t, id)
			if err := final.Replay(entries); err != nil {
				t.Fatal(err)
			}
			if final.releases.active != plan.To || migrationRun(t, final, "M").Migrated != 3 {
				t.Fatal("persistent recovery lost target activation or migration progress")
			}
			for _, rowID := range []string{"A", "B", "C"} {
				before, _ := recovered.records.types["build.asset"].rows.Get(rowID).Image()
				after, _ := final.records.types["build.asset"].rows.Get(rowID).Image()
				if !bytes.Equal(before, after) {
					t.Fatal("persistent recovery changed saved target facts")
				}
			}
		})
	}
}

func TestMigrationIncompleteScopeCannotActivateRequiredTarget(t *testing.T) {
	tn, plan, _ := migrationFixture(t, "migration-invalid-target", []string{"Old"})
	unselected := platform.ExprLiteralValue("boolean", false)
	plan.Scope.Where = &unselected
	builder := memberOf(t, tn, "builder")
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := tn.ActivateRelease(builder, plan.To, "incomplete-target", txnAt); err == nil || tn.releases.active != plan.From {
		t.Fatal("a completed cursor bypassed its required target values")
	}
	decide(t, tn, "builder", construction.ID, "build.migration.cancel", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	decide(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "Source remains usable"}, txnAt)
}

func TestMigrationRequiredFalseIsAnAcceptedTargetValue(t *testing.T) {
	tn, plan, _ := migrationFixture(t, "migration-required-false", []string{"Old"})
	literal := platform.ExprLiteralValue("boolean", false)
	plan.Steps[0].Fields[0].Expr = &literal
	builder := memberOf(t, tn, "builder")
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := tn.ActivateRelease(builder, plan.To, "false-target", txnAt); err != nil {
		t.Fatal("explicit false must satisfy the required target", err)
	}
	image, err := tn.records.types["build.asset"].rows.Get("A").Image()
	var values map[string]json.RawMessage
	if err != nil || json.Unmarshal(image, &values) != nil || string(values["enabled"]) != "false" {
		t.Fatalf("activation lost the accepted false value: %s %v", image, err)
	}
}

func migrationRun(t *testing.T, tn *Tenant, id string) platform.MigrationRun {
	t.Helper()
	c := tn.caller(memberOf(t, tn, "builder"), tn.app(construction.ID), false)
	run, ok := platform.Get[platform.MigrationRun](c, id)
	if !ok {
		t.Fatal("migration progress was not saved")
	}
	return run
}

func TestMigrationBatchesResumeSavedResults(t *testing.T) {
	tn, plan, entries := migrationFixture(t, "migration-batch", []string{"One", "Two", "Three"})
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	builder := memberOf(t, tn, "builder")
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, refusal := tn.Submit(builder, submission(tn, builder, construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}), txnAt); refusal != nil {
		t.Fatalf("start: %v fault: %+v", refusal, tn.fault.Load())
	}
	request := submission(tn, builder, construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{})
	first, refusal := tn.Submit(builder, request, txnAt)
	if refusal != nil {
		t.Fatal(refusal)
	}
	run := migrationRun(t, tn, "M")
	if run.Processed != 2 || run.Migrated != 2 || run.Cursor != "build.asset/B" || run.State != "running" {
		t.Fatalf("first batch progress: %+v", run)
	}
	batch, _, err := resultformat.DecodeBatch(entries[len(entries)-1].Body)
	if err != nil || len(batch.Rows) != 3 {
		t.Fatalf("batch must pair two data rows with its progress: %+v %v", batch, err)
	}
	before := bytes.Clone(entries[len(entries)-1].Body)
	count := len(entries)
	again, refusal := tn.Submit(builder, request, txnAt.Add(time.Hour))
	if refusal != nil || again.GetChangeId() != first.GetChangeId() || len(entries) != count || !bytes.Equal(before, entries[count-1].Body) {
		t.Fatal("same-key retry changed or repeated a migration batch")
	}
	for _, id := range []string{"A", "B"} {
		row := tn.records.types["build.asset"].rows.Get(id)
		image, err := row.Image()
		var values map[string]json.RawMessage
		if err != nil || json.Unmarshal(image, &values) != nil || string(values["enabled"]) != "true" || row.Projection == nil {
			t.Fatalf("target fact/source read image missing: %s %s %v", id, image, err)
		}
	}
	if got := refuse(t, tn, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "Lost future fact"}, txnAt); got == "ok" {
		t.Fatal("old-definition write was accepted during migration")
	}
	if got := refuse(t, tn, "builder", construction.ID, construction.ObjectType+".publish", construction.ObjectType, "asset", map[string]any{}, txnAt); got == "ok" {
		t.Fatal("direct publication bypassed the pending migration")
	}
	if _, err := tn.ActivateRelease(builder, plan.To, "too-early", txnAt); err == nil {
		t.Fatal("target activated before its data batches completed")
	}
	snapshot, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	fromSnapshot := builderTenant(t, tn.ID)
	if err := fromSnapshot.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if run := migrationRun(t, fromSnapshot, "M"); run.Cursor != "build.asset/B" || run.Migrated != 2 {
		t.Fatalf("snapshot lost pending progress: %+v", run)
	}
	for _, id := range []string{"A", "B"} {
		live, _ := tn.records.types["build.asset"].rows.Get(id).Image()
		held, _ := fromSnapshot.records.types["build.asset"].rows.Get(id).Image()
		if !bytes.Equal(live, held) {
			t.Fatal("snapshot changed accepted target facts")
		}
	}
	restored := builderTenant(t, tn.ID)
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if run := migrationRun(t, restored, "M"); run.Cursor != "build.asset/B" || run.Migrated != 2 {
		t.Fatalf("restored progress changed: %+v", run)
	}
	decide(t, restored, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(time.Hour))
	if run := migrationRun(t, restored, "M"); run.Migrated != 3 || run.State != "complete" {
		t.Fatalf("resume did not finish exactly the remaining row: %+v", run)
	}
	if _, err := restored.ActivateRelease(memberOf(t, restored, "builder"), plan.To, "target-activate", txnAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if restored.releases.active != plan.To {
		t.Fatal("migration did not use the original release activation")
	}
	decide(t, restored, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "After migration"}, txnAt.Add(2*time.Hour))
	image, _ := restored.records.types["build.asset"].rows.Get("A").Image()
	var values map[string]json.RawMessage
	if json.Unmarshal(image, &values) != nil || string(values["enabled"]) != "true" {
		t.Fatal("ordinary target-definition edit lost the migrated fact")
	}
}

func TestMigrationDecoderSelectsTheReleaseEvenWithUnchangedFields(t *testing.T) {
	tn, plan, _ := migrationFixture(t, "migration-same-shape", []string{"Source value"})
	fields := []map[string]any{{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "asset", map[string]any{"title": "Asset next", "fields": fields}, txnAt)
	builder := memberOf(t, tn, "builder")
	preview, err := tn.PreviewRelease(builder, platform.AssetObject, "asset")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	plan.To, err = tn.SaveReleaseCandidates(builder, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "asset"}}, preview.CandidateID, "same-shape-target", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	replacement := platform.ExprLiteralValue("text", "Migrated value")
	plan.Steps = []platform.MigrateStep{{Kind: "retype", Types: []string{"build.asset"}, Batch: 2, OnFailure: "reject", Fields: []platform.MigrateField{{From: "name", To: "name", Expr: &replacement}}}}
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil || review.Types[0].SourceShape != review.Types[0].TargetShape {
		t.Fatalf("same field shapes: %+v %v", review, err)
	}
	decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt.Add(time.Hour))
	decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(2*time.Hour))
	if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows.Get("A").Value, "name"); got != "Source value" {
		t.Fatal("source reader saw unactivated target facts")
	}
	if _, err := tn.ActivateRelease(builder, plan.To, "same-shape-activate", txnAt.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows.Get("A").Value, "name"); got != "Migrated value" {
		t.Fatal("target activation selected the source projection by shape alone")
	}
	snapshot, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	recovered := builderTenant(t, tn.ID)
	if err := recovered.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if got := businessField(recovered.records.types["build.asset"].info, recovered.records.types["build.asset"].rows.Get("A").Value, "name"); got != "Migrated value" {
		t.Fatal("snapshot restored the wrong release's facts")
	}
	decide(t, recovered, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "New write"}, txnAt.Add(4*time.Hour))
}

func TestMigrationFailedBatchDoesNotAcceptEarlierRows(t *testing.T) {
	for _, strategy := range []string{"reject", "quarantine"} {
		t.Run(strategy, func(t *testing.T) {
			tn, plan, entries := migrationFixture(t, "migration-failure-"+strategy, []string{"1", "invalid"})
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				entries = append(entries, e)
				return e.Body, nil
			}
			parse := platform.Expr{Kind: "number", Op: "parse-number", Args: []platform.Expr{platform.ExprReference("text", "name")}}
			zero := platform.ExprLiteralValue("number", 0)
			predicate := platform.Expr{Kind: "boolean", Op: ">", Args: []platform.Expr{parse, zero}}
			plan.Steps[0].Fields[0].Expr = &predicate
			plan.Steps[0].OnFailure = strategy
			review, err := tn.ReviewMigrationPlan(memberOf(t, tn, "builder"), plan, txnAt)
			if err != nil || len(review.Issues) != 1 {
				t.Fatalf("failure review: %+v %v", review, err)
			}
			before, _ := tn.records.types["build.asset"].rows.Get("A").Image()
			decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
			decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
			run := migrationRun(t, tn, "M")
			if run.State != "blocked" || len(run.Failures) != 1 || run.Failures[0].Record != "B" {
				t.Fatalf("failure/progress not recorded: %+v", run)
			}
			after, _ := tn.records.types["build.asset"].rows.Get("A").Image()
			if strategy == "reject" && (!bytes.Equal(before, after) || run.Migrated != 0 || run.Processed != 0) {
				t.Fatal("reject accepted a partial batch")
			}
			if strategy == "quarantine" && (bytes.Equal(before, after) || run.Migrated != 1 || run.Processed != 2) {
				t.Fatal("quarantine did not distinguish the accepted and failed rows")
			}
			recovered := builderTenant(t, tn.ID)
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			if restored := migrationRun(t, recovered, "M"); restored.State != "blocked" || len(restored.Failures) != 1 || restored.Migrated != run.Migrated {
				t.Fatal("recovery lost the saved failure boundary")
			}
			if _, err := recovered.ActivateRelease(memberOf(t, recovered, "builder"), plan.To, "blocked-activation", txnAt); err == nil {
				t.Fatal("unresolved conversion failure activated its target")
			}
			decide(t, recovered, "builder", construction.ID, "build.migration.cancel", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(time.Hour))
			if run := migrationRun(t, recovered, "M"); run.State != "cancelled" {
				t.Fatalf("failed migration could not release its source: %+v", run)
			}
			row := recovered.records.types["build.asset"].rows.Get("A")
			image, _ := row.Image()
			var fields map[string]json.RawMessage
			if json.Unmarshal(image, &fields) != nil || fields["enabled"] != nil || string(fields["name"]) != `"1"` || row.Projection != nil {
				t.Fatal("cancellation lost source facts or retained a future field")
			}
			if strategy == "quarantine" && (row.HistoryLength() != 3 || row.RecordHistory()[2].Schema != "build.migration.cancel") {
				t.Fatal("cancellation erased history instead of recording compensation")
			}
			decide(t, recovered, "user", construction.ID, "build.asset.edit", "build.asset", "A", map[string]any{"name": "Writable again"}, txnAt.Add(2*time.Hour))
		})
	}
}
