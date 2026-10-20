package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Only the compact window and declared source identity accompany a business
// result. No raw point has a field in the accepted-result representation.
type ObservationDecisionEvidence struct {
	Key        string                     `json:"key"`
	Definition platform.AssetBinding      `json:"definition"`
	Record     string                     `json:"record"`
	Device     string                     `json:"device"`
	Signal     string                     `json:"signal"`
	Window     platform.ObservationWindow `json:"window"`
}

func (e ObservationDecisionEvidence) Check() error {
	if !strings.HasPrefix(e.Key, "observation:") || len(e.Key) > 200 || e.Definition.Ref.Kind != platform.AssetObservation || e.Definition.Ref.Check() != nil || e.Definition.SourceVersion == "" || e.Record == "" || e.Device == "" || e.Signal == "" {
		return fmt.Errorf("observation decision evidence needs its original source and record")
	}
	return e.Window.Check()
}

func (t *Tenant) visibleObservationEvidence(m platform.Member, evidence *ObservationDecisionEvidence, now time.Time) *ObservationDecisionEvidence {
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
	return &copy
}

func observationRequestIdentity(ref platform.AssetRef, q ObservationEvaluate) (string, string, error) {
	if q.Key == "" || len(q.Key) > 160 || strings.ContainsAny(q.Key, "/\x00\r\n") || q.Until.IsZero() {
		return "", "", fmt.Errorf("choose a stable observation decision key and window end")
	}
	hash, err := acceptance.Digest([]any{ref, q.Record, q.Until.UTC()})
	return "observation:" + q.Key, "observation:" + hash, err
}

// Original accepted receipts are the idempotency owner. A retry must not
// contact the external store or recompute against newer/late observations.
func (t *Tenant) observationPrior(m platform.Member, key, identity string) (*pb.ChangeRecord, *kernel.Error, bool) {
	for _, app := range t.apps {
		var original *pb.Submission
		var receipt *pb.ChangeRecord
		if raw := t.committed.answer(app.Manifest().ID + "/" + key); len(raw) > 0 {
			result, saved, err := decodeAcceptedBatch(raw)
			if err != nil {
				return nil, observationUnavailable(err), true
			}
			receipt = saved
			original, err = batchSubmission(result, saved)
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
		if saved, found := t.committed.refusal(app.Manifest().ID + "/" + key); found {
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
	if err := checkObservationRead(&read, now); err != nil {
		t.mu.Unlock()
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	// Refuse an unavailable action before contacting the store. The original
	// action owner repeats its full authorization when it accepts the result.
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
	result, err := t.fetchObservation(ctx, plan, read)
	if err != nil {
		return answer, observationUnavailable(err)
	}
	matched, inputs, err := platform.EvaluateObservation(plan.info.Definition, plan.entity, plan.record, result.Window, now)
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
	evidence := &ObservationDecisionEvidence{Key: key, Definition: platform.AssetBinding{Ref: ref, SourceVersion: plan.info.Version}, Record: plan.entity.Type + "/" + q.Record, Device: plan.device, Signal: plan.info.Definition.Signal, Window: result.Window}
	receipt, refusal := t.submitAcceptedVersion(owner, m, submission, now, false, nil, nil, evidence)
	if receipt != nil {
		answer.Receipt = &SubmissionAnswer{Record: receipt}
	}
	return answer, refusal
}
