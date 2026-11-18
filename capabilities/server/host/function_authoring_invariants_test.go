package host

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	agentevaluation "platformserver/services/ai/evaluation"
	"platformserver/services/devops/descriptor"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1FunctionAuthoringPublishesEvaluatesReviewsAndReplays(t *testing.T) {
	live := newInvariantTenant(t, "invariant-function-authoring")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})

	declaration := func(instructions string) map[string]any {
		return map[string]any{
			"name": "answer", "title": "Order answer", "description": "Return a typed order answer", "object": "build.alpha",
			"fields": []string{"note"}, "instructions": instructions,
			"output": []platform.Field{{Name: "answer", Type: "string", Description: "Answer"}}, "roles": []string{build.Builder},
			"maxInputBytes": 1024, "maxOutputBytes": 1024, "maxTokens": 100,
		}
	}
	live.accept(t, "build.function.create", protocol.FunctionType, "FUNCTION", declaration("Return the original answer."))
	live.accept(t, "build.function.publish", protocol.FunctionType, "FUNCTION", map[string]any{})
	live.accept(t, "build.agent.create", build.AgentType, "AGENT", map[string]any{
		"name": "helper", "title": "Builder helper", "description": "Builds a small business application",
		"instructions": "Inspect the goal, propose the next step and finish with the result.",
	})
	live.accept(t, "build.agent.publish", build.AgentType, "AGENT", map[string]any{})

	owner := live.App(build.ID).(*build.Build)
	ledger, err := owner.Authority().ForkRead()
	if err != nil {
		t.Fatal(err)
	}
	fork, err := owner.Fork(ledger)
	if err != nil {
		t.Fatal(err)
	}
	live.accept(t, "build.function.edit", protocol.FunctionType, "FUNCTION", map[string]any{"instructions": "Return the revised answer."})
	live.accept(t, "build.agent.edit", build.AgentType, "AGENT", map[string]any{"instructions": "Ask before the final step, then finish with the result."})

	preview, err := owner.DraftReleaseAssetsMulti([]build.JointDraftRef{
		{Kind: platform.AssetFunction, ID: "FUNCTION"}, {Kind: platform.AssetAgent, ID: "AGENT"},
	}, live.member(t), false)
	if err != nil {
		t.Fatal(err)
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFunction, Name: "answer"}
	agentRef := platform.AssetRef{App: build.ID, Kind: platform.AssetAgent, Name: "helper"}
	var candidate platform.ReleaseAsset
	var agentCandidate platform.ReleaseAsset
	for _, asset := range preview.After {
		if asset.Ref == ref {
			candidate = asset
		}
		if asset.Ref == agentRef {
			agentCandidate = asset
		}
	}
	var next platform.AIFunction
	if candidate.SourceVersion != "1.function-2" || json.Unmarshal(candidate.Body, &next) != nil || next.Instructions != "Return the revised answer." {
		t.Fatalf("function draft lost the edited version: %+v", candidate)
	}
	var nextAgent platform.AgentReleaseDescriptor
	if agentCandidate.SourceVersion != "1.agent-2" || json.Unmarshal(agentCandidate.Body, &nextAgent) != nil || nextAgent.Instructions != "Ask before the final step, then finish with the result." {
		t.Fatalf("agent draft lost the edited version: %+v", agentCandidate)
	}
	live.accept(t, "build.function.publish", protocol.FunctionType, "FUNCTION", map[string]any{})
	live.accept(t, "build.agent.publish", build.AgentType, "AGENT", map[string]any{})

	functionVersion := func(app platform.Service, version, expected int, instructions string) {
		t.Helper()
		definition, ordinal, ok := app.(platform.FunctionDefinitions).FunctionDefinition("answer", version)
		if !ok || ordinal != expected || definition.Instructions != instructions {
			t.Fatalf("function version %d: ordinal=%d found=%t definition=%+v", version, ordinal, ok, definition)
		}
	}
	functionVersion(owner, 1, 1, "Return the original answer.")
	functionVersion(owner, 0, 2, "Return the revised answer.")
	functionVersion(fork, 0, 1, "Return the original answer.")
	agentVersion := func(app platform.Service, expected int, instructions string) {
		t.Helper()
		agents := app.Manifest().Agents
		if len(agents) != 1 || agents[0].Name != "helper" || !strings.Contains(agents[0].Instructions, instructions) {
			t.Fatalf("agent version differs: %+v", agents)
		}
		dynamic := app.(protocol.DynamicAgentDefinitions)
		if actual := dynamic.AgentVersion("helper"); actual != expected {
			t.Fatalf("agent version: got %d, want %d", actual, expected)
		}
	}
	agentVersion(owner, 2, "Ask before the final step")
	agentVersion(fork, 1, "Inspect the goal")
	retained, err := owner.FunctionReleaseAsset("answer", "1.function-1")
	if err != nil {
		t.Fatalf("retained function asset: %v", err)
	}
	var retainedDefinition platform.AIFunction
	if retained.Ref != ref || retained.SourceVersion != "1.function-1" || json.Unmarshal(retained.Body, &retainedDefinition) != nil || retainedDefinition.Instructions != "Return the original answer." {
		t.Fatalf("retained function asset: %+v", retained)
	}

	policy := platform.EvaluationPolicy{MinQuality: 1, MaxCostUSD: 1, MaxLatencyMillis: 1000, Cases: []platform.EvaluationCase{{Name: "case", Input: json.RawMessage(`{"note":"ready"}`), Expected: json.RawMessage(`{"answer":"ok"}`)}}}
	live.accept(t, "build.testplan.create", build.TestPlanType, "PLAN", map[string]any{
		"variant": "evaluation-set", "name": "answerquality", "title": "Answer quality", "function": "FUNCTION", "functionVersion": 1, "at": "2000-01-01T00:00:00Z",
		"steps":      []platform.TestStep{{Type: protocol.FunctionCallType, ID: "evaluation", Action: protocol.SchemaFunctionCall, Payload: "{}", Expect: "accepted"}},
		"evaluation": []platform.EvaluationPolicy{policy},
	})
	live.accept(t, "build.testplan.publish", build.TestPlanType, "PLAN", map[string]any{})

	evaluationCaller := live.automation(build.ID, false)
	submitEvaluation := func(schema, id string, payload any) (*pb.ChangeRecord, *platform.Error) {
		sub := live.submission(schema, agentevaluation.EvaluationType, id, payload)
		sub.PrincipalId = evaluationCaller.ID
		return live.Tenant.accept(live.App(build.ID), evaluationCaller.Member, sub, invariantClock, acceptOptions{automation: true})
	}
	definition, ordinal, ok := owner.FunctionDefinition("answer", 1)
	if !ok || ordinal != 1 {
		t.Fatalf("function evaluation definition: ordinal=%d found=%t", ordinal, ok)
	}
	plan, ok := platform.Get[build.TestPlan](evaluationCaller, "PLAN")
	if !ok {
		t.Fatal("saved evaluation plan is unavailable")
	}
	definitionDigest, err := platform.CanonicalDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	planDigest, err := platform.CanonicalDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	start := agentevaluation.EvaluationStart{Candidate: "CANDIDATE", Function: "answer", Version: 1,
		Definition: definition, DefinitionDigest: definitionDigest, Plan: "PLAN", PlanRevision: plan.Revision,
		PlanDigest: planDigest, Model: "test-model", ModelConfig: "test-config", Policy: policy}
	changed := start
	changed.Definition.Instructions = "A definition that was not saved"
	if _, refusal := submitEvaluation(agentevaluation.SchemaEvaluationStart, "EVALUATION-CHANGED", changed); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("changed evaluation definition: %+v", refusal)
	}
	for _, evaluation := range []struct {
		id, answer, state string
	}{
		{id: "EVALUATION", answer: `{"answer":"ok"}`, state: "passed"},
		{id: "EVALUATION-FAILED", answer: `{"answer":"wrong"}`, state: "failed"},
	} {
		start.Candidate = evaluation.id
		if _, refusal := submitEvaluation(agentevaluation.SchemaEvaluationStart, evaluation.id, start); refusal != nil {
			t.Fatalf("start evaluation %s: %v", evaluation.id, refusal)
		}
		run, found := platform.Get[agentevaluation.Evaluation](evaluationCaller, evaluation.id)
		if !found || len(run.Attempts) != agentevaluation.EvaluationRepeats || run.State != "pending" {
			t.Fatalf("started evaluation %s differs: %+v", evaluation.id, run)
		}
		for _, attempt := range run.Attempts {
			answer := protocol.Answer{Call: attempt.Call, Outcome: "accepted", Text: evaluation.answer, Metered: true, TokensReported: true,
				InputTokens: 10, OutputTokens: 5, CostReported: true, CostUSD: 0.01, LatencyMillis: 10, ServedModel: "test-model"}
			if _, refusal := submitEvaluation(agentevaluation.SchemaEvaluationAnswer, evaluation.id, answer); refusal != nil {
				t.Fatalf("answer evaluation %s: %v", evaluation.id, refusal)
			}
		}
		run, found = platform.Get[agentevaluation.Evaluation](evaluationCaller, evaluation.id)
		if !found || run.State != evaluation.state || run.Quality != 1 && evaluation.state == "passed" || run.CostUSD != 0.03 || run.PeakLatency != 10 {
			t.Fatalf("finished evaluation %s differs: %+v", evaluation.id, run)
		}
	}
	duplicate := protocol.Answer{Call: "EVALUATION:1:1", Outcome: "accepted", Metered: true, CostReported: true, CostUSD: 0.01, LatencyMillis: 10}
	if _, refusal := submitEvaluation(agentevaluation.SchemaEvaluationAnswer, "EVALUATION", duplicate); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("completed evaluation accepted another answer: %+v", refusal)
	}

	var evaluationSet platform.ReleaseAsset
	if json.Unmarshal([]byte(plan.Published), &evaluationSet) != nil || evaluationSet.Ref.Kind != platform.AssetEvaluation {
		t.Fatalf("published evaluation set is invalid: %+v", evaluationSet)
	}
	releaseID, err := releaseowner.PrepareReleaseEvaluation(live.ID, live.member(t), releaseowner.ReleaseEvaluationRequest{
		CandidateID: "CANDIDATE", PlanID: "PLAN", Key: "release-evaluation",
	}, invariantClock, releaseowner.EvaluationHooks{
		Candidate: func(string) (protocol.ReleaseCandidate, error) {
			return protocol.ReleaseCandidate{ID: "CANDIDATE", Assets: []platform.ReleaseAsset{retained, evaluationSet}}, nil
		},
		Plan: func(string) (descriptor.FunctionEvaluationPlan, bool) {
			return descriptor.FunctionEvaluationPlan{Revision: plan.Revision, Name: plan.Name, Function: string(plan.Function),
				Model: plan.Model, Evaluation: plan.Evaluation, Digest: func(model string, evaluation []platform.EvaluationPolicy) (string, error) {
					plan.Model, plan.Evaluation = model, evaluation
					return platform.CanonicalDigest(plan)
				}}, true
		},
		Function:       func(string) (protocol.Function, bool) { return protocol.Function{}, false },
		BuilderVersion: func() string { return "1" },
		AppModel:       func() string { return "test-model" },
		HasProvider:    func() bool { return true },
		ModelConfig:    func(string, platform.AIFunction) (string, error) { return "test-config", nil },
		Admits:         func(platform.Member) *platform.Error { return nil },
		Quarantined:    func() bool { return false },
		FunctionStart: func(start descriptor.FunctionEvaluationStart) (*pb.Submission, string, error) {
			return agentevaluation.Prepare(live.ID, build.ID, "release-evaluation", start)
		},
		Accept: func(sub *pb.Submission, at time.Time) *platform.Error {
			_, refusal := live.Tenant.accept(live.App(build.ID), platform.Member{ID: sub.PrincipalId, Tenant: live.ID}, sub, at, acceptOptions{automation: true})
			return refusal
		},
	})
	if err != nil {
		t.Fatalf("release evaluation: %v", err)
	}
	releaseEvaluation, found := platform.Get[agentevaluation.Evaluation](evaluationCaller, releaseID)
	if !found || releaseEvaluation.Set == nil || releaseEvaluation.Candidate != "CANDIDATE" || releaseEvaluation.State != "pending" {
		t.Fatalf("release evaluation start differs: %+v", releaseEvaluation)
	}
	for _, attempt := range releaseEvaluation.Attempts {
		answer := protocol.Answer{Call: attempt.Call, Outcome: "accepted", Text: `{"answer":"ok"}`, Metered: true, TokensReported: true,
			InputTokens: 10, OutputTokens: 5, CostReported: true, CostUSD: 0.01, LatencyMillis: 10, ServedModel: "test-model"}
		if _, refusal := submitEvaluation(agentevaluation.SchemaEvaluationAnswer, releaseID, answer); refusal != nil {
			t.Fatalf("answer release evaluation: %v", refusal)
		}
	}
	releaseEvaluation, found = platform.Get[agentevaluation.Evaluation](evaluationCaller, releaseID)
	if !found || releaseEvaluation.State != "passed" {
		t.Fatalf("release evaluation did not pass: %+v", releaseEvaluation)
	}
	aiAssets := []platform.ReleaseAsset{retained, agentCandidate}
	visible := map[platform.AssetRef]bool{}
	for _, asset := range aiAssets {
		visible[asset.Ref] = true
	}
	ai, refusal := live.applicationAILocked(live.member(t), aiAssets, visible)
	if refusal != nil {
		t.Fatalf("application AI view: %v", refusal)
	}
	var applicationFunction, applicationAgent *protocol.ApplicationAI
	for i := range ai {
		switch ai[i].Resource.Ref {
		case ref:
			applicationFunction = &ai[i]
		case agentRef:
			applicationAgent = &ai[i]
		}
	}
	if applicationFunction == nil || len(applicationFunction.Evaluations) != 3 || applicationAgent == nil {
		t.Fatalf("application AI view differs: %+v", ai)
	}
	evaluations := map[string]bool{}
	for _, evaluation := range applicationFunction.Evaluations {
		evaluations[evaluation.ID] = true
	}
	if !evaluations["EVALUATION"] || !evaluations["EVALUATION-FAILED"] {
		t.Fatalf("application AI evaluations differ: %+v", applicationFunction.Evaluations)
	}
	bad := protocol.ReleaseCandidate{Assets: []platform.ReleaseAsset{{
		Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"},
		Body: platform.Raw(map[string]any{"name": "alpha", "actions": []ontology.Action{{
			Name: "adopt", Adoption: &platform.ProposalAdoption{Function: platform.FunctionRef{Name: "missing"}},
		}}}),
	}}}
	if err := releaseowner.CheckProposalFunctions(bad); err == nil {
		t.Fatal("a candidate accepted an adoption of an absent function")
	}
	live.accept(t, "build.testplan.edit", build.TestPlanType, "PLAN", map[string]any{"title": "Answer quality, revised"})
	live.accept(t, "build.testplan.publish", build.TestPlanType, "PLAN", map[string]any{})

	replayed := live.restore(t, live.entries)
	restored := replayed.App(build.ID).(*build.Build)
	for _, requested := range []struct {
		version, expected int
		instructions      string
	}{
		{version: 1, expected: 1, instructions: "Return the original answer."},
		{version: 0, expected: 2, instructions: "Return the revised answer."},
	} {
		definition, ordinal, ok := restored.FunctionDefinition("answer", requested.version)
		if !ok || ordinal != requested.expected {
			t.Fatalf("replayed function version %d: ordinal=%d found=%t", requested.version, ordinal, ok)
		}
		if definition.Instructions != requested.instructions {
			t.Fatalf("replayed function version %d changed: %+v", requested.version, definition)
		}
	}
	replayedEvaluation, found := platform.Get[agentevaluation.Evaluation](replayed.automation(build.ID, false), "EVALUATION")
	if !found || replayedEvaluation.State != "passed" || len(replayedEvaluation.Attempts) != agentevaluation.EvaluationRepeats {
		t.Fatalf("evaluation replay differs: %+v", replayedEvaluation)
	}
	replayedAI, refusal := replayed.applicationAILocked(replayed.member(t), aiAssets, visible)
	if refusal != nil {
		t.Fatalf("replayed application AI view: %v", refusal)
	}
	replayedFunction := slices.IndexFunc(replayedAI, func(item protocol.ApplicationAI) bool { return item.Resource.Ref == ref })
	if len(replayedAI) != 2 || replayedFunction < 0 || len(replayedAI[replayedFunction].Evaluations) != 3 {
		t.Fatalf("application AI replay differs: %+v", replayedAI)
	}
	replayedPlan, found := platform.Get[build.TestPlan](replayed.automation(build.ID, false), "PLAN")
	if !found || replayedPlan.DefinitionVersion != 2 || len(replayedPlan.Versions) != 2 || replayedPlan.Title != "Answer quality, revised" {
		t.Fatalf("evaluation set replay differs: %+v", replayedPlan)
	}

	member := replayed.member(t)
	store := replayed.Projection
	entity := store.Type(protocol.FunctionCallType)
	if entity == nil {
		t.Fatal("function call entity is unavailable")
	}
	run := protocol.FunctionRun{
		Record: platform.Record{ID: "PROPOSAL"},
		App:    build.ID, Function: "answer", Version: 2, Member: member.ID, State: "ready", Decision: "pending",
		ExpiresAt: invariantClock.Add(time.Hour), Output: `{"answer":"ok"}`,
		Contract: platform.AIFunction{Proposal: &platform.ProposalPolicy{ExpiresAfterSeconds: 3600}},
	}
	value := records.Copy(entity.Info().Go, run)
	records.Header(value).ID = run.ID
	store.Lock()
	entity.SetRow(run.ID, &records.Row{Value: value})
	store.Unlock()
	replayed.invalidateReadImage()
	seeded, refusal := replayed.RecordOf(t.Context(), member, protocol.FunctionCallType, run.ID, invariantClock)
	if refusal != nil {
		t.Fatalf("seed proposal: %v", refusal)
	}
	seededRun, ok := seeded.Record.(protocol.FunctionRun)
	if !ok || seededRun.Contract.Proposal == nil || seededRun.State != "ready" || seededRun.Decision != "pending" || seededRun.Member != member.ID {
		t.Fatalf("seeded proposal differs: %+v", seededRun)
	}
	replayed.accept(t, protocol.SchemaProposalReview, protocol.FunctionCallType, run.ID, map[string]any{"decision": "rejected"})
	view, refusal := replayed.RecordOf(t.Context(), member, protocol.FunctionCallType, run.ID, invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	reviewed, ok := view.Record.(protocol.FunctionRun)
	if !ok || reviewed.Decision != "rejected" || reviewed.DecidedBy != member.ID {
		t.Fatalf("proposal review was not retained: %+v", reviewed)
	}
}
