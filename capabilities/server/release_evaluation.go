package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/aigateway"

	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/studio/build"
)

// ReleaseEvaluationRequest asks the original function or agent evaluator for
// real model calls against one saved candidate and its declared cases.
type ReleaseEvaluationRequest struct {
	CandidateID string `json:"candidateId"`
	PlanID      string `json:"planId,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Key         string `json:"key"`
}

type ReleaseEvaluationStarted struct {
	ID string `json:"id"`
}

func (t *Tenant) EvaluateRelease(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder) || request.CandidateID == "" || (request.PlanID == "") == (request.Agent == "") ||
		request.Key == "" || len(request.Key) > 200 || now.IsZero() {
		return "", fmt.Errorf("a builder, saved candidate, test plan or agent, and idempotency key are required")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.enqueue(now)
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if request.Agent != "" {
		return t.evaluateReleaseAgentLocked(m, request, now)
	}
	candidate, err := t.releases.Candidate(request.CandidateID)
	if err != nil {
		return "", fmt.Errorf("release candidate is not saved: %w", err)
	}
	caller := t.automation(build.ID, false)
	plan, ok := platform.Get[build.TestPlan](caller, request.PlanID)
	if ok && plan.Archived {
		return "", fmt.Errorf("a saved function plan is required")
	}
	if !ok {
		plan = build.TestPlan{Record: platform.Record{ID: request.PlanID, Revision: 1}, Name: request.PlanID}
	}
	var evaluationSet *platform.AssetBinding
	functionName := ""
	for _, selected := range candidate.Assets {
		if selected.Ref.Kind != platform.AssetEvaluation || selected.Ref.App != build.ID || selected.Ref.Name != plan.Name {
			continue
		}
		descriptor, err := platform.ReadEvaluationSet(selected.Body)
		if err != nil {
			return "", err
		}
		plan.Evaluation = []platform.EvaluationPolicy{descriptor.Policy}
		plan.Model = descriptor.Model
		functionName = descriptor.Function.Ref.Name
		evaluationSet = &platform.AssetBinding{Ref: selected.Ref, SourceVersion: selected.SourceVersion}
	}
	if evaluationSet == nil {
		if !ok {
			return "", fmt.Errorf("a saved function plan or candidate evaluation set is required")
		}
		function, ok := platform.Get[build.Function](caller, string(plan.Function))
		if !ok || function.Archived || len(plan.Evaluation) != 1 {
			return "", fmt.Errorf("a saved function plan with one evaluation policy is required")
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
		return "", fmt.Errorf("the saved candidate does not contain the plan's function")
	}
	versionText, found := strings.CutPrefix(asset.SourceVersion, t.app(build.ID).Manifest().Version+".function-")
	version, parseErr := strconv.Atoi(versionText)
	var definition platform.AIFunction
	if !found || parseErr != nil || version < 1 || json.Unmarshal(asset.Body, &definition) != nil ||
		definition.Name != functionName || definition.Check() != nil {
		return "", fmt.Errorf("the saved function version is invalid")
	}
	if plan.Model == "" {
		plan.Model = definition.Model
		if plan.Model == "" {
			plan.Model = t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel)
		}
	}
	if definition.Model != "" && definition.Model != plan.Model {
		return "", fmt.Errorf("the plan model differs from the function's bound model")
	}
	if definition.Model == "" && plan.Model != t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel) {
		return "", fmt.Errorf("the plan model differs from the current app model")
	}
	if t.ai == nil {
		return "", fmt.Errorf("the tenant has no model provider")
	}
	if err := definition.CheckEvaluationPolicy(plan.Evaluation[0]); err != nil {
		return "", err
	}
	model, provider, refusal := t.ai.Model(plan.Model)
	if refusal != nil {
		return "", fmt.Errorf("the plan model is not enabled")
	}
	binding, _, refusal := t.resolveModel("generation", model.Name(), definition.ModelOptions.Override(platform.ModelOptions{MaxTokens: definition.MaxTokens}))
	if refusal != nil {
		return "", fmt.Errorf("model options: %s", refusal.Message)
	}
	binding, refusal = t.modelStage(binding, provider, platform.AIStageDesign)
	if refusal != nil {
		return "", fmt.Errorf("model stage: %s", refusal.Message)
	}
	config, err := aigateway.ConfigurationDigest(model, provider, &binding)
	if err != nil {
		return "", err
	}
	for _, test := range plan.Evaluation[0].Cases {
		if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
			return "", fmt.Errorf("the plan's synthetic cases do not fit the saved function")
		}
	}

	definitionDigest, err := platform.CanonicalDigest(json.RawMessage(asset.Body))
	if err != nil {
		return "", err
	}
	planDigest, err := platform.CanonicalDigest(plan)
	if err != nil {
		return "", err
	}
	identity, err := platform.CanonicalDigest([]string{t.ID, request.CandidateID, request.PlanID, request.Key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	start := build.EvaluationStart{Set: evaluationSet, Candidate: request.CandidateID, Function: definition.Name, Version: version,
		Definition: definition, DefinitionDigest: definitionDigest, Plan: request.PlanID, PlanRevision: plan.Revision,
		PlanDigest: planDigest, Model: plan.Model, ModelConfig: config, Policy: plan.Evaluation[0]}
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: "app:" + build.ID, Authority: build.ID,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: build.EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: build.SchemaEvaluationStart, Version: 1}, Payload: platform.Raw(start)}
	app := t.app(build.ID).(platform.App)
	if _, refusal := t.submitAccepted(app, platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, now, true); refusal != nil {
		return "", fmt.Errorf("function evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

func evalEqualRaw(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func checkApplicationProposalFunctions(candidate platform.ReleaseCandidate) error {
	functions := map[string]platform.ReleaseAsset{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetFunction {
			functions[asset.Ref.Name] = asset
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object build.Object
		if json.Unmarshal(asset.Body, &object) != nil {
			return fmt.Errorf("Proposal object declaration is invalid")
		}
		for _, action := range object.Actions {
			if action.Adoption == nil {
				continue
			}
			functionAsset, ok := functions[action.Adoption.Function.Name]
			if !ok {
				return fmt.Errorf("Application adoption needs its declared function in the candidate")
			}
			definition, version, err := functionAssetDefinition(functionAsset)
			if err != nil {
				return err
			}
			if action.Adoption.Function.Version != 0 && action.Adoption.Function.Version != version {
				return fmt.Errorf("Application adoption function version differs from the candidate")
			}
			inputs := []platform.Field{}
			for _, input := range action.Inputs {
				inputs = append(inputs, input.Field())
			}
			if err := action.Adoption.CheckMapping(definition, inputs); err != nil {
				return err
			}
		}
	}
	return nil
}
