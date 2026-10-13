package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestMigrationRetiresActionAndRejectsItsPendingApproval(t *testing.T) {
	compose := func() *Tenant {
		return builderTenant(t, "migration-retire-action", work.New("migration-retire-action"))
	}
	tn := compose()
	entries := []Entry{}
	failAppend := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if failAppend {
			return nil, errors.New("injected migration append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	fields := []map[string]any{{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}}
	states := []map[string]any{{"name": "open", "title": "Open"}, {"name": "pending", "title": "Pending"}, {"name": "done", "title": "Done"}, {"name": "rejected", "title": "Rejected"}}
	action := map[string]any{"name": "finish", "title": "Finish", "from": []string{"open"}, "to": "done", "roles": []string{build.User}, "approval": map[string]any{"pending": "pending", "rejected": "rejected", "levels": []map[string]any{{"title": "Builder", "role": build.Builder}}}}
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "asset", map[string]any{"name": "asset", "title": "Asset", "fields": fields, "states": states, "actions": []any{action}}, txnAt)
	builder := memberOf(t, tn, "builder")
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
	decide(t, tn, "user", build.ID, "build.asset.create", "build.asset", "A", map[string]any{"name": "Waiting"}, txnAt)
	decide(t, tn, "user", build.ID, "build.asset.finish", "build.asset", "A", map[string]any{}, txnAt)
	decide(t, tn, "user", build.ID, "build.asset.create", "build.asset", "B", map[string]any{"name": "Also waiting"}, txnAt)
	decide(t, tn, "user", build.ID, "build.asset.finish", "build.asset", "B", map[string]any{}, txnAt)
	requests, _, refusal := platform.Find[platform.ApprovalRequest](tn.automation(work.ID, false), platform.Query{})
	if refusal != nil || len(requests) != 2 || requests[0].State != "pending" {
		t.Fatalf("pending approval: %+v %v", requests, refusal)
	}
	original := bytes.Clone([]byte(requests[0].Submission))
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "asset", map[string]any{"actions": []any{}}, txnAt)
	to := save("target")
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "retire-action", Types: []string{"build.asset"}, Action: "build.asset.finish", Batch: 1}}}
	review, err := tn.ReviewMigrationPlan(builder, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, txnAt)
	advance := submission(tn, builder, build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{})
	before, count := snapshot(tn), len(entries)
	failAppend = true
	if _, err := tn.Submit(builder, advance, txnAt.Add(time.Hour)); err == nil || snapshot(tn) != before || len(entries) != count {
		t.Fatal("failed append exposed retirement, tasks or migration progress")
	}
	failAppend = false
	first, refusal := tn.Submit(builder, advance, txnAt.Add(time.Hour))
	if refusal != nil {
		t.Fatal(refusal)
	}
	if run := migrationRun(t, tn, "M"); run.State != "running" || run.Processed != 1 {
		t.Fatalf("retirement ignored its batch boundary: %+v", run)
	}
	before, count = snapshot(tn), len(entries)
	again, retryRefusal := tn.Submit(builder, advance, txnAt.Add(2*time.Hour))
	if retryRefusal != nil || first.GetChangeId() != again.GetChangeId() || snapshot(tn) != before || len(entries) != count {
		t.Fatal("retry repeated an accepted retirement batch")
	}
	if _, err := tn.ActivateRelease(builder, to, "early-retire-activation", txnAt); err == nil {
		t.Fatal("retirement activated with unresolved pending requests")
	}
	decide(t, tn, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt.Add(2*time.Hour))
	a, found := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), requests[0].ID)
	if !found || a.State != "rejected" || a.RejectedBy != "builder" || !bytes.Equal(original, []byte(a.Submission)) {
		t.Fatalf("retirement did not reject the original held request: %+v", a)
	}
	if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows["A"].value, "state"); got != "rejected" {
		t.Fatalf("held business state was not released: %s", got)
	}
	for _, request := range requests {
		task, exists := platform.Get[platform.WorkTask](tn.automation(work.ID, false), request.ID+"#1")
		if !exists || task.State != "canceled" {
			t.Fatal("retirement left an actionable approval task")
		}
	}
	if got := refuse(t, tn, "builder", work.ID, "work.approval.retire", work.ApprovalType, requests[0].ID, map[string]any{"migration": "M"}, txnAt); got == "ok" {
		t.Fatal("a public submission forged the private retirement directive")
	}
	if _, err := tn.ActivateRelease(builder, to, "target-activate", txnAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, declared := range tn.Catalog(memberOf(t, tn, "user")) {
		if declared.Schema == "build.asset.finish" {
			t.Fatal("retired action remained in the live catalog")
		}
	}
	if got := refuse(t, tn, "user", build.ID, "build.asset.finish", "build.asset", "A", map[string]any{}, txnAt); got == "ok" {
		t.Fatal("retired action accepted a new submission")
	}
	restored := compose()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	after, ok := platform.Get[platform.ApprovalRequest](restored.automation(work.ID, false), requests[0].ID)
	if !ok || after.State != "rejected" || !bytes.Equal(original, []byte(after.Submission)) {
		t.Fatal("recovery lost the retired request's saved outcome or original bytes")
	}
	live, _ := json.Marshal(a)
	recovered, _ := json.Marshal(after)
	if !bytes.Equal(live, recovered) {
		t.Fatal("recovery ran retirement again instead of applying its result")
	}
	for _, definition := range restored.definitions {
		if definition.Ref.Kind == platform.AssetAction && definition.Ref.Name == "build.asset.finish" {
			t.Fatal("recovery restored a retired live definition")
		}
	}
}
