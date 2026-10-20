package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	resultformat "platformserver/internal/acceptance/result"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestApplicationProposalAdoption(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "proposals", []Seat{seatOf("builder", "build:builder", "platform:admin", "ai:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer"), seatOf("observer", "build:observer")}, build.New("proposals"), ai.New("proposals"), work.New("proposals"))
	}
	tn := compose()
	entries := []Entry{}
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected durable failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	allowed := true
	excluded := false
	fields := []construction.Field{{Name: "temperature", Title: "Temperature", Type: "integer", SourceType: "record", AIInput: &allowed}, {Name: "verdict", Title: "Verdict", Type: "choice", Choices: "pass,hold", SourceType: "generated"}, {Name: "number", Title: "Number", Type: "text", SourceType: "record"}, {Name: "secret", Title: "Secret", Type: "text", SourceType: "record", AIInput: &excluded}}
	txn := &platform.BusinessTxn{Subject: "build.inspection", Steps: []platform.TxnStep{{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "inspections", Pattern: "I-{n:4}"}}, {Name: "accept", Kind: "update", Target: platform.TxnRef{Type: "build.inspection", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "verdict", From: "verdict"}, {Field: "number", From: "step.number"}}}}}
	adoption := &platform.ProposalAdoption{Function: platform.FunctionRef{App: construction.ID, Name: "inspect"}, Input: "proposal", Mapping: []platform.ProposalInput{{Output: "verdict", Input: "verdict"}}}
	action := construction.Action{Name: "adopt", Title: "Adopt inspection", From: []string{"draft"}, Inputs: []construction.Input{{Name: "proposal", Title: "Proposal", Type: "reference", Ref: construction.FunctionCallType, Required: true}, {Name: "verdict", Title: "Verdict", Type: "choice", Choices: "pass,hold", Required: true}}, Transaction: txn, Adoption: adoption, Roles: []string{construction.User, "reviewer"}}
	checkpoint := action
	checkpoint.Name = "confirm"
	checkpoint.Title = "Confirm inspection"
	checkpoint.Approval = &construction.ActionApproval{Levels: []construction.ApproverLevel{{Title: "Supervisor", Role: "reviewer"}}}
	object := map[string]any{"name": "inspection", "title": "Inspection", "fields": fields, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "access": []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all", Edit: true}, {Role: "observer", Read: "all"}}, "actions": []construction.Action{action, checkpoint}}
	publishObject(t, tn, "builder", "OBJECT", object, at)
	function := map[string]any{"name": "inspect", "title": "Inspect", "description": "Suggest a classification", "object": "build.inspection", "fields": []string{"temperature"}, "instructions": "Classify only supplied facts", "output": []platform.Field{{Name: "verdict", Type: "string", Required: true, Description: "Proposed classification", Choices: []string{"pass", "hold"}}, {Name: "confidence", Type: "decimal", Required: true, Description: "Confidence"}}, "maxInputBytes": 4096, "maxOutputBytes": 1024, "maxTokens": 256, "roles": []string{construction.Builder, construction.User, "reviewer"}, "model": "local/probe", "proposal": platform.ProposalPolicy{HandoffRole: "reviewer", ConfidenceField: "confidence", MinimumConfidence: 0.7, ExpiresAfterSeconds: 3600}}
	decide(t, tn, "builder", construction.ID, "build.function.create", construction.FunctionType, "FUNCTION", function, at)
	decide(t, tn, "builder", construction.ID, build.SchemaFunction, construction.FunctionType, "FUNCTION", map[string]any{}, at)
	decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]any{"value": "local/probe"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "wire": "openai"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]any{"access": "users"}, at)
	resources := []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: "build.inspection"}, {App: construction.ID, Kind: platform.AssetFunction, Name: "inspect"}}
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "APP", map[string]any{"name": "quality", "title": "Quality", "resources": resources}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaHandOver, construction.AppType, "APP", map[string]any{}, at)
	decide(t, tn, "operator", construction.ID, "build.inspection.create", "build.inspection", "I1", map[string]any{"temperature": 24, "secret": "NEVER-SEND-THIS"}, at)
	call := func(id string, confidence float64) construction.FunctionRun {
		t.Helper()
		decide(t, tn, "operator", construction.ID, construction.SchemaFunctionCall, construction.FunctionCallType, id, map[string]any{"name": "inspect", "source": "I1"}, at)
		effects := pendingFunctionEffects(tn)
		if len(effects) != 1 {
			t.Fatalf("pending functions: %d", len(effects))
		}
		output := string(platform.Raw(map[string]any{"verdict": "pass", "confidence": confidence}))
		body := platform.Raw(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": output}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.02}})
		tn.AIClient = func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		answer, usage := tn.sendModel(effects[0], at.Add(time.Second))
		if answer.Result != "delivered" || usage == nil {
			t.Fatalf("provider outcome: %+v", answer)
		}
		usage.Millis = 0
		tn.settleWithUsage(effects[0].ID, answer, usage, at.Add(time.Second))
		if tn.quarantined() {
			t.Fatal(tn.fault.Load())
		}
		run, _ := platform.Get[construction.FunctionRun](tn.automation(construction.ID, false), id)
		if run.Decision != "pending" {
			t.Fatalf("proposal decision: %+v", run)
		}
		return run
	}
	business := func() []byte {
		record, _ := tn.RecordOf(memberOf(t, tn, "builder"), "build.inspection", "I1", at)
		return platform.Raw(record.Record)
	}
	before := business()
	if !strings.Contains(string(before), "NEVER-SEND-THIS") {
		t.Fatal("ordinary member source changed")
	}
	aiReader := memberOf(t, tn, "operator")
	observed, observeErr := (hostView{t: tn}).AgentContext(&aiReader, "build.inspection", "I1", at)
	if observeErr != nil || strings.Contains(string(observed.Body), "NEVER-SEND-THIS") {
		t.Fatal("AI-excluded context field leaked")
	}
	first := call("HIGH", 0.95)
	if !bytes.Equal(before, business()) || len(tn.sequences.clone()) != 0 {
		t.Fatal("proposal mutated a business record or allocated a number")
	}
	if first.SourceValues["temperature"] == nil || len(first.Applications) != 1 {
		t.Fatal("proposal lost accepted input or application attribution")
	}
	// The same original action enforces normal input types and permissions.
	if got := refuse(t, tn, "observer", construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "HIGH", "verdict": "hold"}, at.Add(2*time.Second)); got == "ok" {
		t.Fatal("observer adopted")
	}
	if got := refuse(t, tn, "operator", construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "HIGH", "verdict": "invalid"}, at.Add(2*time.Second)); got == "ok" {
		t.Fatal("invalid amendment adopted")
	}
	sub := submission(tn, memberOf(t, tn, "operator"), construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "HIGH", "verdict": "hold"})
	fail = true
	if _, err := tn.Submit(memberOf(t, tn, "operator"), sub, at.Add(2*time.Second)); err == nil {
		t.Fatal("unpersisted adoption accepted")
	}
	fail = false
	if !bytes.Equal(before, business()) || len(tn.sequences.clone()) != 0 {
		t.Fatal("durable failure leaked business writes")
	}
	receipt, err := tn.Submit(memberOf(t, tn, "operator"), sub, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	after := business()
	count := len(entries)
	if _, err := tn.Submit(memberOf(t, tn, "operator"), sub, at.Add(3*time.Second)); err != nil || len(entries) != count || !bytes.Equal(after, business()) {
		t.Fatal("adoption retry changed state")
	}
	adopted, _ := platform.Get[construction.FunctionRun](tn.automation(construction.ID, false), "HIGH")
	if adopted.Decision != "adopted" || adopted.DecidedBy != "operator" || adopted.AdoptedChange != receipt.GetChangeId() || !strings.Contains(string(adopted.Amendments), `"verdict":"hold"`) {
		t.Fatalf("adoption evidence: %+v", adopted)
	}
	if !strings.Contains(string(after), `"verdict":"hold"`) || !strings.Contains(string(after), "build.function-call/HIGH#output") {
		t.Fatal("adopted value or provenance missing")
	}
	if got := refuse(t, tn, "operator", construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "HIGH", "verdict": "pass"}, at.Add(3*time.Second)); got == "ok" {
		t.Fatal("proposal consumed twice")
	}
	// A low-confidence proposal creates an original work task. An approval
	// checkpoint freezes the full operation while business bytes stay intact.
	low := call("LOW", 0.2)
	if !slices.Contains(low.Reviewers, "reviewer") {
		t.Fatal("reviewer did not receive proposal")
	}
	if got := refuse(t, tn, "operator", construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "LOW", "verdict": "pass"}, at.Add(2*time.Second)); got == "ok" {
		t.Fatal("low confidence bypassed reviewer")
	}
	before = business()
	decide(t, tn, "operator", construction.ID, "build.inspection.confirm", "build.inspection", "I1", map[string]any{"proposal": "LOW", "verdict": "pass"}, at.Add(2*time.Second))
	if !bytes.Equal(before, business()) {
		t.Fatal("approval request mutated business state")
	}
	pending, _, _ := platform.Find[work.ApprovalRequest](tn.automation(work.ID, false), platform.Query{})
	if len(pending) != 1 {
		t.Fatalf("approval count: %d", len(pending))
	}
	decide(t, tn, "reviewer", work.ID, work.ApprovalType+".approve", work.ApprovalType, pending[0].ID, map[string]any{}, at.Add(3*time.Second))
	low, _ = platform.Get[construction.FunctionRun](tn.automation(construction.ID, false), "LOW")
	if low.Decision != "adopted" || low.DecidedBy != "operator" {
		t.Fatalf("approved adoption: %+v", low)
	}
	task, _ := platform.Get[platform.WorkTask](tn.automation(work.ID, false), construction.ID+":proposal:LOW")
	if task.State == "open" {
		t.Fatal("handover task stayed open")
	}
	// Review decisions are retained standard records, with no business write.
	call("REJECT", 0.2)
	before = business()
	decide(t, tn, "reviewer", construction.ID, build.SchemaProposalReview, construction.FunctionCallType, "REJECT", map[string]any{"decision": "rejected"}, at.Add(2*time.Second))
	if !bytes.Equal(before, business()) {
		t.Fatal("proposal rejection changed source")
	}
	rejected, _ := platform.Get[construction.FunctionRun](tn.automation(construction.ID, false), "REJECT")
	if rejected.Decision != "rejected" || rejected.DecidedBy != "reviewer" {
		t.Fatalf("proposal rejection lost its lifecycle: %+v", rejected)
	}
	rejection, _, decodeErr := resultformat.DecodeBatch(entries[len(entries)-1].Body)
	if decodeErr != nil || len(rejection.Business) != 1 || rejection.Business[0].Code != "AI_PROPOSAL_REJECTED" || rejection.Business[0].Record != construction.FunctionCallType+"/REJECT" {
		t.Fatalf("proposal rejection did not enter business refusal history: %+v %v", rejection.Business, decodeErr)
	}
	call("EXPIRE", 0.9)
	if got := refuse(t, tn, "operator", construction.ID, build.SchemaProposalReview, construction.FunctionCallType, "EXPIRE", map[string]any{"decision": "expired"}, at.Add(2*time.Second)); got == "ok" {
		t.Fatal("early expiry accepted")
	}
	decide(t, tn, "operator", construction.ID, build.SchemaProposalReview, construction.FunctionCallType, "EXPIRE", map[string]any{"decision": "expired"}, at.Add(2*time.Hour))
	call("STALE", 0.9)
	decide(t, tn, "operator", construction.ID, "build.inspection.edit", "build.inspection", "I1", map[string]any{"temperature": 25}, at.Add(2*time.Second))
	if got := refuse(t, tn, "operator", construction.ID, "build.inspection.adopt", "build.inspection", "I1", map[string]any{"proposal": "STALE", "verdict": "pass"}, at.Add(3*time.Second)); !strings.Contains(got, "source changed") {
		t.Fatalf("stale proposal: %s", got)
	}
	// The private builder scenario produces an ordinary proposal, but adoption
	// is explicitly refused without creating a work approval or business write.
	samples := []SimulationSample{{Type: "build.inspection", Records: []json.RawMessage{platform.Raw(map[string]any{"id": "I1", "temperature": 25, "state": "draft"})}}}
	trial := CandidateSimulationRequest{ApplicationID: "APP", As: "operator", At: at, Model: "local/probe", Samples: samples, Steps: []SimulationStep{{Type: construction.FunctionCallType, ID: "TRIAL", Action: construction.SchemaFunctionCall, Payload: platform.Raw(map[string]any{"name": "inspect", "source": "I1"}), Expect: "accepted", Function: &construction.FunctionFixture{Output: `{"verdict":"pass","confidence":0.9}`, ExpectState: "ready"}}, {Type: "build.inspection", ID: "I1", Action: "build.inspection.adopt", Payload: platform.Raw(map[string]any{"proposal": "TRIAL", "verdict": "pass"}), Expect: "refused"}}}
	out, problem := tn.SimulateCandidate(memberOf(t, tn, "builder"), trial)
	if problem != nil || !out.Recovered || out.Passed == nil || !*out.Passed {
		t.Fatalf("proposal trial: %+v %+v message=%s", out, problem, problem.Message)
	}
	usage, problem := tn.Read(memberOf(t, tn, "builder"), "ai-usage")
	if problem != nil {
		t.Fatal(problem)
	}
	report := usage.(platform.AIUsageReport)
	if len(report.Applications) == 0 || report.Applications[0].Calls < 5 || report.Applications[0].Cost <= 0 {
		t.Fatalf("application usage missing: %+v", report)
	}
	runtime, problem := tn.ApplicationRuntimeOf(memberOf(t, tn, "builder"), platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "quality"}, at.Add(time.Minute))
	if problem != nil || len(runtime.AI) != 1 || runtime.AI[0].Placement != "propose" || runtime.AI[0].Calls == 0 || len(runtime.AI[0].Checkpoints) == 0 {
		t.Fatalf("application AI projection: %+v %+v %s", runtime.AI, problem, problem.Message)
	}
	// Activation cannot omit the configured proposal function and its original
	// measured evaluation gate. Fixtures never satisfy that gate.
	candidate, releaseErr := tn.ReleaseCandidate([]platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: "build.inspection"}})
	if releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err := tn.evaluationsReadyLocked(candidate.ID, candidate.Bytes); err == nil {
		t.Fatal("adoption function omitted from activation gate")
	}
	candidate, releaseErr = tn.ReleaseCandidate([]platform.AssetRef{{App: construction.ID, Kind: platform.AssetApp, Name: "quality"}})
	if releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err := tn.evaluationsReadyLocked(candidate.ID, candidate.Bytes); err == nil {
		t.Fatal("unevaluated function activated")
	}
	// Recovery applies saved results; it neither calls a model nor re-adopts.
	raw, _, snapshotErr := tn.Snapshot(func() int64 { return 0 })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	saved, _ := platform.Get[construction.FunctionRun](restored.automation(construction.ID, false), "LOW")
	if !reflect.DeepEqual(saved, low) {
		t.Fatal("adoption did not recover")
	}
	// Source revocation masks both the retained output and its adopted field.
	blocked := false
	fields[0].AIInput = &blocked
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "OBJECT", map[string]any{"fields": fields}, at)
	if got := refuse(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "OBJECT", map[string]any{}, at); got == "ok" {
		t.Fatal("AI-excluded input published under an installed function")
	}
	if preview, err := tn.PreviewApplicationDrafts(memberOf(t, tn, "builder"), "APP"); err == nil && preview.Diagnostic == "" {
		t.Fatal("an explicitly excluded AI context still compiled")
	}
	fields[0].AIInput = &allowed
	indirect := action
	indirect.Transaction = &platform.BusinessTxn{Subject: txn.Subject, Steps: append(slices.Clone(txn.Steps), platform.TxnStep{Name: "copy", Kind: "update", Target: platform.TxnRef{Type: txn.Subject, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "number", From: "step.accept.verdict"}}})}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "OBJECT", map[string]any{"fields": fields, "actions": []construction.Action{indirect}}, at)
	if got := refuse(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "OBJECT", map[string]any{}, at); !strings.Contains(got, "generated provenance") {
		t.Fatalf("indirect AI destination escaped provenance: %s", got)
	}
	fields[0].Read = []string{construction.Builder}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "OBJECT", map[string]any{"fields": fields, "actions": []construction.Action{action, checkpoint}}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "OBJECT", map[string]any{}, at)
	masked, problem := tn.RecordOf(memberOf(t, tn, "operator"), construction.FunctionCallType, "LOW", at)
	if problem != nil || strings.Contains(string(platform.Raw(masked.Record)), `"verdict":"pass"`) {
		t.Fatal("revoked proposal output remained readable")
	}
	masked, problem = tn.RecordOf(memberOf(t, tn, "operator"), "build.inspection", "I1", at)
	if problem != nil || strings.Contains(string(platform.Raw(masked.Record)), `"verdict":"pass"`) {
		t.Fatal("adopted field lost source permissions")
	}
}

func TestApplicationAgentCandidateEvaluationGate(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "agent-candidate", []Seat{seatOf("builder", "build:builder", "platform:admin", "ai:admin"), seatOf("operator", "build:user")}, build.New("agent-candidate"), ai.New("agent-candidate"), work.New("agent-candidate"), NewAgents("agent-candidate"))
	}
	tn := compose()
	entries := []Entry{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	publishObject(t, tn, "builder", "O", map[string]any{"name": "case", "title": "Case", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/probe"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "wire": "openai"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]any{"access": "users"}, at)
	decide(t, tn, "builder", construction.ID, "build.agent.create", build.AgentType, "AG", map[string]any{"name": "assistant", "title": "Assistant", "description": "Review a case", "instructions": "Report Ready", "tools": []string{"build.case.edit"}, "cases": []build.AgentCase{{Name: "answer", Goal: "Give a result", Expect: "Ready"}}}, at)
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "APP", map[string]any{"name": "cases", "title": "Cases", "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetAgent, Name: "assistant"}}}, at)
	m := memberOf(t, tn, "builder")
	selection := []construction.JointDraftRef{{Kind: platform.AssetAgent, ID: "AG"}, {Kind: platform.AssetApp, ID: "APP"}}
	preview, problem := tn.PreviewReleaseDrafts(m, selection)
	if problem != nil || preview.Diagnostic != "" {
		t.Fatalf("agent draft preview: %+v %v", preview, problem)
	}
	saved, problem := tn.SaveReleaseCandidates(m, selection, preview.CandidateID, "save", at)
	if problem != nil {
		t.Fatal(problem)
	}
	if _, problem := tn.ActivateRelease(m, saved, "unmeasured", at); problem == nil {
		t.Fatal("unmeasured agent activated")
	}
	if _, problem := tn.EvaluateRelease(memberOf(t, tn, "operator"), ReleaseEvaluationRequest{CandidateID: saved, Agent: "build.assistant", Key: "denied"}, at); problem == nil {
		t.Fatal("operator started candidate evaluation")
	}
	calls, result := 0, "Wrong"
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(platform.Raw(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": result}}}, "usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "cost": 0.01}})))}, nil
	}
	request := ReleaseEvaluationRequest{CandidateID: saved, Agent: "build.assistant", Key: "failed"}
	failed, problem := tn.EvaluateRelease(m, request, at)
	if problem != nil {
		t.Fatal(problem)
	}
	tn.Evaluate(at)
	if _, problem := tn.ActivateRelease(m, saved, "failed-evaluation", at); problem == nil {
		t.Fatal("failed agent evaluation activated")
	}
	result, request.Key = "Ready", "passed"
	passed, problem := tn.EvaluateRelease(m, request, at)
	if problem != nil {
		t.Fatal(problem)
	}
	// Editing the working draft cannot change the saved suite/instructions.
	decide(t, tn, "builder", construction.ID, "build.agent.edit", build.AgentType, "AG", map[string]any{"instructions": "Different draft", "cases": []build.AgentCase{{Name: "different", Goal: "A new goal", Expect: "Wrong"}}}, at)
	tn.Evaluate(at)
	report, ok := platform.Get[Evaluation](tn.automation(AgentApp, false), passed)
	if !ok || report.Candidate != saved || report.Score != 1 || report.DefinitionDigest == "" || report.ModelConfig == "" || len(report.Cases) != 1 || report.Cases[0].Case != "answer" || calls != 6 {
		t.Fatalf("candidate evaluation: %+v (%d calls, prior %s)", report, calls, failed)
	}
	if retry, problem := tn.EvaluateRelease(m, request, at); problem != nil || retry != passed || calls != 6 {
		t.Fatal("evaluation retry changed its binding")
	}
	review, problem := tn.ReviewSavedRelease(m, saved)
	if problem != nil || len(review.AI) != 1 || len(review.AI[0].Evaluations) != 2 {
		t.Fatalf("candidate AI projection: %+v %v", review.AI, problem)
	}
	if _, problem := tn.ActivateRelease(m, saved, "measured", at); problem != nil {
		t.Fatal(problem)
	}
	decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/other", map[string]any{"access": "users"}, at)
	decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/other"}, at)
	candidate, _ := tn.releases.candidate(saved)
	if problem := tn.agentEvaluationsReadyLocked(candidate); problem == nil {
		t.Fatal("evaluation survived a changed model")
	}
	decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/probe"}, at)
	CheckReplay(t, tn, entries, compose)
	// A real agent turn retains the application's frozen resource binding.
	decide(t, tn, "operator", AgentApp, SchemaRunStart, RunType, "RUN", map[string]any{"agent": "build.assistant", "goal": "Give a result"}, at)
	turns := tn.agents.DueAgentTurns(at)
	if len(turns) != 1 {
		t.Fatalf("due turns: %d", len(turns))
	}
	tn.take(turns[0], at)
	usage := tn.ai.Usage()
	last := usage[len(usage)-1]
	if last.Run != "RUN" || len(last.Applications) != 1 || last.Resource == nil || last.Resource.Kind != platform.AssetAgent {
		t.Fatalf("agent usage binding: %+v", last)
	}
	decide(t, tn, "operator", construction.ID, "build.case.create", "build.case", "CASE", map[string]any{"note": "Original"}, at)
	decide(t, tn, "builder", construction.ID, "build.agent.edit", build.AgentType, "AG", map[string]any{"checkpoints": []string{"build.case.edit"}}, at)
	decide(t, tn, "builder", construction.ID, build.SchemaAgentPublish, build.AgentType, "AG", map[string]any{}, at)
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"tool_calls":[{"id":"edit","type":"function","function":{"name":"build_case_edit","arguments":"{\"target\":\"CASE\",\"note\":\"Changed\",\"rationale\":\"A proposed edit\"}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))}, nil
	}
	decide(t, tn, "operator", AgentApp, SchemaRunStart, RunType, "CHECKPOINT", map[string]any{"agent": "build.assistant", "goal": "Edit the case", "act": true}, at)
	turns = tn.agents.DueAgentTurns(at)
	if len(turns) != 1 {
		t.Fatalf("checkpoint due turns: %d", len(turns))
	}
	tn.take(turns[0], at)
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "CHECKPOINT")
	source, refusal := tn.RecordOf(m, "build.case", "CASE", at)
	if run.State != "waiting" || len(run.Draft) != 1 || refusal != nil || strings.Contains(string(platform.Raw(source.Record)), "Changed") {
		t.Fatalf("autonomous run crossed its human checkpoint: %+v %v", run, refusal)
	}
	CheckReplay(t, tn, entries, compose)
}
