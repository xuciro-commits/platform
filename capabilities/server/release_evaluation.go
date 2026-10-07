package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/platform"
)

// ReleaseEvaluationRequest asks for real, measured model calls against one
// saved function version and one retained synthetic test plan.
type ReleaseEvaluationRequest struct {
	CandidateID string `json:"candidateId"`
	PlanID      string `json:"planId"`
	Key         string `json:"key"`
}

type ReleaseEvaluationStarted struct {
	ID string `json:"id"`
}

func (t *Tenant) EvaluateRelease(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder) || request.CandidateID == "" || request.PlanID == "" ||
		request.Key == "" || len(request.Key) > 200 || now.IsZero() {
		return "", fmt.Errorf("a builder, saved candidate, test plan and idempotency key are required")
	}
	if t.AcceptResult == nil {
		return "", fmt.Errorf("function evaluation needs an accepted-result journal")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.enqueue(now)
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	candidate, err := platform.ReadCandidate(request.CandidateID, t.releaseCandidates[request.CandidateID])
	if err != nil {
		return "", fmt.Errorf("release candidate is not saved: %w", err)
	}
	caller := t.automation(build.ID, false)
	plan, ok := platform.Get[build.TestPlan](caller, request.PlanID)
	if !ok || plan.Archived || plan.Function == "" || len(plan.Evaluation) != 1 || plan.Model == "" {
		return "", fmt.Errorf("a saved function plan with one evaluation policy and model is required")
	}
	function, ok := platform.Get[build.Function](caller, string(plan.Function))
	if !ok || function.Archived {
		return "", fmt.Errorf("the plan's function draft is unavailable")
	}
	var asset platform.ReleaseAsset
	for _, selected := range candidate.Assets {
		if selected.Ref.App == build.ID && selected.Ref.Kind == platform.AssetFunction && selected.Ref.Name == function.Name {
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
		definition.Name != function.Name || definition.Check() != nil {
		return "", fmt.Errorf("the saved function version is invalid")
	}
	if definition.Model != "" && definition.Model != plan.Model {
		return "", fmt.Errorf("the plan model differs from the function's bound model")
	}
	if definition.Model == "" && plan.Model != t.setting(t.automation(ai.ID, false), ai.SettingAppModel) {
		return "", fmt.Errorf("the plan model differs from the current app model")
	}
	if t.ai == nil {
		return "", fmt.Errorf("the tenant has no model provider")
	}
	model, provider, refusal := t.ai.Model(plan.Model)
	if refusal != nil {
		return "", fmt.Errorf("the plan model is not enabled")
	}
	config, err := canonicalDigest([]any{model, provider})
	if err != nil {
		return "", err
	}
	for _, test := range plan.Evaluation[0].Cases {
		if !functionEvaluationInput(definition, test.Input) || definition.ValidateOutput(test.Expected) != nil {
			return "", fmt.Errorf("the plan's synthetic cases do not fit the saved function")
		}
	}

	definitionDigest, err := canonicalDigest(json.RawMessage(asset.Body))
	if err != nil {
		return "", err
	}
	planDigest, err := canonicalDigest(plan)
	if err != nil {
		return "", err
	}
	identity, err := canonicalDigest([]string{t.ID, request.CandidateID, request.PlanID, request.Key})
	if err != nil {
		return "", err
	}
	id := "eval-" + identity[:32]
	start := build.EvaluationStart{Candidate: request.CandidateID, Function: definition.Name, Version: version,
		Definition: definition, DefinitionDigest: definitionDigest, Plan: request.PlanID, PlanRevision: plan.Revision,
		PlanDigest: planDigest, Model: plan.Model, ModelConfig: config, Policy: plan.Evaluation[0]}
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: "app:" + build.ID, Authority: build.ID,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: build.EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: build.SchemaEvaluationStart, Version: 1}, Payload: platform.Raw(start)}
	app := t.app(build.ID).(platform.ResultApp)
	if _, refusal := t.submitAccepted(app, platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, now, true); refusal != nil {
		return "", fmt.Errorf("function evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

// Synthetic evaluation uses the same prompt shape as an ordinary accepted call.
// History contains bounded synthetic typed answers, never production call IDs.
func functionEvaluationInput(f platform.AIFunction, raw []byte) bool {
	if len(raw) > f.MaxInputBytes || !utf8.Valid(raw) {
		return false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || input == nil {
		return false
	}
	if f.Conversation {
		var record map[string]json.RawMessage
		var question string
		var history []map[string]json.RawMessage
		if len(input) != 3 || json.Unmarshal(input["record"], &record) != nil || record == nil || json.Unmarshal(input["question"], &question) != nil || strings.TrimSpace(question) == "" || len(question) > 4096 || json.Unmarshal(input["history"], &history) != nil || history == nil || len(history) > 8 {
			return false
		}
		for _, turn := range history {
			var prior string
			if len(turn) != 2 || json.Unmarshal(turn["question"], &prior) != nil || strings.TrimSpace(prior) == "" || len(prior) > 4096 || f.ValidateOutput(turn["answer"]) != nil {
				return false
			}
		}
		input = record
	}
	if len(input) != len(f.Fields) {
		return false
	}
	for _, name := range f.Fields {
		if value, present := input[name]; !present || !scalarEvaluationInput(value) {
			return false
		}
	}
	return true
}

func scalarEvaluationInput(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	switch value.(type) {
	case string, bool, float64:
		return true
	default:
		return false
	}
}

func evaluationFor(candidate string, records []build.Evaluation) []build.Evaluation {
	return slices.DeleteFunc(records, func(e build.Evaluation) bool { return e.Candidate != candidate || e.Archived })
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
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetFunction {
			functions = append(functions, asset)
		}
	}
	if len(functions) == 0 {
		return nil
	}
	if t.app(build.ID) == nil {
		return fmt.Errorf("the saved candidate requires the builder app")
	}
	caller := t.automation(build.ID, false)
	reports, _, refusal := platform.Find[build.Evaluation](caller, platform.Query{
		Domain: platform.Raw([]any{[]any{"candidate", "=", candidateID}, []any{"state", "=", "passed"}})})
	if refusal != nil {
		return fmt.Errorf("function evaluation reports are unavailable: %s", refusal.Message)
	}
	reports = evaluationFor(candidateID, reports)
	for _, asset := range functions {
		versionText, found := strings.CutPrefix(asset.SourceVersion, t.app(build.ID).Manifest().Version+".function-")
		version, parseErr := strconv.Atoi(versionText)
		var definition platform.AIFunction
		if !found || parseErr != nil || json.Unmarshal(asset.Body, &definition) != nil || definition.Check() != nil {
			return fmt.Errorf("the saved function %s is invalid", asset.Ref.Name)
		}
		digest, err := canonicalDigest(json.RawMessage(asset.Body))
		if err != nil {
			return err
		}
		matched := false
		for _, report := range reports {
			if report.Function != asset.Ref.Name || report.Version != version || report.DefinitionDigest != digest ||
				!evalEqualRaw([]byte(report.Definition), asset.Body) || report.State != "passed" || len(report.Policy) != 1 {
				continue
			}
			plan, ok := platform.Get[build.TestPlan](caller, report.Plan)
			if !ok || plan.Archived || plan.Revision != report.PlanRevision || len(plan.Evaluation) != 1 ||
				plan.Model != report.Model || !evalEqualRaw(platform.Raw(plan.Evaluation[0]), platform.Raw(report.Policy[0])) {
				continue
			}
			function, ok := platform.Get[build.Function](caller, string(plan.Function))
			if !ok || function.Archived || function.Name != asset.Ref.Name {
				continue
			}
			planDigest, err := canonicalDigest(plan)
			if err != nil || planDigest != report.PlanDigest {
				continue
			}
			modelName := definition.Model
			if modelName == "" {
				modelName = t.setting(t.automation(ai.ID, false), ai.SettingAppModel)
			}
			if t.ai == nil || report.Model != modelName {
				continue
			}
			model, provider, refusal := t.ai.Model(modelName)
			if refusal != nil {
				continue
			}
			config, err := canonicalDigest([]any{model, provider})
			if err != nil || config != report.ModelConfig || !report.CostComplete ||
				report.Quality < report.Policy[0].MinQuality || report.CostUSD > report.Policy[0].MaxCostUSD ||
				report.PeakLatency > report.Policy[0].MaxLatencyMillis ||
				len(report.Attempts) != len(report.Policy[0].Cases)*build.EvaluationRepeats {
				continue
			}
			matched = true
			break
		}
		if !matched {
			return fmt.Errorf("function %s needs a passing measured evaluation of this candidate, current plan and model configuration", asset.Ref.Name)
		}
	}
	return nil
}

func evalEqualRaw(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}
