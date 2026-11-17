package host

import (
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	agentevaluation "platformserver/services/ai/evaluation"
	aigateway "platformserver/services/ai/gateway"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) EvaluateRelease(m platform.Member, request releaseowner.ReleaseEvaluationRequest, now time.Time) (string, error) {
	result, refusal := decideOnImage(t, context.Background(), "release:evaluate", func(t *Tenant) (readAnswer[string], *platform.Error) {
		caller := t.automation(build.ID, false)
		id, err := releaseowner.PrepareReleaseEvaluation(t.ID, m, request, now, releaseowner.EvaluationHooks{
			Candidate:      func(id string) (platform.ReleaseCandidate, error) { return t.releases.Candidate(id) },
			Plan:           releaseowner.EvaluationPlan(caller),
			Function:       releaseowner.EvaluationFunction(caller),
			BuilderVersion: func() string { return t.App(build.ID).Manifest().Version },
			AppModel:       func() string { return t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel) },
			HasProvider:    func() bool { return t.ai != nil },
			ModelConfig: func(name string, definition platform.AIFunction) (string, error) {
				return aigateway.EvaluationModelConfig(name, definition, t.ai.Model, t.modelResolver())
			},
			Admits:        t.Admits,
			Quarantined:   t.quarantined,
			Agent:         t.evaluateReleaseAgentLocked,
			FunctionStart: agentevaluation.BindPrepare(t.ID, build.ID, request.Key),
			Accept: func(sub *pb.Submission, at time.Time) *platform.Error {
				_, refusal := t.accept(t.App(build.ID).(platform.App), platform.Member{ID: sub.PrincipalId, Tenant: t.ID}, sub, at, acceptOptions{automation: true})
				return refusal
			},
		})
		return readAnswer[string]{value: id, err: err}, nil
	}, func(t *Tenant, candidate preparedAccepted, result readAnswer[string], refusal *platform.Error) (readAnswer[string], *platform.Error) {
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
