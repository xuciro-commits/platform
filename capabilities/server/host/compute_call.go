package host

import (
	"context"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) InvokeOperation(member platform.Member, q platform.OperationRequest, now time.Time) (platform.OperationCall, *kernel.Error) {
	if refusal := compute.CheckDirectCall(q); refusal != nil {
		return platform.OperationCall{}, refusal
	}
	q.Sources = slices.Clone(q.Sources)
	var prepared *preparedOperationInput
	type setup struct {
		member    platform.Member
		operation platform.Operation
		version   int
		id        string
		seal      bool
	}
	initial, refusal := recordRead(t, context.Background(), func(t *Tenant) (setup, *kernel.Error) {
		current, seated := t.Member(member.ID)
		app := t.App(q.App)
		var op platform.Operation
		var version int
		var declared bool
		if app != nil {
			op, version, declared = compute.OperationDefinition(app, q.Name, q.Version)
		}
		callID := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, q.App, q.Key, acceptedformat.OperationEndpoint)
		if x, ok := t.outbound.Get(callID); ok {
			if prior, priorVersion, sealed := compute.PriorSealedDefinition(x.Body); sealed {
				op, version, declared = prior, priorVersion, true
			}
		}
		seal := declared && op.Limits.DataInputBytes > 0
		if seal {
			if !seated || member.Tenant != t.ID || t.quarantined() {
				return setup{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot prepare the operation input")
			}
			c := platform.NewCaller(runtime{t: t, scoped: true}, current, q.App, false, false)
			if refusal := authorizationRefusal(t.authorizeCapability(c, q.App, op.Name, op.Roles, op.Check() == nil, authorizationDetails{At: now, Sources: q.Sources})); refusal != nil {
				return setup{}, refusal
			}
			var frozen platform.Operation
			_ = json.Unmarshal(platform.Raw(op), &frozen)
			op = frozen
		}
		return setup{current, op, version, callID, seal}, nil
	})
	if refusal != nil {
		return platform.OperationCall{}, refusal
	}
	current, op, version, id, seal := initial.member, initial.operation, initial.version, initial.id, initial.seal
	if seal {
		sealed, ref, requestHash, refusal := t.staged.SealCallInput(id, current.ID, q, op, version)
		if refusal != nil {
			return platform.OperationCall{}, refusal
		}
		q = sealed
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
		keep, refusal := decideOnImage(t, context.Background(), "operation:call", func(t *Tenant) (bool, *kernel.Error) {
			latest, ok := t.Member(current.ID)
			if !ok || t.quarantined() || t.Admits(latest) != nil {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The operation member is unavailable")
			}
			owner := t.App(controlowner.PlatformApp)
			if owner == nil {
				return false, unknown()
			}
			known := t.priorOf(owner, q.Key).Known()
			_, refusal := t.accept(owner, latest, s, now, acceptOptions{inputs: []*preparedOperationInput{prepared}})
			return !known, refusal
		}, func(t *Tenant, candidate preparedAccepted, keep bool, refusal *kernel.Error) (bool, *kernel.Error) {
			_, refusal = t.finishPreparedSubmission(current, candidate, nil, refusal)
			if _, rejected := t.Committed.Refusal(controlowner.PlatformApp + "/" + q.Key); rejected {
				keep = false
			}
			return keep, refusal
		})
		retained = keep
		if refusal != nil {
			return platform.OperationCall{}, refusal
		}
	}
	if x, ok := t.outbound.Get(id); ok {
		ticket := ""
		if prepared != nil {
			ticket = prepared.ref.Ticket
		}
		if call, keep, decoded := compute.AcceptedCall(x.Body, ticket, prepared != nil); decoded {
			if prepared != nil {
				retained = keep
			}
			return call, nil
		}
	}
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted operation intent is unavailable")
}

func (t *Tenant) ReadOperation(ctx context.Context, member platform.Member, id string) (platform.OperationResult, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (platform.OperationResult, *kernel.Error) {
		return t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, member, controlowner.PlatformApp, false, false), id)
	})
}
