package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const OperationType = "platform.operation"
const SchemaOperationCall = OperationType + ".call"

func operationActions() []platform.Action {
	return []platform.Action{{Schema: SchemaOperationCall, Target: OperationType, Capability: "compute", Title: "Call operation", Description: "Invoke a registered typed operation through its owner and accepted work.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "The operation owner"}, {Name: "name", Type: "string", Required: true, Description: "The registered operation name"}, {Name: "version", Type: "integer", Description: "The retained owner version"}, {Name: "key", Type: "string", Required: true, Description: "The stable invocation key"}, {Name: "inputs", Type: "json", Required: true, Description: "The typed operation input"}, {Name: "sources", Type: "string[]", Description: "Protected source references"}}}}
}
func (d *Console) decideOperation(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var q platform.OperationRequest
	if json.Unmarshal(s.GetPayload(), &q) != nil || q.Key != s.GetIdempotencyKey() || s.GetTarget().GetId() != q.Key || q.App == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an operation and stable input key")
	}
	return func(r *pb.ChangeRecord) { _, _ = c.RequestOperation(r, q) }, nil
}

// InvokeOperation is the public compute route. The console contributes only an
// invocation receipt; the selected operation owner decides permission/schema.
func (t *Tenant) InvokeOperation(member platform.Member, q platform.OperationRequest, now time.Time) (platform.OperationCall, *kernel.Error) {
	if q.App == "" || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || q.Target != "" || q.OnBehalf != "" || q.Release != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A direct call requires an owner and stable key")
	}
	q.Sources = slices.Clone(q.Sources)
	var prepared *preparedOperationInput
	// Capture a retained definition and current grants under the submission
	// lock; schema decoding and FileStore I/O belong to the preparation lane.
	t.mu.Lock()
	current, seated := t.Member(member.ID)
	app := t.app(q.App)
	var op platform.Operation
	var version int
	var declared bool
	if app != nil {
		op, version, declared = operationDefinition(app, q.Name, q.Version)
	}
	id := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, q.App, q.Key, operationEndpoint)
	t.opsMu.Lock()
	for _, x := range t.outbound {
		if x.ID == id {
			var previous operationBinding
			if json.Unmarshal([]byte(x.Body), &previous) == nil && previous.SealedInput != nil {
				op, version, declared = previous.Definition, previous.Call.Version, true
			}
			break
		}
	}
	t.opsMu.Unlock()
	seal := declared && op.Limits.DataInputBytes > 0
	if seal {
		if !seated || member.Tenant != t.ID || t.quarantined() || t.admits(current) != nil || op.Check() != nil || !slices.Contains(op.Roles, current.Roles[q.App]) {
			t.mu.Unlock()
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot prepare the operation input")
		}
		for _, source := range q.Sources {
			if !t.mayRead(current, now, false)(source) {
				t.mu.Unlock()
				return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation source is not readable")
			}
		}
		// Neither the caller nor a later definition edit can mutate this plan.
		var frozen platform.Operation
		_ = json.Unmarshal(platform.Raw(op), &frozen)
		op = frozen
	}
	t.mu.Unlock()
	if seal {
		if t.AcceptResult == nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Sealed operation inputs require accepted-result storage")
		}
		if len(q.Inputs) > op.Limits.DataInputBytes {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds its declared data channel budget")
		}
		raw := bytes.Clone(q.Inputs)
		if err := op.Input.Validate(raw, op.Limits.DataInputBytes); err != nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		hash, _ := canonicalDigest(json.RawMessage(raw))
		definition, _ := canonicalDigest([]any{q.App, op, version})
		ref, err := t.staged.sealInput(id, current.ID, definition, hash, raw, op.Limits.DataInputBytes)
		if err != nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		// The receipt binds content, rather than a random preparation attempt.
		q.Inputs = platform.Raw(map[string]any{"sealedInput": map[string]any{"digest": ref.Digest, "size": ref.Size, "inputHash": ref.InputHash, "definition": ref.Definition}})
		requestHash, _ := canonicalDigest(q)
		prepared = &preparedOperationInput{ref: ref, requestHash: requestHash}
	} else if len(q.Inputs) > 1<<20 {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds the operation inline budget")
	}
	retained := false
	defer func() {
		if prepared != nil && !retained {
			t.staged.discardInput(prepared.ref)
		}
	}()
	payload, err := json.Marshal(q)
	if err != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Operation input cannot be encoded")
	}
	s := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: PlatformApp, IdempotencyKey: q.Key, Target: &pb.EntityRef{Type: OperationType, Id: q.Key}, Schema: &pb.SchemaRef{Name: SchemaOperationCall, Version: 1}, Payload: payload}
	if prepared == nil {
		if _, err := t.Submit(member, s, now); err != nil {
			return platform.OperationCall{}, err
		}
	} else {
		t.mu.Lock()
		defer t.mu.Unlock()
		latest, ok := t.Member(current.ID)
		if !ok || t.quarantined() || t.admits(latest) != nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The operation member is unavailable")
		}
		owner, ok := t.app(PlatformApp).(platform.ResultApp)
		if !ok {
			return platform.OperationCall{}, unknown()
		}
		_, refusedBefore := t.committed.refusals[PlatformApp+"/"+q.Key]
		known := refusedBefore || len(t.committed.answers[PlatformApp+"/"+q.Key]) > 0 || owner.AcceptedLedger().AcceptedFor(t.ID, q.Key) != nil
		retained = !known // an append error can have committed without its answer
		if _, err := t.submitAccepted(owner, latest, s, now, false, prepared); err != nil {
			if _, rejected := t.committed.refusals[PlatformApp+"/"+q.Key]; rejected {
				retained = false
			}
			return platform.OperationCall{}, err
		}
		t.enqueue(now)
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, x := range t.outbound {
		if x.ID == id {
			var binding operationBinding
			if json.Unmarshal([]byte(x.Body), &binding) == nil {
				if prepared != nil {
					retained = binding.SealedInput != nil && binding.SealedInput.Ticket == prepared.ref.Ticket
				}
				return binding.Call, nil
			}
		}
	}
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted operation intent is unavailable")
}
func (t *Tenant) ReadOperation(member platform.Member, id string) (platform.OperationResult, *kernel.Error) {
	return t.operationResult(platform.NewCaller(runtime{t}, member, PlatformApp, false, false), id)
}
