package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestProcessFunctionsKeepVersionsAndReleaseAcrossRecovery(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tn, err := NewTenant("process-function", NewConsole("process-function",
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, PlatformApp: Admin, ai.ID: ai.Admin, flow.ID: flow.Admin}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}),
			ai.New("process-function"), work.New("process-function"), flow.New("process-function"), build.New("process-function"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	keys := 0
	must := func(who, app, schema, typ, id string, payload any) {
		t.Helper()
		keys++
		_, err := tn.Submit(member(who), &pb.Submission{TenantId: tn.ID, PrincipalId: who, Authority: app, IdempotencyKey: fmt.Sprint(keys), Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatalf("%s: %v fault=%v", schema, err, tn.fault.Load())
		}
	}
	tick := func() {
		at = at.Add(2 * time.Second)
		tn.Work(at)
		if tn.quarantined() {
			t.Fatal(tn.fault.Load())
		}
	}
	must("builder", build.ID, build.ObjectType+".create", build.ObjectType, "O", map[string]any{"name": "intake", "title": "Intake", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}}})
	must("builder", build.ID, build.SchemaPublish, build.ObjectType, "O", struct{}{})
	definition := platform.RecordAdviceFunction("build.intake", []string{"note"}, []string{build.User, build.Builder})
	definition.Name = "advice"
	must("builder", build.ID, build.FunctionType+".create", build.FunctionType, "F", definition)
	must("builder", build.ID, build.SchemaFunction, build.FunctionType, "F", struct{}{})
	pageSections := []build.Section{{Widget: "table", Fields: []string{"note"}}, {Widget: "function", Function: &platform.FunctionRef{Name: "advice", Version: 1}}}
	must("builder", build.ID, build.PageType+".create", build.PageType, "PAGE", map[string]any{"name": "intakeadvice", "title": "Intake advice", "object": "build.intake", "sections": pageSections})
	must("builder", build.ID, build.SchemaRelease, build.PageType, "PAGE", struct{}{})
	pagePreview, err := tn.PreviewRelease(member("builder"), platform.AssetPage, "PAGE")
	if err != nil || pagePreview.Diagnostic != "" {
		t.Fatalf("page function binding: %+v %v", pagePreview, err)
	}
	pageRoot := []platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "intakeadvice"}}
	firstPage, err := tn.ReleaseCandidate(pageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Assets) != 3 {
		t.Fatalf("page omitted source or function: %+v", firstPage.Assets)
	}
	steps := []build.ProcessStep{{Name: "gate", Kind: "ask", Ask: build.User, Answers: []string{"continue"}, Next: "infer"}, {Name: "infer", Kind: "ai", Function: &platform.FunctionRef{Name: "advice", Version: 1}, Next: "review"}, {Name: "review", Kind: "ask", Ask: build.User, Answers: []string{"approve"}, Next: "close"}, {Name: "close", Kind: "action", Act: "close"}}
	must("builder", build.ID, build.ProcessType+".create", build.ProcessType, "P", map[string]any{"name": "review", "title": "Review intake", "object": "build.intake", "when": "open", "steps": steps})

	before := snapshot(tn)
	request := CandidateSimulationRequest{ProcessID: "P", As: "operator", Model: "fixture/probe", At: at, Steps: []SimulationStep{
		{Type: "build.intake", ID: "TEST", Action: "build.intake.create", Payload: json.RawMessage(`{"note":"Fixed sample"}`), AdvanceSeconds: 2, Expect: "accepted"},
		{Type: "build.intake", ID: "TEST", Flow: "build.review", Step: "gate", Answer: "continue", AdvanceSeconds: 2, Expect: "accepted",
			Function: &build.FunctionFixture{Output: `{"summary":"Check source","category":"review","review":true}`, ExpectState: "ready"}},
		{Type: "build.intake", ID: "TEST", AdvanceSeconds: 2, Expect: "accepted"},
		{Type: "build.intake", ID: "TEST", Flow: "build.review", Step: "review", Answer: "approve", AdvanceSeconds: 2, Expect: "accepted"},
	}}
	result, problem := tn.SimulateCandidate(member("builder"), request)
	if problem != nil || !result.Recovered || result.Passed == nil || !*result.Passed || len(result.Steps[3].Flows) != 1 || result.Steps[3].Flows[0].State != "done" {
		t.Fatalf("native function simulation: %+v %v", result, problem)
	}
	if snapshot(tn) != before {
		t.Fatal("simulation changed production")
	}
	request.Steps = request.Steps[:2]
	request.Steps[1].Function = nil
	if _, problem := tn.SimulateCandidate(member("builder"), request); problem == nil {
		t.Fatal("unfinished function passed")
	}
	request.Steps[1].Function = &build.FunctionFixture{Output: "invalid JSON", ExpectState: "rejected"}
	request.Steps = append(request.Steps, SimulationStep{Type: "build.intake", ID: "TEST", AdvanceSeconds: 2, Expect: "accepted"})
	rejected, problem := tn.SimulateCandidate(member("builder"), request)
	if problem != nil || rejected.Passed == nil || !*rejected.Passed || len(rejected.Steps[2].Tasks) != 1 || len(rejected.Steps[2].Functions) != 1 || rejected.Steps[2].Functions[0].Output != "" {
		t.Fatalf("rejected model answer did not reach human review: %+v %v", rejected, problem)
	}
	must("builder", build.ID, build.SchemaProcess, build.ProcessType, "P", struct{}{})
	reportCost := true
	reportInvalid := false
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		usage := map[string]any{"prompt_tokens": 4, "completion_tokens": 8}
		if reportCost {
			usage["cost"] = 0.01
		}
		answer := `{"summary":"Check source","category":"review","review":true}`
		if reportInvalid {
			answer = `not JSON`
			reportInvalid = false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}, "usage": usage})
	}))
	defer model.Close()
	must("builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]string{"kind": "local", "baseUrl": model.URL})
	must("builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "users"})
	must("builder", PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": "fixture/probe"})
	must("builder", build.ID, build.TestPlanType+".create", build.TestPlanType, "EVAL", map[string]any{
		"title": "Synthetic advice quality", "function": "F", "model": "fixture/probe", "at": at,
		"steps": []build.TestStep{{Type: "build.intake", ID: "SAMPLE", Action: "build.intake.create", Payload: `{"note":"Synthetic"}`, Expect: "accepted"}},
		"evaluation": []build.EvaluationPolicy{{MinQuality: 0.5, MaxCostUSD: 0.05, MaxLatencyMillis: 300000,
			Cases: []build.EvaluationCase{{Name: "synthetic", Input: json.RawMessage(`{"note":"Synthetic"}`), Expected: json.RawMessage(`{"summary":"Check source","category":"review","review":true}`)}}}},
	})
	firstEvaluation := true
	evaluate := func(id, want string) {
		t.Helper()
		if _, err := tn.ActivateRelease(member("builder"), id, "before-evaluation", at); err == nil {
			t.Fatal("function candidate activated without a measured evaluation")
		}
		keys++
		reportID, err := tn.EvaluateRelease(member("builder"), ReleaseEvaluationRequest{CandidateID: id, PlanID: "EVAL", Key: fmt.Sprint(keys)}, at)
		if err != nil {
			t.Fatal(err)
		}
		attempts := 0
		for _, effect := range tn.Effects(at) {
			var ask modelAsk
			if json.Unmarshal([]byte(effect.Body), &ask) != nil || !ask.Evaluation || !strings.HasPrefix(ask.Call, reportID+":") {
				continue
			}
			outcome, usage := tn.sendModel(effect, at)
			if (outcome.Result != "delivered" && outcome.Result != "rejected") || want == "passed" && outcome.Result != "delivered" || usage == nil || usage.CostReported != reportCost {
				t.Fatalf("evaluation model call: %+v %+v", outcome, usage)
			}
			tn.settleWithUsage(effect.ID, outcome, usage, at)
			attempts++
		}
		if attempts != build.EvaluationRepeats {
			t.Fatalf("evaluated %d times", attempts)
		}
		report, ok := platform.Get[build.Evaluation](tn.automation(build.ID, false), reportID)
		if !ok || report.State != want || report.CostComplete != reportCost {
			t.Fatalf("evaluation did not pass: %+v", report)
		}
	}
	activate := func() string {
		t.Helper()
		preview, err := tn.PreviewRelease(member("builder"), platform.AssetFlow, "P")
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		keys++
		id, err := tn.SaveReleaseCandidate(member("builder"), platform.AssetFlow, "P", preview.CandidateID, fmt.Sprint(keys), at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tn.EvaluateRelease(member("operator"), ReleaseEvaluationRequest{CandidateID: id, PlanID: "EVAL", Key: "unauthorised"}, at); err == nil {
			t.Fatal("operator started a private function evaluation")
		}
		if firstEvaluation {
			firstEvaluation = false
			reportCost = false
			evaluate(id, "failed")
			if _, err := tn.ActivateRelease(member("builder"), id, "missing-cost", at); err == nil {
				t.Fatal("unreported provider cost passed the release gate")
			}
			reportCost = true
			reportInvalid = true
			evaluate(id, "failed")
			if _, err := tn.ActivateRelease(member("builder"), id, "invalid-answer", at); err == nil {
				t.Fatal("one malformed model answer passed the release gate")
			}
		}
		evaluate(id, "passed")
		must("builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "everyone"})
		if _, err := tn.ActivateRelease(member("builder"), id, "changed-config", at); err == nil {
			t.Fatal("changed model configuration passed the release gate")
		}
		must("builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]string{"access": "users"})
		keys++
		if _, err := tn.ActivateRelease(member("builder"), id, fmt.Sprint(keys), at); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// A development run must remain unbound even after a release is activated.
	must("operator", build.ID, "build.intake.create", "build.intake", "DEV", map[string]string{"note": "Before activation"})
	tick()
	keys++
	_, impersonation := tn.Submit(member("operator"), &pb.Submission{TenantId: tn.ID, PrincipalId: "operator", Authority: build.ID, IdempotencyKey: fmt.Sprint(keys),
		Schema: &pb.SchemaRef{Name: build.SchemaFunctionCall, Version: 1}, Target: &pb.EntityRef{Type: build.FunctionCallType, Id: "SPOOF"},
		Payload: json.RawMessage(`{"name":"advice","source":"DEV","release":""}`)}, at)
	if impersonation == nil {
		t.Fatal("human request overrode its release binding")
	}
	first := activate()
	must("operator", build.ID, "build.intake.create", "build.intake", "OLD", map[string]string{"note": "First release"})
	tick()
	CheckReplay(t, tn, entries, compose)
	must("builder", build.ID, build.FunctionType+".edit", build.FunctionType, "F", map[string]string{"instructions": "New function instructions"})
	must("builder", build.ID, build.SchemaFunction, build.FunctionType, "F", struct{}{})
	retainedPage, err := tn.ReleaseCandidate(pageRoot)
	if err != nil || retainedPage.ID != firstPage.ID {
		t.Fatalf("page drifted to latest function: %v %v", retainedPage.ID, err)
	}
	keys++
	pageRelease, err := tn.SaveReleaseCandidate(member("builder"), platform.AssetPage, "PAGE", firstPage.ID, fmt.Sprint(keys), at)
	if err != nil {
		t.Fatalf("save retained page: %v", err)
	}
	evaluate(pageRelease, "passed")
	keys++
	if activated, err := tn.ActivateRelease(member("builder"), pageRelease, fmt.Sprint(keys), at); err != nil || activated != firstPage.ID {
		t.Fatalf("activate page pinned to old function: %s %v", activated, err)
	}
	CheckReplay(t, tn, entries, compose)
	conflict, err := tn.PreviewRelease(member("builder"), platform.AssetFunction, "F")
	if err == nil && conflict.Diagnostic == "" {
		t.Fatalf("function draft silently replaced a flow pin: %+v %v", conflict, err)
	}
	steps[1].Function = &platform.FunctionRef{Name: "advice", Version: 2}
	must("builder", build.ID, build.ProcessType+".edit", build.ProcessType, "P", map[string]any{"steps": steps})
	must("builder", build.ID, build.SchemaProcess, build.ProcessType, "P", struct{}{})
	second := activate()
	pageSections[1].Function = &platform.FunctionRef{Name: "advice", Version: 2}
	must("builder", build.ID, build.PageType+".edit", build.PageType, "PAGE", map[string]any{"sections": pageSections})
	must("builder", build.ID, build.SchemaRelease, build.PageType, "PAGE", struct{}{})
	pagePreview, err = tn.PreviewRelease(member("builder"), platform.AssetPage, "PAGE")
	if err != nil || pagePreview.Diagnostic != "" {
		t.Fatalf("updated page function binding: %+v %v", pagePreview, err)
	}
	updatedPage, err := tn.ReleaseCandidate(pageRoot)
	if err != nil || updatedPage.ID == firstPage.ID {
		t.Fatalf("updated page retained old function: %v %v", updatedPage.ID, err)
	}
	keys++
	if _, err := tn.ActivateRelease(member("builder"), pageRelease, fmt.Sprint(keys), at); err == nil {
		t.Fatal("changed page activated a stale saved candidate")
	}
	if first == second {
		t.Fatal("new function reused release")
	}
	must("operator", build.ID, "build.intake.create", "build.intake", "NEW", map[string]string{"note": "Second release"})
	tick()
	CheckReplay(t, tn, entries, compose)
	answer := func(source, answer string) {
		t.Helper()
		inbox, err := tn.Read(member("operator"), "inbox")
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range inbox.([]work.WorkTask) {
			if task.Ref == "build.intake/"+source {
				must("operator", work.ID, "work.task.complete", work.TaskType, task.ID, map[string]string{"answer": answer})
				tick()
				return
			}
		}
		t.Fatalf("no human task for %s", source)
	}
	for _, tc := range []struct {
		id, release string
		version     int
	}{{"DEV", "", 1}, {"OLD", first, 1}, {"NEW", second, 2}} {
		answer(tc.id, "continue")
		pending := pendingFunctionEffects(tn)
		if len(pending) != 1 {
			t.Fatalf("native function did not queue: %+v", pending)
		}
		var ask modelAsk
		json.Unmarshal([]byte(pending[0].Body), &ask)
		if ask.Function == nil || ask.Function.Call.Version != tc.version || ask.Function.Call.Release != tc.release || ask.Function.Member != "operator" {
			t.Fatalf("wrong retained binding: %+v", ask.Function)
		}
		if (tc.version == 2) != strings.Contains(ask.Function.Definition.Instructions, "New function") {
			t.Fatal("function prompt drifted")
		}
		CheckReplay(t, tn, entries, compose)
		matched, err := settleFunctionFixture(tn, member("operator"), build.FunctionFixture{Output: `{"summary":"Check source","category":"review","review":true}`, InputTokens: 4, OutputTokens: 8, ExpectState: "ready"}, at)
		if err != nil || !matched {
			t.Fatalf("native reply: %v %v", matched, err)
		}
		tick()
		answer(tc.id, "approve")
		view, problem := tn.RecordOf(member("builder"), flow.InstanceType, "build.review:"+tc.id, at)
		if problem != nil || view.Record.(flow.FlowInstance).State != "done" {
			t.Fatalf("flow did not finish: %+v %v", view, problem)
		}
	}
	CheckReplay(t, tn, entries, compose)
	// The source object underlies both operator surfaces. Its affected closure
	// must include their shared pinned function, page and native workflow.
	joint, err := tn.PreviewRelease(member("builder"), platform.AssetObject, "O")
	if err != nil || joint.Diagnostic != "" || joint.CandidateID == "" {
		t.Fatalf("shared function candidate: %+v %v", joint, err)
	}
	keys++
	jointID, err := tn.SaveReleaseCandidate(member("builder"), platform.AssetObject, "O", joint.CandidateID, fmt.Sprint(keys), at)
	if err != nil || jointID != joint.CandidateID {
		t.Fatalf("save shared candidate: %s %v", jointID, err)
	}
	closed, err := platform.ReadCandidate(jointID, tn.releases.candidates[jointID])
	if err != nil {
		t.Fatal(err)
	}
	if len(joint.Included) != len(closed.Assets) {
		t.Fatalf("review concealed candidate assets: %+v", joint.Included)
	}
	seen := map[platform.AssetKind]bool{}
	for i, asset := range closed.Assets {
		if joint.Included[i] != asset.Ref {
			t.Fatalf("review changed candidate asset order: %+v", joint.Included)
		}
		if asset.Ref.Name == "advice" && asset.Ref.Kind == platform.AssetFunction {
			seen[platform.AssetFunction] = true
			if asset.SourceVersion != "1.function-2" {
				t.Fatalf("shared candidate changed the function pin: %+v", asset)
			}
		}
		if asset.Ref.Name == "intakeadvice" || asset.Ref.Name == "build.review" {
			seen[asset.Ref.Kind] = true
		}
	}
	if !seen[platform.AssetFunction] || !seen[platform.AssetPage] || !seen[platform.AssetFlow] {
		t.Fatalf("shared candidate omitted function, page or workflow: %+v", closed.Assets)
	}
	evaluate(jointID, "passed")
	keys++
	if active, err := tn.ActivateRelease(member("builder"), jointID, fmt.Sprint(keys), at); err != nil || active != jointID {
		t.Fatalf("activate shared function candidate: %s %v", active, err)
	}
	must("operator", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "JOINT-CALL", map[string]any{"name": "advice", "version": 2, "source": "OLD"})
	pending := pendingFunctionEffects(tn)
	if len(pending) != 1 {
		t.Fatalf("page function call after shared activation: %+v", pending)
	}
	var jointAsk modelAsk
	if err := json.Unmarshal([]byte(pending[0].Body), &jointAsk); err != nil || jointAsk.Function == nil ||
		jointAsk.Function.Call.Version != 2 || jointAsk.Function.Call.Release != jointID {
		t.Fatalf("page function lost shared release: %+v %v", jointAsk.Function, err)
	}
	matched, err := settleFunctionFixture(tn, member("operator"), build.FunctionFixture{Output: `{"summary":"Check source","category":"review","review":true}`, InputTokens: 4, OutputTokens: 8, ExpectState: "ready"}, at)
	if err != nil || !matched {
		t.Fatalf("shared page function answer: %v %v", matched, err)
	}
	tick()
	must("operator", build.ID, "build.intake.create", "build.intake", "JOINT-FLOW", map[string]string{"note": "Shared release"})
	tick()
	answer("JOINT-FLOW", "continue")
	pending = pendingFunctionEffects(tn)
	if len(pending) != 1 {
		t.Fatalf("workflow function call after shared activation: %+v", pending)
	}
	if err := json.Unmarshal([]byte(pending[0].Body), &jointAsk); err != nil || jointAsk.Function == nil ||
		jointAsk.Function.Call.Version != 2 || jointAsk.Function.Call.Release != jointID {
		t.Fatalf("workflow function lost shared release: %+v %v", jointAsk.Function, err)
	}
	matched, err = settleFunctionFixture(tn, member("operator"), build.FunctionFixture{Output: `{"summary":"Check source","category":"review","review":true}`, InputTokens: 4, OutputTokens: 8, ExpectState: "ready"}, at)
	if err != nil || !matched {
		t.Fatalf("shared workflow function answer: %v %v", matched, err)
	}
	tick()
	answer("JOINT-FLOW", "approve")
	CheckReplay(t, tn, entries, compose)
	// Pure candidate reading must reject a mismatched pinned dependency.
	saved, err := platform.ReadCandidate(first, tn.releases.candidates[first])
	if err != nil {
		t.Fatal(err)
	}
	for i := range saved.Assets {
		if saved.Assets[i].Ref.Kind == platform.AssetFunction {
			saved.Assets[i].SourceVersion = "1.function-2"
		}
	}
	if _, err := platform.Candidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetFlow, Name: "build.review"}}, saved.Assets); err == nil {
		t.Fatal("candidate accepted a different pinned function")
	}
	for i := range firstPage.Assets {
		if firstPage.Assets[i].Ref.Kind == platform.AssetFunction {
			firstPage.Assets[i].SourceVersion = "1.function-2"
		}
	}
	if _, err := platform.Candidate(pageRoot, firstPage.Assets); err == nil {
		t.Fatal("page candidate accepted a different pinned function")
	}
}
