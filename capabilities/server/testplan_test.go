package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestSavedCandidatePlansUseRecordsAndAcceptedRecovery(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant("plans", NewConsole("plans", seat("builder", build.Builder), seat("user", build.User)), build.New("plans"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	user, _ := tn.Member("user")
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		journal = append(journal, e)
		return e.Body, nil
	}
	keys := 0
	submit := func(m platform.Member, schema, id string, payload any, revision *uint32) *pb.Submission {
		keys++
		raw, _ := json.Marshal(payload)
		typ := build.TestPlanType
		if schema == build.ObjectType+".create" || schema == build.ObjectType+".edit" {
			typ = build.ObjectType
		}
		return &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw, ExpectedRevision: revision}
	}
	create := submit(builder, build.ObjectType+".create", "O1", map[string]any{"name": "visit", "title": "Visit", "fields": []build.Field{{Name: "guest", Title: "Guest", Type: "text"}},
		"states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}}}, nil)
	if _, err := tn.Submit(builder, create, at); err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{"title": "Fixed visits", "object": "O1", "as": "user", "at": at, "steps": []build.TestStep{
		{Type: "build.visit", ID: "TEST", Action: "build.visit.create", Payload: `{"guest":"Ada"}`, Expect: "accepted"},
		{Type: "build.visit", ID: "TEST", Action: "build.visit.close", Payload: `{}`, Expect: "accepted"},
		{Type: "build.visit", ID: "TEST", Action: "build.visit.close", Payload: `{}`, Expect: "refused"},
	}}
	request := submit(builder, build.TestPlanType+".create", "T1", plan, nil)
	before, count := snapshot(tn), len(journal)
	fail = true
	if _, err := tn.Submit(builder, request, at); err == nil || snapshot(tn) != before || len(journal) != count {
		t.Fatal("failed append exposed a plan")
	}
	fail = false
	if _, err := tn.Submit(builder, request, at); err != nil {
		t.Fatal(err)
	}
	if len(journal) != count+1 || journal[count].Kind != "accepted-result" {
		t.Fatal("plan bypassed accepted results")
	}
	if _, err := tn.Submit(builder, request, at.Add(time.Hour)); err != nil || len(journal) != count+1 {
		t.Fatal("plan retry duplicated a decision")
	}
	if rows, _ := tn.Records(user, build.TestPlanType, platform.Query{}, at); len(rows.Records) != 0 {
		t.Fatal("user read private samples")
	}
	if _, err := tn.RecordOf(user, build.TestPlanType, "T1", at); err == nil {
		t.Fatal("user read plan by ID")
	}
	foreign := builder
	foreign.Tenant = "elsewhere"
	if _, err := tn.RecordOf(foreign, build.TestPlanType, "T1", at); err == nil {
		t.Fatal("foreign builder read plan")
	}
	if _, err := tn.Submit(foreign, submit(foreign, build.TestPlanType+".edit", "T1", map[string]any{"title": "foreign"}, nil), at); err == nil {
		t.Fatal("foreign builder edited plan")
	}
	if _, err := tn.Submit(user, submit(user, build.TestPlanType+".edit", "T1", map[string]any{"title": "stolen"}, nil), at); err == nil {
		t.Fatal("user edited plan")
	}
	for _, entity := range tn.Entities(user) {
		if entity.Type == build.TestPlanType {
			t.Fatal("private plan appeared in user types")
		}
	}
	read := func(tenant *Tenant) build.TestPlan {
		view, err := tenant.RecordOf(builder, build.TestPlanType, "T1", at)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view.Record)
		var plan build.TestPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	run := func(tenant *Tenant, saved build.TestPlan) CandidateSimulation {
		input := CandidateSimulationRequest{ObjectID: string(saved.Object), As: saved.As, At: saved.At}
		for _, step := range saved.Steps {
			input.Steps = append(input.Steps, SimulationStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect})
		}
		before := snapshot(tenant)
		out, err := tenant.SimulateCandidate(builder, input)
		if err != nil || out.Passed == nil || !*out.Passed || !out.Recovered || snapshot(tenant) != before {
			t.Fatalf("saved plan failed: %+v, %v", out, err)
		}
		return out
	}
	saved := read(tn)
	first := run(tn, saved)
	restored := compose()
	raw, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	if got := run(restored, read(restored)); !reflect.DeepEqual(first, got) {
		t.Fatal("restored plan changed inputs/results")
	}
	stale := saved.Revision - 1
	if _, err := tn.Submit(builder, submit(builder, build.TestPlanType+".edit", "T1", map[string]any{"title": "stale"}, &stale), at); err == nil || read(tn).Title != saved.Title {
		t.Fatal("stale edit overwrote plan")
	}
	// Invalid fixed plans never reach stored rows; ordinary records own the patch.
	for _, bad := range []any{map[string]any{"steps": []build.TestStep{}}, map[string]any{"at": "bad"}, map[string]any{"steps": []build.TestStep{{Type: "build.visit", ID: "TEST", Action: "build.visit.close", Payload: `null`, Expect: "accepted"}}}} {
		if _, err := tn.Submit(builder, submit(builder, build.TestPlanType+".edit", "T1", bad, nil), at); err == nil {
			t.Fatal("invalid plan saved")
		}
	}
	// The plan's outcomes are not a cached pass for changed candidate rules.
	edit := submit(builder, build.ObjectType+".edit", "O1", map[string]any{"actions": []build.Action{{Name: "close", Title: "Close", From: []string{"done"}, To: "done"}}}, nil)
	if _, err := tn.Submit(builder, edit, at); err != nil {
		t.Fatal(err)
	}
	input := CandidateSimulationRequest{ObjectID: "O1", As: "user", At: at}
	for _, step := range saved.Steps {
		input.Steps = append(input.Steps, SimulationStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect})
	}
	changed, refusal := tn.SimulateCandidate(builder, input)
	if refusal != nil || changed.Passed == nil || *changed.Passed || changed.CandidateID == first.CandidateID || changed.Steps[1].Matched == nil || *changed.Steps[1].Matched {
		t.Fatalf("changed rules reused old pass: %+v, %v", changed, refusal)
	}
	for i := range input.Steps {
		input.Steps[i].Expect = ""
	}
	unasserted, refusal := tn.SimulateCandidate(builder, input)
	if refusal != nil || unasserted.Passed != nil {
		t.Fatal("unasserted demonstration claimed a pass")
	}
	CheckReplay(t, tn, journal, compose)
}
