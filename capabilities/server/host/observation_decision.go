package host

import (
	"context"
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"

	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ObservationDecisionEvidence = platform.ObservationDecisionEvidence

func (t *Tenant) visibleObservationEvidence(m platform.Member, evidence *platform.ObservationDecisionEvidence, now time.Time) *platform.ObservationDecisionEvidence {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil
	}
	asset, err := registry.ResolvePinned(evidence.Definition.Ref, evidence.Definition.SourceVersion)
	if err != nil {
		return nil
	}
	var definition platform.ObservationDefinition
	if json.Unmarshal(asset.Body, &definition) != nil {
		return nil
	}
	caller := platform.NewCaller(runtime{t: t, scoped: true}, m, evidence.Definition.Ref.App, false, false)
	if !t.authorizeNative(caller, evidence.Definition.Ref.Name, "read", definition.Roles, true).Allow {
		return nil
	}
	typ, id, valid := strings.Cut(evidence.Record, "/")
	if !valid || typ != definition.Object.Name {
		return nil
	}
	fields := []string{definition.DeviceField}
	if definition.Decision != nil {
		fields = append(fields, observationDecisionFields(*definition.Decision)...)
	}
	permission := t.authorizeFrom(t.Projection, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: fields}, now, false)
	if !permission.Allow || len(permission.Withheld) > 0 {
		return nil
	}
	copy := *evidence
	if evidence.Intervals != nil {
		intervals := evidence.Intervals.Clone()
		copy.Intervals = &intervals
	}
	return &copy
}

func observationRequestIdentity(ref platform.AssetRef, q ObservationEvaluate) (string, string, error) {
	if q.Key == "" || len(q.Key) > 160 || strings.ContainsAny(q.Key, "/\x00\r\n") || q.Until.IsZero() {
		return "", "", fmt.Errorf("choose a stable observation decision key and window end")
	}
	hash, err := platform.CanonicalDigest([]any{ref, q.Record, q.Until.UTC()})
	return "observation:" + q.Key, "observation:" + hash, err
}

func (t *Tenant) observationPrior(m platform.Member, key, identity string) (*pb.ChangeRecord, *kernel.Error, bool) {
	for _, app := range t.apps {
		var original *pb.Submission
		var receipt *pb.ChangeRecord
		prior := t.priorOf(app, key)
		if len(prior.Answer) > 0 {
			result, saved, err := acceptedformat.DecodeBatch(prior.Answer)
			if err != nil {
				return nil, observationUnavailable(err), true
			}
			receipt = saved
			original, err = acceptedformat.BatchSubmission(result, saved)
			if err != nil {
				return nil, observationUnavailable(err), true
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

func (t *Tenant) EvaluateObservation(ctx context.Context, m platform.Member, ref platform.AssetRef, q ObservationEvaluate, now time.Time) (ObservationEvaluation, *kernel.Error) {
	var answer ObservationEvaluation
	key, identity, err := observationRequestIdentity(ref, q)
	if err != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	type prepared struct {
		answer ObservationEvaluation
		member platform.Member
		plan   observationReadPlan
		read   ObservationRead
	}
	initial, refusal := recordRead(t, ctx, func(t *Tenant) (prepared, *kernel.Error) {
		var answer ObservationEvaluation
		m = t.authorizationMember(m)
		if refusal := t.admits(m); refusal != nil {
			return prepared{answer: answer}, refusal
		}
		if !m.Active() || m.Tenant != t.ID {
			return prepared{answer: answer}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation member is unavailable")
		}
		if receipt, refusal, found := t.observationPrior(m, key, identity); found {
			answer.Matched, answer.Reused = true, true
			if receipt != nil {
				answer.Receipt = &SubmissionAnswer{Record: receipt}
			}
			return prepared{answer: answer}, refusal
		}
		plan, refusal := t.prepareObservation(m, ref, q.Record, now, true)
		if refusal != nil {
			return prepared{answer: answer}, refusal
		}
		read := ObservationRead{Record: q.Record, Since: q.Until.Add(-time.Duration(plan.info.Definition.WindowSeconds) * time.Second), Until: q.Until, Mode: "summary"}
		if plan.info.Definition.Decision.Mode == "intervals" {
			read.Mode = "intervals"
			read.Limit = platform.ObservationMaxPoints
		}
		if err := checkObservationRead(&read, now); err != nil {
			return prepared{answer: answer}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		decision := plan.info.Definition.Decision
		owner := t.app(decision.Action.App)
		if owner == nil {
			return prepared{answer: answer}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation decision action is unavailable")
		}
		action, declared := owner.Manifest().Actions.Action(decision.Action.Name)
		if err := platform.CheckObservationAction(plan.info.Definition, action); err != nil {
			return prepared{answer: answer}, observationUnavailable(err)
		}
		caller := platform.NewCaller(runtime{t: t, scoped: true}, m, decision.Action.App, false, false)
		if refusal := authorizationRefusal(t.authorizeCapability(caller, decision.Action.App, action.Schema, action.Roles, declared && action.Target == plan.entity.Type, authorizationDetails{At: now, Sources: plan.sources})); refusal != nil {
			return prepared{answer: answer}, refusal
		}
		return prepared{member: m, plan: plan, read: read}, nil
	})
	if refusal != nil || initial.answer.Reused {
		return initial.answer, refusal
	}
	m, plan, read := initial.member, initial.plan, initial.read
	decision := plan.info.Definition.Decision
	result, err := t.fetchObservation(ctx, plan, read, now)
	if err != nil {
		return answer, observationUnavailable(err)
	}
	var matched bool
	var inputs map[string]json.RawMessage
	if result.Intervals != nil {
		matched, inputs, err = platform.EvaluateObservationIntervals(plan.info.Definition, plan.entity, plan.record, *result.Intervals, now, t.services.Rules)
	} else {
		matched, inputs, err = platform.EvaluateObservation(plan.info.Definition, plan.entity, plan.record, result.Window, now, t.services.Rules)
	}
	if err != nil {
		return answer, observationUnavailable(err)
	}
	return decideOnImage(t, ctx, "observation:answer", func(t *Tenant) (ObservationEvaluation, *kernel.Error) {
		m = t.authorizationMember(m)
		var answer ObservationEvaluation
		current, refusal := t.prepareObservation(m, ref, q.Record, now, true)
		if refusal != nil {
			return answer, refusal
		}
		if current.stamp != plan.stamp {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation source or permissions changed while reading")
		}
		if receipt, refusal, found := t.observationPrior(m, key, identity); found {
			answer.Matched, answer.Reused = true, true
			if receipt != nil {
				answer.Receipt = &SubmissionAnswer{Record: receipt}
			}
			return answer, refusal
		}
		answer.Observation, answer.Matched = &result, matched
		if !matched {
			return answer, nil
		}
		payload, err := json.Marshal(inputs)
		if err != nil || len(payload) > 64<<10 {
			return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation decision inputs exceed their bounded scalar shape")
		}
		var source struct {
			Revision uint32 `json:"revision"`
		}
		if json.Unmarshal(plan.record, &source) != nil {
			return answer, observationUnavailable(fmt.Errorf("observation record revision is unavailable"))
		}
		submission := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: decision.Action.App, IdempotencyKey: key, CorrelationId: identity,
			Target: &pb.EntityRef{Type: plan.entity.Type, Id: q.Record}, Schema: &pb.SchemaRef{Name: decision.Action.Name, Version: 1}, ExpectedRevision: &source.Revision, Payload: payload}
		if refusal := t.delegatedBound(m, submission); refusal != nil {
			return answer, refusal
		}
		owner := t.app(decision.Action.App)
		if owner == nil {
			return answer, unknown()
		}
		submission = t.currentSubmissionSchema(owner, submission)
		evidence := &platform.ObservationDecisionEvidence{Key: key, Definition: platform.AssetBinding{Ref: ref, SourceVersion: plan.info.Version}, Record: plan.entity.Type + "/" + q.Record, Device: plan.device, Signal: plan.info.Definition.Signal, Window: result.Window, Intervals: result.Intervals}
		receipt, refusal := t.accept(owner, m, submission, now, acceptOptions{observation: evidence})
		if receipt != nil {
			answer.Receipt = &SubmissionAnswer{Record: receipt}
		}
		return answer, refusal
	}, func(t *Tenant, candidate preparedAccepted, value ObservationEvaluation, refusal *kernel.Error) (ObservationEvaluation, *kernel.Error) {
		if len(candidate.entry.Body) == 0 {
			return value, refusal
		}
		receipt, refusal := t.finishPreparedSubmission(m, candidate, nil, refusal)
		if receipt != nil {
			value.Receipt = &SubmissionAnswer{Record: receipt}
		}
		return value, refusal
	})
}
