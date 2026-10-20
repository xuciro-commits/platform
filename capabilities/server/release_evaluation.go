package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/platform"
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
	if !holdsIndependentBuildRole(m, construction.Builder) || request.CandidateID == "" || (request.PlanID == "") == (request.Agent == "") ||
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
	candidate, err := t.releases.candidate(request.CandidateID)
	if err != nil {
		return "", fmt.Errorf("release candidate is not saved: %w", err)
	}
	caller := t.automation(construction.ID, false)
	plan, ok := platform.Get[construction.TestPlan](caller, request.PlanID)
	if ok && plan.Archived {
		return "", fmt.Errorf("a saved function plan is required")
	}
	if !ok {
		plan = construction.TestPlan{Record: platform.Record{ID: request.PlanID, Revision: 1}, Name: request.PlanID}
	}
	var evaluationSet *platform.AssetBinding
	functionName := ""
	for _, selected := range candidate.Assets {
		if selected.Ref.Kind != platform.AssetEvaluation || selected.Ref.App != construction.ID || selected.Ref.Name != plan.Name {
			continue
		}
		descriptor, err := platform.ReadEvaluationSet(selected.Body)
		if err != nil {
			return "", err
		}
		plan.Evaluation = []construction.EvaluationPolicy{descriptor.Policy}
		plan.Model = descriptor.Model
		functionName = descriptor.Function.Ref.Name
		evaluationSet = &platform.AssetBinding{Ref: selected.Ref, SourceVersion: selected.SourceVersion}
	}
	if evaluationSet == nil {
		if !ok {
			return "", fmt.Errorf("a saved function plan or candidate evaluation set is required")
		}
		function, ok := platform.Get[construction.Function](caller, string(plan.Function))
		if !ok || function.Archived || len(plan.Evaluation) != 1 || plan.Model == "" {
			return "", fmt.Errorf("a saved function plan with one evaluation policy and model is required")
		}
		functionName = function.Name
	}
	var asset platform.ReleaseAsset
	for _, selected := range candidate.Assets {
		if selected.Ref.App == construction.ID && selected.Ref.Kind == platform.AssetFunction && selected.Ref.Name == functionName {
			asset = selected
			break
		}
	}
	if asset.Ref.Name == "" {
		return "", fmt.Errorf("the saved candidate does not contain the plan's function")
	}
	versionText, found := strings.CutPrefix(asset.SourceVersion, t.app(construction.ID).Manifest().Version+".function-")
	version, parseErr := strconv.Atoi(versionText)
	var definition platform.AIFunction
	if !found || parseErr != nil || version < 1 || json.Unmarshal(asset.Body, &definition) != nil ||
		definition.Name != functionName || definition.Check() != nil {
		return "", fmt.Errorf("the saved function version is invalid")
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
	model, provider, refusal := t.ai.Model(plan.Model)
	if refusal != nil {
		return "", fmt.Errorf("the plan model is not enabled")
	}
	config, err := acceptance.Digest([]any{model, provider})
	if err != nil {
		return "", err
	}
	for _, test := range plan.Evaluation[0].Cases {
		if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
			return "", fmt.Errorf("the plan's synthetic cases do not fit the saved function")
		}
	}

	definitionDigest, err := acceptance.Digest(json.RawMessage(asset.Body))
	if err != nil {
		return "", err
	}
	planDigest, err := acceptance.Digest(plan)
	if err != nil {
		return "", err
	}
	identity, err := acceptance.Digest([]string{t.ID, request.CandidateID, request.PlanID, request.Key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	start := construction.EvaluationStart{Set: evaluationSet, Candidate: request.CandidateID, Function: definition.Name, Version: version,
		Definition: definition, DefinitionDigest: definitionDigest, Plan: request.PlanID, PlanRevision: plan.Revision,
		PlanDigest: planDigest, Model: plan.Model, ModelConfig: config, Policy: plan.Evaluation[0]}
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: "app:" + construction.ID, Authority: construction.ID,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: construction.EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: construction.SchemaEvaluationStart, Version: 1}, Payload: platform.Raw(start)}
	app := t.app(construction.ID).(platform.App)
	if _, refusal := t.submitAccepted(app, platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, now, true); refusal != nil {
		return "", fmt.Errorf("function evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

func evaluationFor(candidate string, records []construction.Evaluation) []construction.Evaluation {
	return slices.DeleteFunc(records, func(e construction.Evaluation) bool { return e.Candidate != candidate || e.Archived })
}

// evaluationsReadyLocked is the activation gate for tenant-authored AI
// functions. A fixture simulation never creates one of these reports.
func (t *Tenant) evaluationsReadyLocked(candidateID string, raw []byte) error {
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	functions := []platform.ReleaseAsset{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == construction.ID && asset.Ref.Kind == platform.AssetFunction {
			functions = append(functions, asset)
		}
	}
	if err := checkApplicationProposalFunctions(candidate); err != nil {
		return err
	}
	if err := t.agentEvaluationsReadyLocked(candidate); err != nil {
		return err
	}
	if len(functions) == 0 {
		return nil
	}
	if t.app(construction.ID) == nil {
		return fmt.Errorf("the saved candidate requires the builder app")
	}
	caller := t.automation(construction.ID, false)
	reports, _, refusal := platform.Find[construction.Evaluation](caller, platform.Query{
		Domain: platform.Raw([]any{[]any{"candidate", "=", candidateID}, []any{"state", "=", "passed"}})})
	if refusal != nil {
		return fmt.Errorf("function evaluation reports are unavailable: %s", refusal.Message)
	}
	reports = evaluationFor(candidateID, reports)
	for _, asset := range functions {
		versionText, found := strings.CutPrefix(asset.SourceVersion, t.app(construction.ID).Manifest().Version+".function-")
		version, parseErr := strconv.Atoi(versionText)
		var definition platform.AIFunction
		if !found || parseErr != nil || json.Unmarshal(asset.Body, &definition) != nil || definition.Check() != nil {
			return fmt.Errorf("the saved function %s is invalid", asset.Ref.Name)
		}
		digest, err := acceptance.Digest(json.RawMessage(asset.Body))
		if err != nil {
			return err
		}
		requiredSets := []platform.AssetBinding{}
		for _, set := range candidate.Assets {
			if set.Ref.Kind == platform.AssetEvaluation {
				descriptor, err := platform.ReadEvaluationSet(set.Body)
				if err != nil {
					return err
				}
				if descriptor.Function.Ref == asset.Ref {
					requiredSets = append(requiredSets, platform.AssetBinding{Ref: set.Ref, SourceVersion: set.SourceVersion})
				}
			}
		}
		matched := false
		passedSets := map[platform.AssetBinding]bool{}
		for _, report := range reports {
			if report.Function != asset.Ref.Name || report.Version != version || report.DefinitionDigest != digest ||
				!evalEqualRaw([]byte(report.Definition), asset.Body) || report.State != "passed" || len(report.Policy) != 1 {
				continue
			}
			if len(requiredSets) > 0 && (report.Set == nil || !slices.Contains(requiredSets, *report.Set)) {
				continue
			}
			if report.Set != nil {
				selected := slices.IndexFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
					return a.Ref == report.Set.Ref && a.SourceVersion == report.Set.SourceVersion
				})
				if selected < 0 {
					continue
				}
				descriptor, err := platform.ReadEvaluationSet(candidate.Assets[selected].Body)
				if err != nil || descriptor.Function.Ref != asset.Ref || descriptor.Function.SourceVersion != asset.SourceVersion || descriptor.Model != report.Model || !evalEqualRaw(platform.Raw(descriptor.Policy), platform.Raw(report.Policy[0])) {
					continue
				}
			} else {
				plan, ok := platform.Get[construction.TestPlan](caller, report.Plan)
				if !ok || plan.Archived {
					continue
				}
				if plan.Revision != report.PlanRevision || len(plan.Evaluation) != 1 || plan.Model != report.Model || !evalEqualRaw(platform.Raw(plan.Evaluation[0]), platform.Raw(report.Policy[0])) {
					continue
				}
				function, ok := platform.Get[construction.Function](caller, string(plan.Function))
				if !ok || function.Archived || function.Name != asset.Ref.Name {
					continue
				}
				planDigest, err := acceptance.Digest(plan)
				if err != nil || planDigest != report.PlanDigest {
					continue
				}
			}
			modelName := definition.Model
			if modelName == "" {
				modelName = t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel)
			}
			if t.ai == nil || report.Model != modelName {
				continue
			}
			model, provider, refusal := t.ai.Model(modelName)
			if refusal != nil {
				continue
			}
			config, err := acceptance.Digest([]any{model, provider})
			if err != nil || config != report.ModelConfig || !report.CostComplete ||
				report.Quality < report.Policy[0].MinQuality || report.CostUSD > report.Policy[0].MaxCostUSD ||
				report.PeakLatency > report.Policy[0].MaxLatencyMillis ||
				len(report.Attempts) != len(report.Policy[0].Cases)*construction.EvaluationRepeats {
				continue
			}
			matched = true
			if report.Set != nil {
				passedSets[*report.Set] = true
			}
		}
		if !matched || slices.ContainsFunc(requiredSets, func(set platform.AssetBinding) bool { return !passedSets[set] }) {
			return fmt.Errorf("function %s needs a passing measured evaluation of this candidate, current plan and model configuration", asset.Ref.Name)
		}
	}
	return nil
}

func evalEqualRaw(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func checkApplicationProposalFunctions(candidate platform.ReleaseCandidate) error {
	functions := map[string]platform.ReleaseAsset{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == construction.ID && asset.Ref.Kind == platform.AssetFunction {
			functions[asset.Ref.Name] = asset
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object construction.Object
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
