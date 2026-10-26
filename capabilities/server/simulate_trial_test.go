package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/internal/host"
	"platformserver/platform"
)

func TestApplicationTrialUsesExplicitOrganizationWithoutCopyingLiveMemberships(t *testing.T) {
	const id = "organization-trial"
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("other", "build:operator")}
	live := platform.OrgSeed{Units: []platform.Unit{{ID: "secret", Name: "Live private organization", Kind: "department"}}, Memberships: []platform.Membership{{Party: "member:other", Unit: "secret", Role: "employee"}}}
	tn := composeTenant(t, id, seats, build.New(id), enterprise.New(id, live))
	decide(t, tn, "builder", "build", "build.object.create", "build.object", "ASSIGNMENT", map[string]any{
		"name": "assignment", "title": "Assignment", "plural": "Assignments",
		"fields": []construction.Field{{Name: "unit", Title: "Unit", Type: "reference", Ref: "enterprise.element", Stereotype: "ActualOrganization", Required: true}, {Name: "note", Title: "Note", Type: "text"}},
		"scope":  construction.ObjectScope{Unit: "unit", Structure: "site"}, "access": []construction.Access{{Role: "operator", Read: "unit", Create: true, Edit: true}},
	}, at)
	organization := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}}, Units: []platform.Unit{{ID: "synthetic", Name: "Synthetic department", Kind: "department"}}, Memberships: []platform.Membership{{Party: "member:operator", Unit: "synthetic", Role: "employee"}}}
	request := CandidateSimulationRequest{ObjectID: "ASSIGNMENT", Actors: []string{"operator", "other"}, At: at, Organization: &organization,
		Samples: []SimulationSample{{Type: "build.assignment", Records: []json.RawMessage{platform.Raw(map[string]any{"id": "A", "unit": "synthetic", "note": "Synthetic task"})}}},
		Steps:   []SimulationStep{{Type: "build.assignment", ID: "A", Read: true, Payload: platform.Raw(map[string]any{})}, {Type: "build.assignment", ID: "A", Action: "build.assignment.edit", Payload: platform.Raw(map[string]any{"note": "Completed synthetic task"})}, {Type: "build.assignment", ID: "LEAK", Action: "build.assignment.create", Payload: platform.Raw(map[string]any{"unit": "secret", "note": "Must not resolve live organizations"}), Expect: "refused"}},
	}
	before := snapshot(tn)
	result, failure := tn.SimulateCandidate(memberOf(t, tn, "builder"), request)
	if failure != nil || len(result.Actors) != 2 {
		t.Fatalf("isolated organization: %+v %s", failure, platform.Raw(result))
	}
	if !result.Actors[0].Steps[0].Accepted || !result.Actors[0].Steps[1].Accepted || result.Actors[1].Steps[0].Accepted || result.Actors[1].Steps[1].Accepted || !result.Actors[0].Recovered || !result.Actors[1].Recovered {
		t.Fatalf("same-role unit isolation or recovery: %s", platform.Raw(result))
	}
	for _, actor := range result.Actors {
		if actor.Steps[2].Accepted {
			t.Fatal("a live organization or membership entered the sandbox")
		}
	}
	if snapshot(tn) != before {
		t.Fatal("the synthetic organization changed live state or grants")
	}
}

func TestApplicationTrial(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "trial", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited"), seatOf("observer", "build:observer")}, build.New("trial"))
	}
	tn := compose()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	access := []construction.Access{{Role: "operator", Read: "all", Create: true, Edit: true}, {Role: "limited", Read: "all", Create: true, Edit: true}, {Role: "observer", Read: "none"}}
	publishObject(t, tn, "builder", "LOG", map[string]any{"name": "triallog", "title": "Old log", "fields": []construction.Field{{Name: "label", Title: "Label", Type: "text", Required: true}}, "access": []construction.Access{{Role: "operator", Read: "all", Create: true}, {Role: "limited", Read: "all"}, {Role: "observer", Read: "all"}}}, at)
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "LOG", map[string]any{"title": "Current draft log"}, at)
	positive := booleanRule(">", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 0))
	txn := &platform.BusinessTxn{Subject: "build.trialjob", Guard: positive, Message: "Quantity must be positive", Steps: []platform.TxnStep{
		{Name: "complete", Kind: "update", Target: platform.TxnRef{Type: "build.trialjob", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "done", From: "=true"}}},
		{Name: "log", Kind: "create", Target: platform.TxnRef{Type: "build.triallog"}, Changes: []platform.TxnSet{{Field: "label", From: "=completed"}}},
	}}
	decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "JOB", map[string]any{"name": "trialjob", "title": "Job", "fields": []construction.Field{{Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "done", Title: "Done", Type: "boolean"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{construction.Builder}}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "access": access, "constraints": []platform.Constraint{{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Expr: positive, Message: "Quantity must be positive"}}, "actions": []construction.Action{{Name: "complete", Title: "Complete job", From: []string{"draft"}, Roles: []string{"operator", "limited"}, Transaction: txn}}}, at)
	decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "UNRELATED", map[string]any{"name": "unrelated", "title": "Unrelated", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	samples := []platform.SampleRecords{{Type: "build.trialjob", Records: []json.RawMessage{platform.Raw(map[string]any{"id": "JOB-1", "qty": 2, "done": false, "state": "draft", "secret": "SYNTHETIC-SECRET"})}}}
	decide(t, tn, "builder", construction.ID, "build.sample-set.create", build.SampleSetType, "SAMPLE", map[string]any{"name": "jobs", "title": "Job scenario", "samples": samples}, at)
	decide(t, tn, "builder", construction.ID, "build.dataset.create", construction.DatasetType, "DATASET", map[string]any{"name": "trialinput", "title": "Synthetic input"}, at)
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "APP", map[string]any{"name": "trialapp", "title": "Trial application", "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: "build.trialjob"}, {App: construction.ID, Kind: platform.AssetSampleSet, Name: "jobs"}, {App: construction.ID, Kind: platform.AssetDataset, Name: "trialinput"}}}, at)
	builder := memberOf(t, tn, "builder")
	drafts, err := tn.app(construction.ID).(host.ConstructionDrafts).ApplicationDrafts("APP")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drafts, construction.JointDraftRef{Kind: platform.AssetObject, ID: "LOG"}) || slices.Contains(drafts, construction.JointDraftRef{Kind: platform.AssetObject, ID: "UNRELATED"}) {
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
	bad.Steps = []SimulationStep{{Type: construction.DatasetType, ID: "OTHER", Action: "build.dataset.create", Payload: platform.Raw(map[string]any{"name": "other", "title": "Other"})}}
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
	decide(t, tn, "builder", construction.ID, "build.testplan.create", construction.TestPlanType, "PLAN", map[string]any{"title": "Complete job regression", "application": "APP", "sample": "jobs", "actors": request.Actors, "at": at, "steps": testSteps}, at)
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
	decide(t, tn, "builder", construction.ID, "build.sample-set.edit", build.SampleSetType, "SAMPLE", map[string]any{"samples": changed}, at)
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "JOB", map[string]any{"actions": []construction.Action{}}, at)
	second, problem := tn.SimulateCandidate(builder, frozen)
	if problem != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("retained version changed: %+v", problem)
	}
	// Activation retains the original sample asset beside the application;
	// mutable draft edits are not promoted into those bytes.
	if _, err := tn.ActivateRelease(builder, saved, "activate-trial", at); err != nil {
		t.Fatal(err)
	}
	assets, err := tn.app(construction.ID).(*build.Build).ReleaseAssets()
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
	decide(t, tn, "operator", construction.ID, "build.trialjob.create", "build.trialjob", "JOB-1", map[string]any{"qty": 2}, at)
	decide(t, tn, "operator", construction.ID, "build.trialjob.complete", "build.trialjob", "JOB-1", map[string]any{}, at)
	decide(t, tn, "limited", construction.ID, "build.trialjob.create", "build.trialjob", "LIMITED", map[string]any{"qty": 2}, at)
	if got := refuse(t, tn, "limited", construction.ID, "build.trialjob.complete", "build.trialjob", "LIMITED", map[string]any{}, at); !strings.HasSuffix(got, first.Actors[1].Steps[1].Refusal) {
		t.Fatal("production accepted a trial refusal")
	}
}
