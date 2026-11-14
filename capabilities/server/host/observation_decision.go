package host

import (
	"context"
	"encoding/json"
	dataowner "platformserver/services/data"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ObservationDecisionEvidence = platform.ObservationDecisionEvidence

func (t *Tenant) visibleObservationEvidence(m platform.Member, evidence *platform.ObservationDecisionEvidence, now time.Time) *platform.ObservationDecisionEvidence {
	caller := platform.NewCaller(runtime{t: t, scoped: true}, m, evidence.Definition.Ref.App, false, false)
	return dataowner.VisibleObservationEvidence(evidence,
		func() (definition platform.ObservationDefinition, found bool) {
			registry, err := t.assetRegistryLocked()
			if err != nil {
				return definition, false
			}
			asset, err := registry.ResolvePinned(evidence.Definition.Ref, evidence.Definition.SourceVersion)
			return definition, err == nil && json.Unmarshal(asset.Body, &definition) == nil
		},
		func(name string, roles []string) bool {
			return t.authorizeNative(caller, name, "read", roles, true).Allow
		},
		func(typ, id string, fields []string) bool {
			permission := t.authorizeFrom(t.Projection, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: fields}, now, false)
			return permission.Allow && len(permission.Withheld) == 0
		})
}

func (t *Tenant) observationPrior(m platform.Member, key, identity string) (*pb.ChangeRecord, *kernel.Error, bool) {
	return t.Committed.ReusedPrior(t.apps, t.ID, key, m.ID, identity)
}

func (t *Tenant) observationDecision() dataowner.ObservationDecision {
	return dataowner.ObservationDecision{Tenant: t.ID, Sources: t.observationSources(), Admits: t.Admits, Prior: t.observationPrior,
		App: t.App,
		Authorize: func(m platform.Member, app string, action platform.Action, targets bool, at time.Time, sources []string) *kernel.Error {
			caller := platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
			return authorizationRefusal(t.authorizeCapability(caller, app, action.Schema, action.Roles, targets, authorizationDetails{At: at, Sources: sources}))
		},
		Accept: func(m platform.Member, submission *pb.Submission, evidence *platform.ObservationDecisionEvidence, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
			if refusal := t.delegatedBound(m, submission); refusal != nil {
				return nil, refusal
			}
			owner := t.App(submission.GetAuthority())
			if owner == nil {
				return nil, unknown()
			}
			return t.accept(owner, m, t.CurrentSchema(owner, submission), at, acceptOptions{observation: evidence})
		}}
}

func observationEvaluation(a dataowner.ObservationAnswer) ObservationEvaluation {
	answer := ObservationEvaluation{Observation: a.Observation, Matched: a.Matched, Reused: a.Reused}
	if a.Receipt != nil {
		answer.Receipt = &SubmissionAnswer{Record: a.Receipt}
	}
	return answer
}

func (t *Tenant) EvaluateObservation(ctx context.Context, m platform.Member, ref platform.AssetRef, q dataowner.ObservationEvaluate, now time.Time) (ObservationEvaluation, *kernel.Error) {
	key, identity, err := dataowner.ObservationRequestIdentity(ref, q)
	if err != nil {
		return ObservationEvaluation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	initial, refusal := recordRead(t, ctx, func(t *Tenant) (dataowner.ObservationPrepared, *kernel.Error) {
		m = t.authorizationMember(m)
		return t.observationDecision().Prepare(m, ref, q, key, identity, now)
	})
	if refusal != nil || initial.Answer.Reused {
		return observationEvaluation(initial.Answer), refusal
	}
	m = initial.Member
	outcome, refusal := t.observationDecision().Observe(ctx, initial, now)
	if refusal != nil {
		return ObservationEvaluation{}, refusal
	}
	answer, refusal := decideOnImage(t, ctx, "observation:answer", func(t *Tenant) (dataowner.ObservationAnswer, *kernel.Error) {
		m = t.authorizationMember(m)
		return t.observationDecision().Commit(m, ref, q, key, identity, initial, outcome, now)
	}, func(t *Tenant, candidate preparedAccepted, value dataowner.ObservationAnswer, refusal *kernel.Error) (dataowner.ObservationAnswer, *kernel.Error) {
		if len(candidate.entry.Body) == 0 {
			return value, refusal
		}
		receipt, refusal := t.finishPreparedSubmission(m, candidate, nil, refusal)
		if receipt != nil {
			value.Receipt = receipt
		}
		return value, refusal
	})
	return observationEvaluation(answer), refusal
}
