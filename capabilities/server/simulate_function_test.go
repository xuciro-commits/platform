package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestCandidateFunctionFixtures(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tn, err := NewTenant("function-fixture", NewConsole("function-fixture",
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}), build.New("function-fixture"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	tn.AIClient = func(*http.Request) (*http.Response, error) { t.Fatal("used production model"); return nil, nil }
	tn.Secrets = func(string) ([]byte, bool) { t.Fatal("read production secret"); return nil, false }
	builder, _ := tn.Member("builder")
	keys := 0
	submit := func(schema, kind, id string, payload any) {
		t.Helper()
		keys++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprint(keys), Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: kind, Id: id}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
	}
	submit(build.ObjectType+".create", build.ObjectType, "O1", map[string]any{"name": "intake", "title": "Intake", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	submit(build.SchemaPublish, build.ObjectType, "O1", struct{}{})
	definition := platform.RecordAdviceFunction("build.intake", []string{"note"}, []string{build.Builder, build.User})
	definition.Name = "advice"
	submit(build.FunctionType+".create", build.FunctionType, "F1", definition)
	submit(build.SchemaFunction, build.FunctionType, "F1", struct{}{})
	submit(build.FunctionType+".edit", build.FunctionType, "F1", map[string]string{"instructions": "Candidate instructions"})
	submit("build.intake.create", "build.intake", "S1", map[string]string{"note": "Production source"})
	answer := `{"summary":"Fixture answer","category":"routine","review":false}`
	request := CandidateSimulationRequest{FunctionID: "F1", Model: "fixture/probe", As: "operator", At: at, Steps: []SimulationStep{
		{Type: "build.intake", ID: "S1", Action: "build.intake.create", Payload: json.RawMessage(`{"note":"Fixed sample"}`), Expect: "accepted"},
		{Type: build.FunctionCallType, ID: "R1", Action: build.SchemaFunctionCall, Payload: json.RawMessage(`{"name":"advice","source":"S1"}`), Expect: "accepted",
			Function: &build.FunctionFixture{Output: answer, InputTokens: 4, OutputTokens: 8, ExpectState: "ready", ExpectOutput: answer}},
	}}
	plan := map[string]any{"title": "Fixed advice", "function": "F1", "model": request.Model, "as": request.As, "at": at,
		"steps": []build.TestStep{
			{Type: request.Steps[0].Type, ID: "S1", Action: request.Steps[0].Action, Payload: string(request.Steps[0].Payload), Expect: "accepted"},
			{Type: build.FunctionCallType, ID: "R1", Action: build.SchemaFunctionCall, Payload: string(request.Steps[1].Payload), Expect: "accepted", Function: request.Steps[1].Function},
		}}
	submit(build.TestPlanType+".create", build.TestPlanType, "T1", plan)
	view, problem := tn.RecordOf(builder, build.TestPlanType, "T1", at)
	if problem != nil {
		t.Fatal(problem)
	}
	stored := view.Record.(build.TestPlan)
	if stored.Function != "F1" || stored.Model != request.Model || stored.Steps[1].Function.Output != answer {
		t.Fatalf("lost fixed inputs: %+v", stored)
	}
	CheckReplay(t, tn, entries, compose)
	before := snapshot(tn)
	result, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || !result.Recovered || result.Passed == nil || !*result.Passed || !result.Fixture || result.TestID == "" {
		t.Fatalf("fixture: %+v %v", result, refusal)
	}
	call := result.Steps[1].Functions[0]
	if call.Version != 2 || call.Model != request.Model || call.Output != answer || call.State != "ready" || call.Member != "operator" {
		t.Fatalf("wrong binding: %+v", call)
	}
	if !reflect.DeepEqual(before, snapshot(tn)) {
		t.Fatal("fixture changed production")
	}
	again, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("fixture is not repeatable: %+v %v", again, refusal)
	}
	request.Model = "fixture/other"
	changed, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || changed.TestID == result.TestID || changed.CandidateID != result.CandidateID || !*changed.Passed {
		t.Fatalf("model identity: %+v %v", changed, refusal)
	}
	request.Model = "fixture/probe"
	request.As = "builder"
	changed, refusal = tn.SimulateCandidate(builder, request)
	if refusal != nil || changed.TestID == result.TestID || changed.CandidateID != result.CandidateID || !*changed.Passed {
		t.Fatalf("actor identity: %+v %v", changed, refusal)
	}
	request.As = "operator"
	for _, test := range []struct {
		name, output, state string
		matches             bool
	}{
		{"unknown field", `{"summary":"Bad","category":"routine","review":false,"extra":1}`, "rejected", true},
		{"invalid JSON", "broken", "rejected", true},
		{"different answer", `{"summary":"Other","category":"routine","review":false}`, "ready", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &build.FunctionFixture{Output: test.output, InputTokens: 4, OutputTokens: 8, ExpectState: test.state}
			if test.state == "ready" {
				fixture.ExpectOutput = answer
			}
			request.Steps[1].Function = fixture
			got, err := tn.SimulateCandidate(builder, request)
			if err != nil || got.Passed == nil || *got.Passed != test.matches || got.TestID == result.TestID || got.CandidateID != result.CandidateID {
				t.Fatalf("fixture: %+v %v", got, err)
			}
		})
	}
	request.Steps[1].Function = nil
	if _, err := tn.SimulateCandidate(builder, request); err == nil {
		t.Fatal("unfinished call passed")
	}
	request.Model, request.Steps[1].Expect = "", "refused"
	missing, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || missing.Passed == nil || !*missing.Passed || missing.Steps[1].Accepted {
		t.Fatalf("missing model: %+v %v", missing, refusal)
	}
	request.Model, request.Steps[1].Expect = "fixture/probe", "accepted"
	request.Steps[1].Function = &build.FunctionFixture{Output: answer, OutputTokens: definition.MaxTokens + 1, ExpectState: "rejected"}
	budget, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || budget.Passed == nil || !*budget.Passed || budget.Steps[1].Functions[0].Output != "" {
		t.Fatalf("budget: %+v %v", budget, refusal)
	}
	request.Steps[1].Function = &build.FunctionFixture{Output: answer, OutputTokens: 8, ExpectState: "ready", ExpectOutput: answer}
	submit(build.FunctionType+".edit", build.FunctionType, "F1", map[string]string{"instructions": "Another candidate"})
	changed, refusal = tn.SimulateCandidate(builder, request)
	if refusal != nil || changed.Passed == nil || !*changed.Passed || changed.CandidateID == result.CandidateID || changed.TestID == result.TestID {
		t.Fatalf("candidate identity: %+v %v", changed, refusal)
	}
	submit(build.FunctionType+".edit", build.FunctionType, "F1", map[string]any{"roles": []string{build.Builder}})
	request.Steps[1].Function, request.Steps[1].Expect = nil, "refused"
	denied, refusal := tn.SimulateCandidate(builder, request)
	if refusal != nil || denied.Passed == nil || !*denied.Passed || denied.Steps[1].Accepted {
		t.Fatalf("function role: %+v %v", denied, refusal)
	}
	request.Steps = request.Steps[:1]
	if _, err := tn.SimulateCandidate(builder, request); err == nil {
		t.Fatal("uncalled function passed")
	}
}
