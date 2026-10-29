package platformserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

// InvokeCapability is a projection onto the original owner entry points.
// Discovery is checked here; the selected owner still checks current authority,
// source visibility, input schema, versions and the canonical idempotency key.
func (t *Tenant) InvokeCapability(m platform.Member, q platform.CapabilityInvocation, now time.Time) (platform.CapabilityResult, *kernel.Error) {
	answer := platform.CapabilityResult{Ref: q.Ref}
	if refusal := t.admits(m); refusal != nil {
		return answer, refusal
	}
	bad := func(message string) (platform.CapabilityResult, *kernel.Error) {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	if len(q.Key) == 0 || len(q.Key) > 200 || strings.ContainsAny(q.Key, "/\x00\r\n") || q.Version < 0 {
		return bad("Choose a capability and bounded stable invocation key")
	}
	if _, err := platform.DecodeValue(q.Inputs, 64<<10); err != nil {
		return bad(err.Error())
	}
	prepared, refusal := recordRead(t, context.Background(), func() (capabilityPreparation, *kernel.Error) {
		return t.prepareCapability(m, q, now)
	})
	if refusal != nil {
		return answer, refusal
	}
	selected := prepared.descriptor
	switch selected.Kind {
	case "query":
		answer.State, answer.Result = "completed", prepared.query
		return answer, nil
	case "action":
		payload := q.Inputs
		if len(q.Bindings) > 0 || q.Record != "" {
			bound := prepared.inputs
			var inputs, values map[string]json.RawMessage
			if json.Unmarshal(q.Inputs, &inputs) != nil || inputs == nil || json.Unmarshal(bound, &values) != nil {
				return bad("Bound action inputs need an object payload")
			}
			for name, value := range values {
				inputs[name] = value // fresh scoped values replace client previews
			}
			payload, _ = json.Marshal(inputs)
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
			// An approval request deliberately has no accepted business change.
			answer.State = "pending"
		}
		return answer, nil
	case "compute":
		inputs, sources := prepared.inputs, prepared.sources
		call, err := t.InvokeOperation(m, platform.OperationRequest{App: q.Ref.App, Name: q.Ref.Name, Version: q.Version, Key: q.Key, Inputs: inputs, Sources: sources}, now)
		answer.State, answer.Call = "pending", call.ID
		return answer, err
	case "ai":
		if q.Target != "" || q.ExpectedRevision != nil || len(q.Sources) != 0 || q.Record != "" || len(q.Bindings) != 0 {
			return bad("AI function takes a readable source record")
		}
		input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}
		if err := input.Validate(q.Inputs, 64<<10); err != nil {
			return bad(err.Error())
		}
		var values struct {
			Source string `json:"source"`
		}
		_ = json.Unmarshal(q.Inputs, &values)
		payload, _ := json.Marshal(map[string]any{"app": q.Ref.App, "name": q.Ref.Name, "source": values.Source, "version": q.Version})
		s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: q.Key,
			Target: &pb.EntityRef{Type: build.FunctionCallType, Id: q.Key}, Schema: &pb.SchemaRef{Name: build.SchemaFunctionCall, Version: 1}, Payload: payload}
		_, err := t.Submit(m, s, now)
		answer.State, answer.Call = "pending", q.Key
		return answer, err
	default:
		return bad("This descriptor is a design control, not a callable capability")
	}
}

// Selection and source bindings share one definition/record view. The owner
// entry point runs after that read ends, retaining its own current checks and
// lock-free input preparation or external work.
type capabilityPreparation struct {
	descriptor platform.CapabilityDescriptor
	inputs     json.RawMessage
	sources    []string
	query      json.RawMessage
}

func (t *Tenant) prepareCapability(m platform.Member, q platform.CapabilityInvocation, now time.Time) (capabilityPreparation, *kernel.Error) {
	out := capabilityPreparation{}
	selected := false
	if q.Version > 0 {
		out.descriptor, selected = t.retainedCapability(m, q.Ref, q.Version)
	} else {
		for _, descriptor := range t.capabilities(m) {
			if descriptor.Ref == q.Ref {
				out.descriptor, selected = descriptor, true
				break
			}
		}
	}
	if !selected {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability is unavailable to this member")
	}
	if out.descriptor.Kind == "query" {
		if out.descriptor.Source == "tenant" && q.Version == 0 || q.Target != "" || q.ExpectedRevision != nil || len(q.Sources) != 0 || q.Record != "" || len(q.Bindings) != 0 {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query takes only its declared inputs")
		}
		caller := platform.NewCaller(runtime{t}, m, q.Ref.App, false, false)
		var refusal *kernel.Error
		out.query, refusal = caller.ReadQueryVersion(q.Ref.App, q.Ref.Name, q.Version, q.Inputs, now)
		return out, refusal
	}
	if out.descriptor.Kind == "action" && (q.Version != 0 || q.Target == "" || len(q.Sources) != 0) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action requires its target and canonical payload")
	}
	if out.descriptor.Kind == "compute" && (q.Target != "" || q.ExpectedRevision != nil) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Compute takes typed inputs and protected source references")
	}
	if out.descriptor.Kind == "compute" || out.descriptor.Kind == "action" && (len(q.Bindings) > 0 || q.Record != "") {
		var refusal *kernel.Error
		out.inputs, out.sources, refusal = t.bindCapabilityInputs(m, q, now)
		return out, refusal
	}
	return out, nil
}
