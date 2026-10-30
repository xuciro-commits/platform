package platformserver

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
	caller := platform.NewCaller(runtime{t}, m, evidence.Definition.Ref.App, false, false)
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
	permission := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: fields}, now, false)
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
		if raw := t.committed.Answer(app.Manifest().ID + "/" + key); len(raw) > 0 {
			result, saved, err := acceptedformat.DecodeBatch(raw)
			if err != nil {
				return nil, observationUnavailable(err), true
			}
			receipt = saved
			original, err = acceptedformat.BatchSubmission(result, saved)
			if err != nil {
				return nil, observationUnavailable(err), true
			}
		} else if saved := app.AcceptedLedger().AcceptedFor(t.ID, key); saved != nil {
			receipt, original = saved, saved.GetSubmission()
		}
		if original != nil {
			if original.GetPrincipalId() != m.ID || original.GetCorrelationId() != identity {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
			}
			return receipt, nil, true
		}
		if saved, found := t.committed.Refusal(app.Manifest().ID + "/" + key); found {
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
	t.mu.Lock()
	m = t.authorizationMember(m)
	if refusal := t.admits(m); refusal != nil {
		t.mu.Unlock()
		return answer, refusal
	}
	if !m.Active() || m.Tenant != t.ID {
		t.mu.Unlock()
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation member is unavailable")
	}
	if receipt, refusal, found := t.observationPrior(m, key, identity); found {
		t.mu.Unlock()
		answer.Matched, answer.Reused = true, true
		if receipt != nil {
			answer.Receipt = &SubmissionAnswer{Record: receipt}
		}
		return answer, refusal
	}
	plan, refusal := t.prepareObservation(m, ref, q.Record, now, true)
	if refusal != nil {
		t.mu.Unlock()
		return answer, refusal
	}
	read := ObservationRead{Record: q.Record, Since: q.Until.Add(-time.Duration(plan.info.Definition.WindowSeconds) * time.Second), Until: q.Until, Mode: "summary"}
	if plan.info.Definition.Decision.Mode == "intervals" {
		read.Mode = "intervals"
		read.Limit = platform.ObservationMaxPoints
	}
	if err := checkObservationRead(&read, now); err != nil {
		t.mu.Unlock()
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	decision := plan.info.Definition.Decision
	owner := t.app(decision.Action.App)
	if owner == nil {
		t.mu.Unlock()
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation decision action is unavailable")
	}
	action, declared := owner.Manifest().Actions.Action(decision.Action.Name)
	if err := platform.CheckObservationAction(plan.info.Definition, action); err != nil {
		t.mu.Unlock()
		return answer, observationUnavailable(err)
	}
	caller := platform.NewCaller(runtime{t}, m, decision.Action.App, false, false)
	if refusal := authorizationRefusal(t.authorizeCapability(caller, decision.Action.App, action.Schema, action.Roles, declared && action.Target == plan.entity.Type, authorizationDetails{At: now, Sources: plan.sources})); refusal != nil {
		t.mu.Unlock()
		return answer, refusal
	}
	t.mu.Unlock()
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
	t.mu.Lock()
	defer t.mu.Unlock()
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
	submission = t.currentSubmissionSchema(owner, submission)
	evidence := &platform.ObservationDecisionEvidence{Key: key, Definition: platform.AssetBinding{Ref: ref, SourceVersion: plan.info.Version}, Record: plan.entity.Type + "/" + q.Record, Device: plan.device, Signal: plan.info.Definition.Signal, Window: result.Window, Intervals: result.Intervals}
	receipt, refusal := t.submitAcceptedVersion(owner, m, submission, now, false, nil, nil, evidence, nil)
	if receipt != nil {
		answer.Receipt = &SubmissionAnswer{Record: receipt}
	}
	return answer, refusal
}
