package host

import (
	"context"
	"encoding/json"
	"platformserver/services/apps/pages"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	functions "platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) InvokeCapability(ctx context.Context, m platform.Member, q platform.CapabilityInvocation, now time.Time) (platform.CapabilityResult, *kernel.Error) {
	answer := platform.CapabilityResult{Ref: q.Ref}
	if refusal := t.Admits(m); refusal != nil {
		return answer, refusal
	}
	if refusal := functions.CheckInvocation(q); refusal != nil {
		return answer, refusal
	}
	prepared, refusal := recordRead(t, ctx, func(t *Tenant) (functions.CapabilityPreparation, *kernel.Error) {
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
		record, err, issues := t.submitDiagnosed(m, s, now)
		answer.Issues = issues
		if err != nil {
			return answer, err
		}
		if record != nil {
			answer.Result, _ = protojson.Marshal(record)
			answer.State = "completed"
			if record.GetSubmission().GetTarget().GetType() == "work.approval" && record.GetSubmission().GetSchema().GetName() == "work.approval.request" {
				answer.State = "pending"
			}
		} else {
			answer.State = "pending"
		}
		return answer, nil
	case "compute":
		inputs, sources := prepared.Inputs, prepared.Sources
		call, err := t.InvokeOperation(m, platform.OperationRequest{App: q.Ref.App, Name: q.Ref.Name, Version: q.Version, Key: q.Key, Inputs: inputs, Sources: sources}, now)
		answer.State, answer.Call = "pending", call.ID
		return answer, err
	case "ai":
		payload, refusal := functions.FunctionCallPayload(q)
		if refusal != nil {
			return answer, refusal
		}
		s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: q.Key,
			Target: &pb.EntityRef{Type: build.FunctionCallType, Id: q.Key}, Schema: &pb.SchemaRef{Name: build.SchemaFunctionCall, Version: 1}, Payload: payload}
		_, err := t.Submit(m, s, now)
		answer.State, answer.Call = "pending", q.Key
		return answer, err
	default:
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This descriptor is a design control, not a callable capability")
	}
}

func (t *Tenant) prepareCapability(m platform.Member, q platform.CapabilityInvocation, now time.Time) (functions.CapabilityPreparation, *kernel.Error) {
	return functions.PrepareCapability(functions.CapabilityHooks{
		Retained: func() (platform.CapabilityDescriptor, bool) { return t.retainedCapability(m, q.Ref, q.Version) },
		Palette:  func() []platform.CapabilityDescriptor { return t.capabilities(m) },
		ReadQuery: func() (json.RawMessage, *kernel.Error) {
			return platform.NewCaller(runtime{t: t}, m, q.Ref.App, false, false).ReadQueryVersion(q.Ref.App, q.Ref.Name, q.Version, q.Inputs, now)
		},
		Bind: func() (json.RawMessage, []string, *kernel.Error) {
			return pages.BindCapabilityInputs(platform.NewCaller(runtime{t: t, scoped: true}, m, q.Ref.App, false, false), q, now)
		},
	}, q)
}
