package host

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"
	"platformserver/services/apps/pages"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	functions "platformserver/services/functions/authoring"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) InvokeCapability(ctx context.Context, m platform.Member, q protocol.CapabilityInvocation, now time.Time) (protocol.CapabilityResult, *platform.Error) {
	answer := protocol.CapabilityResult{Ref: q.Ref}
	if refusal := t.Admits(m); refusal != nil {
		return answer, refusal
	}
	if refusal := functions.CheckInvocation(q); refusal != nil {
		return answer, refusal
	}
	prepared, refusal := recordRead(t, ctx, func(t *Tenant) (functions.CapabilityPreparation, *platform.Error) {
		return t.prepareCapability(m, q, now)
	})
	if refusal != nil {
		return answer, refusal
	}
	selected := prepared.Descriptor
	switch selected.Kind {
	case "query":
		answer.State, answer.Result = "completed", prepared.Query
		return answer, nil
	case "action":
		payload, refusal := functions.BoundActionPayload(q, prepared.Inputs)
		if refusal != nil {
			return answer, refusal
		}
		s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: q.Ref.App, IdempotencyKey: q.Key,
			Target: &pb.EntityRef{Type: selected.Target, Id: q.Target}, Schema: &pb.SchemaRef{Name: q.Ref.Name, Version: 1}, Payload: payload, ExpectedRevision: q.ExpectedRevision}
		record, err, issues := t.submit(ctx, m, s, now, submitOptions{})
		answer.Issues = issues
		if err != nil {
			return answer, err
		}
		return functions.ActionAnswer(answer, record), nil
	case "compute":
		call, err := t.InvokeOperation(m, platform.OperationRequest{App: q.Ref.App, Name: q.Ref.Name, Version: q.Version, Key: q.Key, Inputs: prepared.Inputs, Sources: prepared.Sources}, now)
		answer.State, answer.Call = "pending", call.ID
		return answer, err
	case "ai":
		payload, refusal := functions.FunctionCallPayload(q)
		if refusal != nil {
			return answer, refusal
		}
		s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: q.Key,
			Target: &pb.EntityRef{Type: protocol.FunctionCallType, Id: q.Key}, Schema: &pb.SchemaRef{Name: protocol.SchemaFunctionCall, Version: 1}, Payload: payload}
		_, err, _ := t.submit(ctx, m, s, now, submitOptions{})
		answer.State, answer.Call = "pending", q.Key
		return answer, err
	default:
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This descriptor is a design control, not a callable capability")
	}
}

func (t *Tenant) prepareCapability(m platform.Member, q protocol.CapabilityInvocation, now time.Time) (functions.CapabilityPreparation, *platform.Error) {
	return functions.PrepareCapability(functions.CapabilityHooks{
		Retained: func() (protocol.CapabilityDescriptor, bool) { return t.retainedCapability(m, q.Ref, q.Version) },
		Palette:  func() []protocol.CapabilityDescriptor { return t.capabilities(m) },
		ReadQuery: func() (json.RawMessage, *platform.Error) {
			return platform.NewCaller(runtime{t: t}, m, q.Ref.App, false, false).ReadQueryVersion(q.Ref.App, q.Ref.Name, q.Version, q.Inputs, now)
		},
		Bind: func() (json.RawMessage, []string, *platform.Error) {
			return pages.BindCapabilityInputs(platform.NewCaller(runtime{t: t, scoped: true}, m, q.Ref.App, false, false), q, now)
		},
	}, q)
}
