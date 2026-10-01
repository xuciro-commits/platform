package platformserver

import (
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
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
	var selected *platform.CapabilityDescriptor
	if q.Version > 0 {
		if descriptor, ok := t.retainedCapability(m, q.Ref, q.Version); ok {
			selected = &descriptor
		}
	}
	for _, descriptor := range t.Capabilities(m) {
		if selected == nil && q.Version == 0 && descriptor.Ref == q.Ref {
			selected = &descriptor
			break
		}
	}
	if selected == nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability is unavailable to this member")
	}
	switch selected.Kind {
	case "query":
		if q.Version != 0 || q.Target != "" || q.ExpectedRevision != nil || len(q.Sources) != 0 || q.Record != "" || len(q.Bindings) != 0 {
			return bad("Query takes only its declared inputs")
		}
		caller := platform.NewCaller(runtime{t}, m, q.Ref.App, false, false)
		result, err := caller.ReadQuery(q.Ref.App, q.Ref.Name, q.Inputs, now)
		answer.State, answer.Result = "completed", result
		return answer, err
	case "action":
		if q.Version != 0 || q.Target == "" || len(q.Sources) != 0 {
			return bad("Action requires its target and canonical payload")
		}
		payload := q.Inputs
		if len(q.Bindings) > 0 || q.Record != "" {
			bound, _, refusal := t.bindCapabilityInputs(m, q, now)
			if refusal != nil {
				return answer, refusal
			}
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
		record, err := t.Submit(m, s, now)
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
		if q.Target != "" || q.ExpectedRevision != nil {
			return bad("Compute takes typed inputs and protected source references")
		}
		inputs, sources, refusal := t.bindCapabilityInputs(m, q, now)
		if refusal != nil {
			return answer, refusal
		}
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
