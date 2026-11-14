package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ObservationAnswer struct {
	Observation *platform.ObservationResult
	Matched     bool
	Receipt     *pb.ChangeRecord
	Reused      bool
}

type ObservationPrepared struct {
	Answer ObservationAnswer
	Member platform.Member
	Plan   ObservationPlan
	Read   ObservationRead
}

type ObservationOutcome struct {
	Result  platform.ObservationResult
	Matched bool
	Inputs  map[string]json.RawMessage
}

type ObservationDecision struct {
	Tenant    string
	Sources   Observations
	Admits    func(platform.Member) *kernel.Error
	Prior     func(m platform.Member, key, identity string) (*pb.ChangeRecord, *kernel.Error, bool)
	App       func(app string) platform.App
	Authorize func(m platform.Member, app string, action platform.Action, targets bool, at time.Time, sources []string) *kernel.Error
	Accept    func(m platform.Member, s *pb.Submission, evidence *platform.ObservationDecisionEvidence, at time.Time) (*pb.ChangeRecord, *kernel.Error)
}

func ObservationRequestIdentity(ref platform.AssetRef, q ObservationEvaluate) (string, string, error) {
	if q.Key == "" || len(q.Key) > 160 || strings.ContainsAny(q.Key, "/\x00\r\n") || q.Until.IsZero() {
		return "", "", fmt.Errorf("choose a stable observation decision key and window end")
	}
	hash, err := platform.CanonicalDigest([]any{ref, q.Record, q.Until.UTC()})
	return "observation:" + q.Key, "observation:" + hash, err
}

func (d ObservationDecision) reused(m platform.Member, key, identity string) (ObservationAnswer, *kernel.Error, bool) {
	receipt, refusal, found := d.Prior(m, key, identity)
	if !found {
		return ObservationAnswer{}, nil, false
	}
	return ObservationAnswer{Matched: true, Reused: true, Receipt: receipt}, refusal, true
}

func (d ObservationDecision) Prepare(m platform.Member, ref platform.AssetRef, q ObservationEvaluate, key, identity string, now time.Time) (ObservationPrepared, *kernel.Error) {
	if refusal := d.Admits(m); refusal != nil {
		return ObservationPrepared{}, refusal
	}
	if !m.Active() || m.Tenant != d.Tenant {
		return ObservationPrepared{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation member is unavailable")
	}
	if answer, refusal, found := d.reused(m, key, identity); found {
		return ObservationPrepared{Answer: answer}, refusal
	}
	plan, refusal := d.Sources.Prepare(m, ref, q.Record, now, true)
	if refusal != nil {
		return ObservationPrepared{}, refusal
	}
	read := ObservationRead{Record: q.Record, Since: q.Until.Add(-time.Duration(plan.Info.Definition.WindowSeconds) * time.Second), Until: q.Until, Mode: "summary"}
	if plan.Info.Definition.Decision.Mode == "intervals" {
		read.Mode = "intervals"
		read.Limit = platform.ObservationMaxPoints
	}
	if err := CheckObservationRead(&read, now); err != nil {
		return ObservationPrepared{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	decision := plan.Info.Definition.Decision
	owner := d.App(decision.Action.App)
	if owner == nil {
		return ObservationPrepared{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Observation decision action is unavailable")
	}
	action, declared := owner.Manifest().Actions.Action(decision.Action.Name)
	if err := platform.CheckObservationAction(plan.Info.Definition, action); err != nil {
		return ObservationPrepared{}, ObservationUnavailable(err)
	}
	if refusal := d.Authorize(m, decision.Action.App, action, declared && action.Target == plan.Entity.Type, now, plan.Sources); refusal != nil {
		return ObservationPrepared{}, refusal
	}
	return ObservationPrepared{Member: m, Plan: plan, Read: read}, nil
}

func (d ObservationDecision) Observe(ctx context.Context, p ObservationPrepared, now time.Time) (ObservationOutcome, *kernel.Error) {
	result, err := d.Sources.Fetch(ctx, p.Plan, p.Read, now)
	if err != nil {
		return ObservationOutcome{}, ObservationUnavailable(err)
	}
	outcome := ObservationOutcome{Result: result}
	definition, rules := p.Plan.Info.Definition, d.Sources.host.Rules()
	if result.Intervals != nil {
		outcome.Matched, outcome.Inputs, err = platform.EvaluateObservationIntervals(definition, p.Plan.Entity, p.Plan.Record, *result.Intervals, now, rules)
	} else {
		outcome.Matched, outcome.Inputs, err = platform.EvaluateObservation(definition, p.Plan.Entity, p.Plan.Record, result.Window, now, rules)
	}
	if err != nil {
		return ObservationOutcome{}, ObservationUnavailable(err)
	}
	return outcome, nil
}

func (d ObservationDecision) Commit(m platform.Member, ref platform.AssetRef, q ObservationEvaluate, key, identity string, p ObservationPrepared, o ObservationOutcome, now time.Time) (ObservationAnswer, *kernel.Error) {
	plan := p.Plan
	current, refusal := d.Sources.Prepare(m, ref, q.Record, now, true)
	if refusal != nil {
		return ObservationAnswer{}, refusal
	}
	if current.Stamp != plan.Stamp {
		return ObservationAnswer{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation source or permissions changed while reading")
	}
	if answer, refusal, found := d.reused(m, key, identity); found {
		return answer, refusal
	}
	answer := ObservationAnswer{Observation: &o.Result, Matched: o.Matched}
	if !o.Matched {
		return answer, nil
	}
	payload, err := json.Marshal(o.Inputs)
	if err != nil || len(payload) > 64<<10 {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation decision inputs exceed their bounded scalar shape")
	}
	var source struct {
		Revision uint32 `json:"revision"`
	}
	if json.Unmarshal(plan.Record, &source) != nil {
		return answer, ObservationUnavailable(fmt.Errorf("observation record revision is unavailable"))
	}
	decision := plan.Info.Definition.Decision
	submission := &pb.Submission{TenantId: d.Tenant, PrincipalId: m.ID, Authority: decision.Action.App, IdempotencyKey: key, CorrelationId: identity,
		Target: &pb.EntityRef{Type: plan.Entity.Type, Id: q.Record}, Schema: &pb.SchemaRef{Name: decision.Action.Name, Version: 1}, ExpectedRevision: &source.Revision, Payload: payload}
	evidence := &platform.ObservationDecisionEvidence{Key: key, Definition: platform.AssetBinding{Ref: ref, SourceVersion: plan.Info.Version}, Record: plan.Entity.Type + "/" + q.Record, Device: plan.Device, Signal: plan.Info.Definition.Signal, Window: o.Result.Window, Intervals: o.Result.Intervals}
	answer.Receipt, refusal = d.Accept(m, submission, evidence, now)
	return answer, refusal
}

func ObservationInfos(definitions []platform.Definition) []ObservationInfo {
	out := []ObservationInfo{}
	for _, definition := range definitions {
		if definition.Observation != nil {
			out = append(out, ObservationInfo{Ref: definition.Ref, Version: definition.Version, Definition: *definition.Observation})
		}
	}
	return out
}

func (o Observations) Read(ctx context.Context, q ObservationRead, now time.Time, prepare func() (ObservationPlan, *kernel.Error)) (platform.ObservationResult, *kernel.Error) {
	if err := CheckObservationRead(&q, now); err != nil {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	plan, refusal := prepare()
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if q.Until.Sub(q.Since) > time.Duration(plan.Info.Definition.ExpectedRetentionSeconds)*time.Second {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation time window exceeds the declared retention")
	}
	result, err := o.Fetch(ctx, plan, q, now)
	if err != nil {
		return platform.ObservationResult{}, ObservationUnavailable(err)
	}
	current, refusal := prepare()
	if refusal != nil {
		return platform.ObservationResult{}, refusal
	}
	if current.Stamp != plan.Stamp {
		return platform.ObservationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation source or permissions changed while reading")
	}
	return result, nil
}
