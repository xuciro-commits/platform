package platformserver

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/internal/host"
	"platformserver/platform"
)

func TestApplicationTrial(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "trial", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited"), seatOf("observer", "build:observer")}, build.New("trial"))
	}
	tn := compose()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	access := []build.Access{{Role: "operator", Read: "all", Create: true, Edit: true}, {Role: "limited", Read: "all", Create: true, Edit: true}, {Role: "observer", Read: "none"}}
	publishObject(t, tn, "builder", "LOG", map[string]any{"name": "triallog", "title": "Old log", "fields": []build.Field{{Name: "label", Title: "Label", Type: "text", Required: true}}, "access": []build.Access{{Role: "operator", Read: "all", Create: true}, {Role: "limited", Read: "all"}, {Role: "observer", Read: "all"}}}, at)
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "LOG", map[string]any{"title": "Current draft log"}, at)
	positive := booleanRule(">", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 0))
	txn := &platform.BusinessTxn{Subject: "build.trialjob", Guard: positive, Message: "Quantity must be positive", Steps: []platform.TxnStep{
		{Name: "complete", Kind: "update", Target: platform.TxnRef{Type: "build.trialjob", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "done", From: "=true"}}},
		{Name: "log", Kind: "create", Target: platform.TxnRef{Type: "build.triallog"}, Changes: []platform.TxnSet{{Field: "label", From: "=completed"}}},
	}}
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "JOB", map[string]any{"name": "trialjob", "title": "Job", "fields": []build.Field{{Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "done", Title: "Done", Type: "boolean"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}, "states": []build.State{{Name: "draft", Title: "Draft"}}, "access": access, "constraints": []platform.Constraint{{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Expr: positive, Message: "Quantity must be positive"}}, "actions": []build.Action{{Name: "complete", Title: "Complete job", From: []string{"draft"}, Roles: []string{"operator", "limited"}, Transaction: txn}}}, at)
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "UNRELATED", map[string]any{"name": "unrelated", "title": "Unrelated", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	samples := []platform.SampleRecords{{Type: "build.trialjob", Records: []json.RawMessage{platform.Raw(map[string]any{"id": "JOB-1", "qty": 2, "done": false, "state": "draft", "secret": "SYNTHETIC-SECRET"})}}}
	decide(t, tn, "builder", build.ID, "build.sample-set.create", build.SampleSetType, "SAMPLE", map[string]any{"name": "jobs", "title": "Job scenario", "samples": samples}, at)
	decide(t, tn, "builder", build.ID, "build.dataset.create", build.DatasetType, "DATASET", map[string]any{"name": "trialinput", "title": "Synthetic input"}, at)
	decide(t, tn, "builder", build.ID, "build.app.create", build.AppType, "APP", map[string]any{"name": "trialapp", "title": "Trial application", "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.trialjob"}, {App: build.ID, Kind: platform.AssetSampleSet, Name: "jobs"}, {App: build.ID, Kind: platform.AssetDataset, Name: "trialinput"}}}, at)
	builder := memberOf(t, tn, "builder")
	drafts, err := tn.app(build.ID).(host.ConstructionDrafts).ApplicationDrafts("APP")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drafts, build.JointDraftRef{Kind: platform.AssetObject, ID: "LOG"}) || slices.Contains(drafts, build.JointDraftRef{Kind: platform.AssetObject, ID: "UNRELATED"}) {
		t.Fatalf("application boundary: %+v", drafts)
	}
	request := CandidateSimulationRequest{ApplicationID: "APP", Sample: "jobs", Actors: []string{"operator", "limited", "observer"}, At: at, Steps: []SimulationStep{{Type: "build.trialjob", ID: "JOB-1", Read: true, Payload: platform.Raw(map[string]any{}), Expect: "accepted"}, {Type: "build.trialjob", ID: "JOB-1", Action: "build.trialjob.complete", Payload: platform.Raw(map[string]any{}), Expect: "accepted"}, {Type: "build.trialjob", ID: "JOB-1", Read: true, Payload: platform.Raw(map[string]any{}), Expect: "accepted"}}}
	snapshot := func() []byte {
		raw, _, err := tn.Snapshot(func() int64 { return 0 })
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	out, problem := tn.SimulateCandidate(builder, request)
	if problem != nil {
		t.Fatalf("trial: %s: %s", problem.Code, problem.Message)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("trial changed production state")
	}
	if len(out.Actors) != 3 || !out.Actors[0].Steps[1].Accepted || out.Actors[1].Steps[1].Accepted || out.Actors[2].Steps[1].Accepted {
		t.Fatalf("role outcomes: %s", platform.Raw(out))
	}
	for _, actor := range out.Actors {
		if !actor.Recovered {
			t.Fatal("isolated recovery missing")
		}
		if strings.Contains(string(platform.Raw(actor)), "SYNTHETIC-SECRET") {
			t.Fatal("withheld sample leaked in explanation")
		}
	}
	successful := out.Actors[0].Steps[1]
	kinds := map[string]bool{}
	for _, event := range successful.Trace {
		kinds[event.Kind] = true
	}
	for _, kind := range []string{"rule", "constraint", "transaction-step", "transaction-change", "authorization"} {
		if !kinds[kind] {
			t.Fatalf("missing original executor observation %s", kind)
		}
	}
	if !strings.Contains(string(out.Actors[0].Steps[2].Read), `"done":true`) || strings.Contains(string(out.Actors[1].Steps[2].Read), `"done":true`) {
		t.Fatal("transaction rollback or role isolation failed")
	}
	for _, event := range out.Actors[2].Steps[0].Trace {
		if event.Request != nil && event.Authorization != nil && !event.Authorization.Allow && event.Request.Record != "" {
			t.Fatal("denied reads exposed a hidden record ID")
		}
	}
	if out.Actors[2].Steps[0].RolledBack {
		t.Fatal("a refused read was described as a rolled-back write")
	}
	if len(out.Actors[1].Steps[1].Changes) != 0 || !out.Actors[1].Steps[1].RolledBack {
		t.Fatal("refused writes appeared committed")
	}
	repeated, problem := tn.SimulateCandidate(builder, request)
	if problem != nil || !reflect.DeepEqual(out, repeated) {
		t.Fatalf("fixed scenario differs: %+v", problem)
	}
	if _, problem := tn.SimulateCandidate(memberOf(t, tn, "operator"), request); problem == nil {
		t.Fatal("non-builder simulation admitted")
	}
	bad := request
	bad.Actors = []string{"unknown"}
	if _, problem := tn.SimulateCandidate(builder, bad); problem == nil {
		t.Fatal("uncontrolled identity admitted")
	}
	bad = request
	bad.Sample = "unrelated"
	if _, problem := tn.SimulateCandidate(builder, bad); problem == nil {
		t.Fatal("sample outside application closure admitted")
	}
	bad = request
	bad.Steps = []SimulationStep{{Type: build.DatasetType, ID: "OTHER", Action: "build.dataset.create", Payload: platform.Raw(map[string]any{"name": "other", "title": "Other"})}}
	if _, problem := tn.SimulateCandidate(builder, bad); problem == nil {
		t.Fatal("undeclared construction action admitted")
	}
	// The exact same scenario is an ordinary TestPlan, with no new test store.
	testSteps := []map[string]any{}
	for _, step := range request.Steps {
		var values map[string]any
		_ = json.Unmarshal(platform.Raw(step), &values)
		values["payload"] = string(step.Payload)
		testSteps = append(testSteps, values)
	}
	decide(t, tn, "builder", build.ID, "build.testplan.create", build.TestPlanType, "PLAN", map[string]any{"title": "Complete job regression", "application": "APP", "sample": "jobs", "actors": request.Actors, "at": at, "steps": testSteps}, at)
	review, err := tn.PreviewReleaseDrafts(builder, drafts)
	if err != nil || review.Diagnostic != "" {
		t.Fatalf("release: %v %s", err, review.Diagnostic)
	}
	saved, err := tn.SaveReleaseCandidates(builder, drafts, review.CandidateID, "save-trial", at)
	if err != nil {
		t.Fatal(err)
	}
	frozen := request
	frozen.ApplicationID = ""
	frozen.CandidateID = saved
	first, problem := tn.SimulateCandidate(builder, frozen)
	if problem != nil {
		t.Fatal(problem)
	}
	// Editing both draft rule and sample does not affect a retained candidate.
	changed := samples
	samples[0].Records = []json.RawMessage{platform.Raw(map[string]any{"id": "JOB-1", "qty": 0, "state": "draft"})}
	decide(t, tn, "builder", build.ID, "build.sample-set.edit", build.SampleSetType, "SAMPLE", map[string]any{"samples": changed}, at)
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "JOB", map[string]any{"actions": []build.Action{}}, at)
	second, problem := tn.SimulateCandidate(builder, frozen)
	if problem != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("retained version changed: %+v", problem)
	}
	// Activation retains the original sample asset beside the application;
	// mutable draft edits are not promoted into those bytes.
	if _, err := tn.ActivateRelease(builder, saved, "activate-trial", at); err != nil {
		t.Fatal(err)
	}
	assets, err := tn.app(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool {
		return a.Ref.Kind == platform.AssetSampleSet && a.Ref.Name == "jobs" && strings.Contains(string(a.Body), `"qty":2`)
	}) {
		t.Fatal("application did not retain its frozen sample")
	}
	// Restore retains exactly the same owner bytes and repeatable scenario.
	raw := snapshot()
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	replayed, problem := restored.SimulateCandidate(memberOf(t, restored, "builder"), frozen)
	if problem != nil || !reflect.DeepEqual(first, replayed) {
		t.Fatalf("recovered trial differs: %+v", problem)
	}
	// Production and the private run share one transaction executor and refusal.
	decide(t, tn, "operator", build.ID, "build.trialjob.create", "build.trialjob", "JOB-1", map[string]any{"qty": 2}, at)
	decide(t, tn, "operator", build.ID, "build.trialjob.complete", "build.trialjob", "JOB-1", map[string]any{}, at)
	decide(t, tn, "limited", build.ID, "build.trialjob.create", "build.trialjob", "LIMITED", map[string]any{"qty": 2}, at)
	if got := refuse(t, tn, "limited", build.ID, "build.trialjob.complete", "build.trialjob", "LIMITED", map[string]any{}, at); !strings.HasSuffix(got, first.Actors[1].Steps[1].Refusal) {
		t.Fatal("production accepted a trial refusal")
	}
}
