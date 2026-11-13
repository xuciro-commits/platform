package host

import (
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	aigateway "platformserver/services/ai/gateway"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) EvaluateRelease(m platform.Member, request releaseowner.ReleaseEvaluationRequest, now time.Time) (string, error) {
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

func (t *Tenant) prepareEvaluateRelease(m platform.Member, request releaseowner.ReleaseEvaluationRequest, now time.Time) (string, error) {
	caller := t.automation(build.ID, false)
	return releaseowner.PrepareReleaseEvaluation(t.ID, m, request, now, releaseowner.EvaluationHooks{
		Candidate:      func(id string) (platform.ReleaseCandidate, error) { return t.releases.Candidate(id) },
		Plan:           func(id string) (build.TestPlan, bool) { return platform.Get[build.TestPlan](caller, id) },
		Function:       func(id string) (build.Function, bool) { return platform.Get[build.Function](caller, id) },
		BuilderVersion: func() string { return t.App(build.ID).Manifest().Version },
		AppModel:       func() string { return t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel) },
		HasProvider:    func() bool { return t.ai != nil },
		ModelConfig:    t.evaluationModelConfig,
		Admits:         t.Admits,
		Quarantined:    t.quarantined,
		Agent:          t.evaluateReleaseAgentLocked,
		Accept: func(sub *pb.Submission, at time.Time) *kernel.Error {
			_, refusal := t.accept(t.App(build.ID).(platform.App), platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, at, acceptOptions{automation: true})
			return refusal
		},
	})
}

func (t *Tenant) evaluationModelConfig(name string, definition platform.AIFunction) (string, error) {
	return aigateway.EvaluationModelConfig(name, definition, t.ai.Model, t.modelResolver())
}
