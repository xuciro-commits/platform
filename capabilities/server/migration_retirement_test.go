package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
	"platformserver/platform/authz"
)

func TestMigrationRetiresActionAndRejectsItsPendingApproval(t *testing.T) {
	testActionRetirement(t, actionRetirementCase{})
}
func TestMigrationRedirectsApprovalAfterTargetActivation(t *testing.T) {
	testActionRetirement(t, actionRetirementCase{redirect: true})
}

func TestMigrationRedirectReauthorizesRequester(t *testing.T) {
	testActionRetirement(t, actionRetirementCase{redirect: true, deny: true})
}

func TestMigrationRedirectsMultilevelDelegatedApproval(t *testing.T) {
	testActionRetirement(t, actionRetirementCase{redirect: true, multilevel: true})
}

type actionRetirementCase struct{ redirect, deny, multilevel bool }

func testActionRetirement(t *testing.T, scenario actionRetirementCase) {
	redirect, deny := scenario.redirect, scenario.deny
	compose := func() *Tenant {
		if scenario.multilevel {
			return composeTenant(t, "migration-retire-action", []Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user"), seatOf("deputy", "build:user")}, build.New("migration-retire-action"), work.New("migration-retire-action"))
		}
		return builderTenant(t, "migration-retire-action", work.New("migration-retire-action"))
	}
	tn := compose()
	entries := []Entry{}
	failAppend := false
	attach := func() {
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			if failAppend {
				return nil, errors.New("injected migration append failure")
			}
			entries = append(entries, e)
			return e.Body, nil
		}
	}
	attach()
	fields := []map[string]any{{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}}
	states := []map[string]any{{"name": "open", "title": "Open"}, {"name": "pending", "title": "Pending"}, {"name": "done", "title": "Done"}, {"name": "rejected", "title": "Rejected"}}
	levels := []map[string]any{{"title": "Builder", "role": build.Builder}}
	if scenario.multilevel {
		levels = append(levels, map[string]any{"title": "Final review", "role": build.Builder})
	}
	action := map[string]any{"name": "finish", "title": "Finish", "from": []string{"open"}, "to": "done", "roles": []string{build.User}, "approval": map[string]any{"pending": "pending", "rejected": "rejected", "levels": levels}}
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
	if scenario.multilevel {
		for i, request := range requests {
			decide(t, tn, "builder", work.ID, "work.approval.approve", work.ApprovalType, request.ID, map[string]any{}, txnAt)
			requests[i], _ = platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), request.ID)
			if requests[i].Level != 1 || requests[i].State != "pending" {
				t.Fatal("the source request did not wait at its second level")
			}
		}
		decide(t, tn, "builder", work.ID, work.SchemaDelegate, work.DelegationType, "D", map[string]any{"to": "deputy", "start": txnAt.UTC().Format(time.DateOnly), "end": txnAt.AddDate(1, 0, 0).UTC().Format(time.DateOnly)}, txnAt)
	}
	original := bytes.Clone([]byte(requests[0].Submission))
	targetActions := []any{}
	if redirect {
		newAction := map[string]any{"name": "finishnew", "title": "New finish", "from": []string{"open"}, "to": "done", "roles": []string{build.User}, "approval": map[string]any{"pending": "pending", "rejected": "rejected", "levels": levels}}
		targetActions = append(targetActions, newAction)
	}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "asset", map[string]any{"actions": targetActions}, txnAt)
	to := save("target")
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.asset"}, Steps: []platform.MigrateStep{{Kind: "retire-action", Types: []string{"build.asset"}, Action: "build.asset.finish", Batch: 1}}}
	if redirect {
		plan.Steps[0].Redirect = "build.asset.finishnew"
	}
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
	wantState := "rejected"
	if redirect {
		wantState = "redirected"
	}
	actor := a.RejectedBy
	if redirect {
		actor = a.RedirectedBy
	}
	if !found || a.State != wantState || actor != "builder" || !bytes.Equal(original, []byte(a.Submission)) {
		t.Fatalf("retirement did not reject the original held request: %+v", a)
	}
	wantBusinessState := "rejected"
	if redirect {
		wantBusinessState = "open"
	}
	if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows.get("A").value, "state"); got != wantBusinessState {
		t.Fatalf("held business state was not released: %s", got)
	}
	for _, request := range requests {
		task, exists := platform.Get[platform.WorkTask](tn.automation(work.ID, false), fmt.Sprintf("%s#%d", request.ID, request.Level+1))
		if !exists || task.State != "canceled" {
			t.Fatal("retirement left an actionable approval task")
		}
	}
	if got := refuse(t, tn, "builder", work.ID, "work.approval.retire", work.ApprovalType, requests[0].ID, map[string]any{"migration": "M"}, txnAt); got == "ok" {
		t.Fatal("a public submission forged the private retirement directive")
	}
	if redirect {
		tn.Work(txnAt.Add(3 * time.Hour))
		for _, old := range requests {
			queued, _ := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), old.ID)
			if queued.RedirectTo != "" {
				t.Fatal("redirect opened before its target was active")
			}
		}
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
	if redirect && deny {
		tn.app(build.ID).Manifest().Actions.UsePolicies(func() []authz.Policy {
			return []authz.Policy{{ID: "redirect-denied", Effect: "deny", Permission: "build.asset.finishnew"}}
		})
		tn.Work(txnAt.Add(4 * time.Hour))
		for _, old := range requests {
			failed, _ := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), old.ID)
			if failed.State != "refused" || failed.RedirectTo != "" || failed.Outcome == "" || failed.Submission != old.Submission {
				t.Fatalf("redirect ignored the current policy: %+v", failed)
			}
		}
		all, _, _ := platform.Find[platform.ApprovalRequest](tn.automation(work.ID, false), platform.Query{})
		if len(all) != 2 {
			t.Fatal("denied redirect created a target request")
		}
		wantState = "refused"
		a, _ = platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), requests[0].ID)
	}
	if redirect && !deny {
		tn.Work(txnAt.Add(4 * time.Hour))
		for _, old := range requests {
			queued, _ := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), old.ID)
			if queued.RedirectTo == "" {
				t.Fatalf("target activation lost the queued redirect: %+v", queued)
			}
			newRequest, exists := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), queued.RedirectTo)
			if !exists || newRequest.State != "pending" || newRequest.Action != "build.asset.finishnew" || newRequest.Release != to || newRequest.Requester != old.Requester {
				t.Fatalf("redirect was not a new fixed target request: %+v", newRequest)
			}
			approver := "builder"
			if scenario.multilevel {
				approver = "deputy"
				if len(newRequest.Levels) != 2 || newRequest.Levels[0].Delegates[approver] != "builder" {
					t.Fatal("the target request did not capture its declared levels and current delegation")
				}
				if got := refuse(t, tn, "user", work.ID, "work.approval.approve", work.ApprovalType, newRequest.ID, map[string]any{}, txnAt); got == "ok" {
					t.Fatal("the original requester approved the redirected request")
				}
				decide(t, tn, approver, work.ID, "work.approval.approve", work.ApprovalType, newRequest.ID, map[string]any{}, txnAt.Add(5*time.Hour))
				intermediate, _ := platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), newRequest.ID)
				if intermediate.Level != 1 || intermediate.State != "pending" || intermediate.Levels[0].DecidedBy["builder"] != approver || intermediate.Submission != newRequest.Submission {
					t.Fatal("delegated first-level approval lost its actor, fixed input or next level")
				}
				restarted := compose()
				if err := restarted.Replay(entries); err != nil {
					t.Fatal(err)
				}
				tn = restarted
				attach()
				last := submission(tn, memberOf(t, tn, approver), work.ID, "work.approval.approve", work.ApprovalType, newRequest.ID, map[string]any{})
				before, count := snapshot(tn), len(entries)
				failAppend = true
				if _, err := tn.Submit(memberOf(t, tn, approver), last, txnAt.Add(5*time.Hour)); err == nil || snapshot(tn) != before || len(entries) != count {
					t.Fatal("a failed last approval exposed business completion or closed tasks")
				}
				failAppend = false
				first, refusal := tn.Submit(memberOf(t, tn, approver), last, txnAt.Add(5*time.Hour))
				if refusal != nil {
					t.Fatal(refusal)
				}
				before, count = snapshot(tn), len(entries)
				again, refusal := tn.Submit(memberOf(t, tn, approver), last, txnAt.Add(6*time.Hour))
				if refusal != nil || first.GetChangeId() != again.GetChangeId() || snapshot(tn) != before || len(entries) != count {
					t.Fatal("retry repeated the last approval or its business decision")
				}
			} else {
				decide(t, tn, approver, work.ID, "work.approval.approve", work.ApprovalType, newRequest.ID, map[string]any{}, txnAt.Add(5*time.Hour))
			}
		}
		if got := businessField(tn.records.types["build.asset"].info, tn.records.types["build.asset"].rows.get("A").value, "state"); got != "done" {
			t.Fatalf("target approval did not execute: %s", got)
		}
		a, _ = platform.Get[platform.ApprovalRequest](tn.automation(work.ID, false), requests[0].ID)
		count := len(entries)
		tn.Work(txnAt.Add(6 * time.Hour))
		all, _, _ := platform.Find[platform.ApprovalRequest](tn.automation(work.ID, false), platform.Query{})
		if len(all) != 4 || len(entries) < count {
			t.Fatal("redirect duplicated or rewrote accepted requests")
		}
	}
	restored := compose()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	after, ok := platform.Get[platform.ApprovalRequest](restored.automation(work.ID, false), requests[0].ID)
	if !ok || after.State != wantState || !bytes.Equal(original, []byte(after.Submission)) {
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
