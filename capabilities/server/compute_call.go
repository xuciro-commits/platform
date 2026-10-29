package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"time"
)

func (t *Tenant) InvokeOperation(member platform.Member, q platform.OperationRequest, now time.Time) (platform.OperationCall, *kernel.Error) {
	if q.App == "" || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || q.Target != "" || q.OnBehalf != "" || q.Release != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A direct call requires an owner and stable key")
	}
	q.Sources = slices.Clone(q.Sources)
	var prepared *preparedOperationInput
	t.mu.Lock()
	current, seated := t.Member(member.ID)
	app := t.app(q.App)
	var op platform.Operation
	var version int
	var declared bool
	if app != nil {
		op, version, declared = operationDefinition(app, q.Name, q.Version)
	}
	id := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, q.App, q.Key, acceptedformat.OperationEndpoint)
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
		if !seated || member.Tenant != t.ID || t.quarantined() {
			t.mu.Unlock()
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot prepare the operation input")
		}
		c := platform.NewCaller(runtime{t}, current, q.App, false, false)
		if refusal := authorizationRefusal(t.authorizeCapability(c, q.App, op.Name, op.Roles, op.Check() == nil, authorizationDetails{At: now, Sources: q.Sources})); refusal != nil {
			t.mu.Unlock()
			return platform.OperationCall{}, refusal
		}
		var frozen platform.Operation
		_ = json.Unmarshal(platform.Raw(op), &frozen)
		op = frozen
	}
	t.mu.Unlock()
	if seal {

		if len(q.Inputs) > op.Limits.DataInputBytes {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds its declared data channel budget")
		}
		raw := bytes.Clone(q.Inputs)
		if err := op.Input.Validate(raw, op.Limits.DataInputBytes); err != nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		hash, _ := platform.CanonicalDigest(json.RawMessage(raw))
		definition, _ := platform.CanonicalDigest([]any{q.App, op, version})
		ref, err := t.staged.SealInput(id, current.ID, definition, hash, raw, op.Limits.DataInputBytes)
		if err != nil {
			return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		q.Inputs = platform.Raw(map[string]any{"sealedInput": map[string]any{"digest": ref.Digest, "size": ref.Size, "inputHash": ref.InputHash, "definition": ref.Definition}})
		requestHash, _ := platform.CanonicalDigest(q)
		prepared = &preparedOperationInput{ref: ref, requestHash: requestHash}
	} else if len(q.Inputs) > 1<<20 {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds the operation inline budget")
	}
	retained := false
	defer func() {
		if prepared != nil && !retained {
			t.staged.DiscardInput(prepared.ref)
		}
	}()
	payload, err := json.Marshal(q)
	if err != nil {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Operation input cannot be encoded")
	}
	s := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: controlowner.PlatformApp, IdempotencyKey: q.Key, Target: &pb.EntityRef{Type: controlowner.OperationType, Id: q.Key}, Schema: &pb.SchemaRef{Name: controlowner.SchemaOperationCall, Version: 1}, Payload: payload}
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
		owner, ok := t.app(controlowner.PlatformApp).(platform.App)
		if !ok {
			return platform.OperationCall{}, unknown()
		}
		_, refusedBefore := t.committed.Refusal(controlowner.PlatformApp + "/" + q.Key)
		known := refusedBefore || len(t.committed.Answer(controlowner.PlatformApp+"/"+q.Key)) > 0 || owner.AcceptedLedger().AcceptedFor(t.ID, q.Key) != nil
		retained = !known
		if _, err := t.submitAccepted(owner, latest, s, now, false, prepared); err != nil {
			if _, rejected := t.committed.Refusal(controlowner.PlatformApp + "/" + q.Key); rejected {
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
	return recordRead(t, context.Background(), func() (platform.OperationResult, *kernel.Error) {
		return t.operationResult(platform.NewCaller(runtime{t}, member, controlowner.PlatformApp, false, false), id)
	})
}
