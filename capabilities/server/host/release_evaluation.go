package host

import (
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"platformserver/platform"
	aigateway "platformserver/services/ai/gateway"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"reflect"
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

func (t *Tenant) EvaluateRelease(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	result, refusal := decideOnImage(t, context.Background(), "release:evaluate", func(t *Tenant) (readAnswer[string], *kernel.Error) {
		id, err := t.prepareEvaluateRelease(m, request, now)
		return readAnswer[string]{value: id, err: err}, nil
	}, func(t *Tenant, candidate preparedAccepted, result readAnswer[string], refusal *kernel.Error) (readAnswer[string], *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			_, refusal = t.finishPreparedSubmission(platform.Member{ID: "app:" + candidate.entry.App, Tenant: t.ID}, candidate, nil, refusal)
		}
		return result, refusal
	})
	if refusal != nil {
		return "", refusal
	}
	return result.value, result.err
}

func (t *Tenant) prepareEvaluateRelease(m platform.Member, request ReleaseEvaluationRequest, now time.Time) (string, error) {
	if err := t.Admits(m); err != nil {
		return "", err
	}
	if !build.HoldsIndependentRole(m, build.Builder) || request.CandidateID == "" || (request.PlanID == "") == (request.Agent == "") ||
		request.Key == "" || len(request.Key) > 200 || now.IsZero() {
		return "", fmt.Errorf("a builder, saved candidate, test plan or agent, and idempotency key are required")
	}

	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if request.Agent != "" {
		return t.evaluateReleaseAgentLocked(m, request, now)
	}
	caller := t.automation(build.ID, false)
	sub, id, err := releaseowner.PrepareEvaluation(t.ID, request.CandidateID, request.PlanID, request.Key, releaseowner.EvaluationHooks{
		Candidate:      func(id string) (platform.ReleaseCandidate, error) { return t.releases.Candidate(id) },
		Plan:           func(id string) (build.TestPlan, bool) { return platform.Get[build.TestPlan](caller, id) },
		Function:       func(id string) (build.Function, bool) { return platform.Get[build.Function](caller, id) },
		BuilderVersion: func() string { return t.App(build.ID).Manifest().Version },
		AppModel:       func() string { return t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel) },
		HasProvider:    func() bool { return t.ai != nil },
		ModelConfig:    t.evaluationModelConfig,
	})
	if err != nil {
		return "", err
	}
	app := t.App(build.ID).(platform.App)
	if _, refusal := t.accept(app, platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, now, acceptOptions{automation: true}); refusal != nil {
		return "", fmt.Errorf("function evaluation was refused: %s", refusal.Message)
	}
	return id, nil
}

func (t *Tenant) evaluationModelConfig(name string, definition platform.AIFunction) (string, error) {
	model, provider, refusal := t.ai.Model(name)
	if refusal != nil {
		return "", fmt.Errorf("the plan model is not enabled")
	}
	binding, _, refusal := t.modelResolver().Resolve("generation", model.Name(), definition.ModelOptions.Override(platform.ModelOptions{MaxTokens: definition.MaxTokens}))
	if refusal != nil {
		return "", fmt.Errorf("model options: %s", refusal.Message)
	}
	binding, refusal = t.modelResolver().Stage(binding, provider, platform.AIStageDesign)
	if refusal != nil {
		return "", fmt.Errorf("model stage: %s", refusal.Message)
	}
	return aigateway.ConfigurationDigest(model, provider, &binding)
}

func evalEqualRaw(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}
