package host

import (
	"context"
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"
	dataowner "platformserver/services/data"

	"time"

	"google.golang.org/protobuf/encoding/protojson"
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
	for _, app := range t.apps {
		var original *pb.Submission
		var receipt *pb.ChangeRecord
		prior := t.priorOf(app, key)
		if len(prior.Answer) > 0 {
			result, saved, err := acceptedformat.DecodeBatch(prior.Answer)
			if err != nil {
				return nil, dataowner.ObservationUnavailable(err), true
			}
			receipt = saved
			original, err = acceptedformat.BatchSubmission(result, saved)
			if err != nil {
				return nil, dataowner.ObservationUnavailable(err), true
			}
		} else if prior.Record != nil {
			receipt, original = prior.Record, prior.Record.GetSubmission()
		}
		if original != nil {
			if original.GetPrincipalId() != m.ID || original.GetCorrelationId() != identity {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
			}
			return receipt, nil, true
		}
		if saved := prior.Refusal; saved != nil {
			var original pb.Submission
			if len(saved.Submission) == 0 || protojson.Unmarshal(saved.Submission, &original) != nil || original.GetPrincipalId() != m.ID || original.GetCorrelationId() != identity {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
			}
			refusal := saved.Error
			return nil, &refusal, true
		}
	}
	return nil, nil, false
}

func (t *Tenant) observationDecision() dataowner.ObservationDecision {
	return dataowner.ObservationDecision{Tenant: t.ID, Sources: t.observationSources(), Admits: t.Admits, Prior: t.observationPrior,
		Action: func(app, name string) (platform.Action, bool, bool) {
			owner := t.App(app)
			if owner == nil {
				return platform.Action{}, false, false
			}
			action, declared := owner.Manifest().Actions.Action(name)
			return action, declared, true
		},
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
			return t.accept(owner, m, t.currentSubmissionSchema(owner, submission), at, acceptOptions{observation: evidence})
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
