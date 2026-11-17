package releases

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/devops/descriptor"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
	"strconv"
	"strings"
	"time"
)

type ReleaseEvaluationRequest struct {
	CandidateID string `json:"candidateId"`
	PlanID      string `json:"planId,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Key         string `json:"key"`
}

type ReleaseEvaluationStarted struct {
	ID string `json:"id"`
}

type EvaluationHooks struct {
	Candidate      func(id string) (platform.ReleaseCandidate, error)
	Plan           func(id string) (descriptor.FunctionEvaluationPlan, bool)
	Function       func(id string) (functionauthoring.Function, bool)
	BuilderVersion func() string
	AppModel       func() string
	HasProvider    func() bool
	ModelConfig    func(model string, definition platform.AIFunction) (string, error)
	Admits         func(platform.Member) *platform.Error
	Quarantined    func() bool
	Agent          func(platform.Member, ReleaseEvaluationRequest, time.Time) (string, error)
	FunctionStart  func(descriptor.FunctionEvaluationStart) (*pb.Submission, string, error)
	Accept         func(*pb.Submission, time.Time) *platform.Error
}

func EvaluationPlan(c platform.Caller) func(string) (descriptor.FunctionEvaluationPlan, bool) {
	return func(id string) (descriptor.FunctionEvaluationPlan, bool) {
		plan, ok := platform.Get[build.TestPlan](c, id)
		if !ok {
			plan = build.TestPlan{Record: platform.Record{ID: id, Revision: 1}, Name: id}
		}
		return descriptor.FunctionEvaluationPlan{Revision: plan.Revision, Archived: plan.Archived, Name: plan.Name,
			Function: string(plan.Function), Model: plan.Model, Evaluation: plan.Evaluation,
			Digest: func(model string, evaluation []platform.EvaluationPolicy) (string, error) {
				plan.Model, plan.Evaluation = model, evaluation
				return platform.CanonicalDigest(plan)
			}}, ok
	}
}

func EvaluationFunction(c platform.Caller) func(string) (functionauthoring.Function, bool) {
	return func(id string) (functionauthoring.Function, bool) {
		return platform.Get[functionauthoring.Function](c, id)
	}
}

func PrepareReleaseEvaluation(tenant string, member platform.Member, request ReleaseEvaluationRequest, now time.Time, hooks EvaluationHooks) (string, error) {
	if err := hooks.Admits(member); err != nil {
		return "", err
	}
	if !build.HoldsIndependentRole(member, build.Builder) || request.CandidateID == "" || (request.PlanID == "") == (request.Agent == "") ||
		request.Key == "" || len(request.Key) > 200 || now.IsZero() {
		return "", fmt.Errorf("a builder, saved candidate, test plan or agent, and idempotency key are required")
	}
	if hooks.Quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if request.Agent != "" {
		return hooks.Agent(member, request, now)
	}
	start, err := PrepareEvaluation(request.CandidateID, request.PlanID, hooks)
	if err != nil {
		return "", err
	}
	sub, id, err := hooks.FunctionStart(start)
	if err != nil {
		return "", err
	}
	if refusal := hooks.Accept(sub, now); refusal != nil {
		return "", fmt.Errorf("function evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

func PrepareEvaluation(candidateID, planID string, hooks EvaluationHooks) (descriptor.FunctionEvaluationStart, error) {
	candidate, err := hooks.Candidate(candidateID)
	if err != nil {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("release candidate is not saved: %w", err)
	}
	plan, ok := hooks.Plan(planID)
	if ok && plan.Archived {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("a saved function plan is required")
	}
	if !ok {
		plan.Revision, plan.Name = 1, planID
	}
	var evaluationSet *platform.AssetBinding
	functionName := ""
	for _, selected := range candidate.Assets {
		if selected.Ref.Kind != platform.AssetEvaluation || selected.Ref.App != build.ID || selected.Ref.Name != plan.Name {
			continue
		}
		set, err := platform.ReadEvaluationSet(selected.Body)
		if err != nil {
			return descriptor.FunctionEvaluationStart{}, err
		}
		plan.Evaluation = []platform.EvaluationPolicy{set.Policy}
		plan.Model = set.Model
		functionName = set.Function.Ref.Name
		evaluationSet = &platform.AssetBinding{Ref: selected.Ref, SourceVersion: selected.SourceVersion}
	}
	if evaluationSet == nil {
		if !ok {
			return descriptor.FunctionEvaluationStart{}, fmt.Errorf("a saved function plan or candidate evaluation set is required")
		}
		function, ok := hooks.Function(string(plan.Function))
		if !ok || function.Archived || len(plan.Evaluation) != 1 {
			return descriptor.FunctionEvaluationStart{}, fmt.Errorf("a saved function plan with one evaluation policy is required")
		}
		functionName = function.Name
	}
	var asset platform.ReleaseAsset
	for _, selected := range candidate.Assets {
		if selected.Ref.App == build.ID && selected.Ref.Kind == platform.AssetFunction && selected.Ref.Name == functionName {
			asset = selected
			break
		}
	}
	if asset.Ref.Name == "" {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the saved candidate does not contain the plan's function")
	}
	versionText, found := strings.CutPrefix(asset.SourceVersion, hooks.BuilderVersion()+".function-")
	version, parseErr := strconv.Atoi(versionText)
	var definition platform.AIFunction
	if !found || parseErr != nil || version < 1 || json.Unmarshal(asset.Body, &definition) != nil ||
		definition.Name != functionName || definition.Check() != nil {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the saved function version is invalid")
	}
	if plan.Model == "" {
		plan.Model = definition.Model
		if plan.Model == "" {
			plan.Model = hooks.AppModel()
		}
	}
	if definition.Model != "" && definition.Model != plan.Model {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the plan model differs from the function's bound model")
	}
	if definition.Model == "" && plan.Model != hooks.AppModel() {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the plan model differs from the current app model")
	}
	if !hooks.HasProvider() {
		return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the tenant has no model provider")
	}
	if err := definition.CheckEvaluationPolicy(plan.Evaluation[0]); err != nil {
		return descriptor.FunctionEvaluationStart{}, err
	}
	config, err := hooks.ModelConfig(plan.Model, definition)
	if err != nil {
		return descriptor.FunctionEvaluationStart{}, err
	}
	for _, test := range plan.Evaluation[0].Cases {
		if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
			return descriptor.FunctionEvaluationStart{}, fmt.Errorf("the plan's synthetic cases do not fit the saved function")
		}
	}
	definitionDigest, err := platform.CanonicalDigest(json.RawMessage(asset.Body))
	if err != nil {
		return descriptor.FunctionEvaluationStart{}, err
	}
	planDigest, err := plan.Digest(plan.Model, plan.Evaluation)
	if err != nil {
		return descriptor.FunctionEvaluationStart{}, err
	}
	return descriptor.FunctionEvaluationStart{Set: evaluationSet, Candidate: candidateID, Function: definition.Name, Version: version,
		Definition: definition, DefinitionDigest: definitionDigest, Plan: planID, PlanRevision: plan.Revision,
		PlanDigest: planDigest, Model: plan.Model, ModelConfig: config, Policy: plan.Evaluation[0]}, nil
}
