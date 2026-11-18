package host

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	compute "platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) InvokeOperation(member platform.Member, q platform.OperationRequest, now time.Time) (platform.OperationCall, *platform.Error) {
	if refusal := compute.CheckDirectCall(q); refusal != nil {
		return platform.OperationCall{}, refusal
	}
	q.Sources = slices.Clone(q.Sources)
	var prepared *preparedOperationInput
	initial, refusal := recordRead(t, context.Background(), func(t *Tenant) (compute.CallPlan, *platform.Error) {
		return compute.CallSetup{Tenant: t.ID, Member: t.Member, App: t.App, Definition: t.operationDefinition, Quarantined: t.quarantined,
			Prior: func(callID string) (string, bool) {
				x, ok := t.outbound.Get(callID)
				return x.Body, ok
			},
			Authorize: func(current platform.Member, op platform.Operation) *platform.Error {
				c := platform.NewCaller(runtime{t: t, scoped: true}, current, q.App, false, false)
				return authorizationRefusal(t.authorizeCapability(c, q.App, op.Name, op.Roles, op.Check() == nil, authorizationDetails{At: now, Sources: q.Sources}))
			}}.Prepare(member, q)
	})
	if refusal != nil {
		return platform.OperationCall{}, refusal
	}
	current, op, version, id, seal := initial.Member, initial.Operation, initial.Version, initial.ID, initial.Seal
	if seal {
		sealed, ref, requestHash, refusal := t.staged.SealCallInput(id, current.ID, q, op, version)
		if refusal != nil {
			return platform.OperationCall{}, refusal
		}
		q = sealed
		prepared = &preparedOperationInput{Artifact: ref, RequestHash: requestHash}
	} else if len(q.Inputs) > 1<<20 {
		return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds the operation inline budget")
	}
	retained := false
	defer func() {
		if prepared != nil && !retained {
			t.staged.DiscardInput(prepared.Artifact)
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
		keep, refusal := decideOnImage(t, context.Background(), "operation:call", func(t *Tenant) (bool, *platform.Error) {
			latest, ok := t.Member(current.ID)
			if !ok || t.quarantined() || t.Admits(latest) != nil {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The operation member is unavailable")
			}
			owner := t.App(controlowner.PlatformApp)
			if owner == nil {
				return false, unknown()
			}
			known := t.PriorOf(owner, q.Key).Known()
			_, refusal := t.accept(owner, latest, s, now, acceptOptions{inputs: []*preparedOperationInput{prepared}})
			return !known, refusal
		}, func(t *Tenant, decision preparedAccepted, keep bool, refusal *platform.Error) (bool, *platform.Error) {
			_, refusal = t.finishPreparedSubmission(current, decision, nil, refusal)
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
			ticket = prepared.Artifact.Ticket
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

func (t *Tenant) ReadOperation(ctx context.Context, member platform.Member, id string) (platform.OperationResult, *platform.Error) {
	return recordRead(t, ctx, func(t *Tenant) (platform.OperationResult, *platform.Error) {
		return t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, member, controlowner.PlatformApp, false, false), id)
	})
}
