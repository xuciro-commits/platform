package host

import (
	"context"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) operationClaims() compute.OperationClaims {
	return compute.OperationClaims{Tenant: t.ID, App: controlowner.PlatformApp,
		Queued: func(id string) (platform.Effect, bool, bool) {
			x, ok := t.outbound.Get(id)
			return x.Effect, x.Sending, ok
		},
		Claimed: func(id string, after platform.Effect) {
			t.outbound.Update(id, func(x *actions.Queued) { x.Effect, x.Sending = after, true })
		},
		Live: func(id string, generation uint32) bool {
			return t.outbound.Any(func(x actions.Queued) bool {
				return x.ID == id && x.Generation == generation && !platform.EffectSettled(x.State)
			})
		},
		Works: func() *kernel.Works { return t.works }, Fork: t.forkWorkState, Install: func(w *kernel.Works) { t.works = w },
		Quarantine: t.quarantine, Applied: t.acceptedResultApplied}
}

func (t *Tenant) operationCancellations() compute.OperationCancellations {
	return compute.OperationCancellations{Tenant: t.ID, Effect: t.operationEffect,
		Discard: func(id string, after platform.Effect) {
			t.outbound.Update(id, func(x *actions.Queued) { x.Effect, x.Sending = after, false })
		},
		Works: func() *kernel.Works { return t.works }, Fork: t.forkWorkState, Cancel: func(id string) { t.cancels.Cancel(id) }}
}

func (t *Tenant) claimOperation(id string, now time.Time) (platform.Effect, bool) {
	type claim struct {
		effect  platform.Effect
		claimed bool
	}
	value, refusal := decideOnImage(t, context.Background(), "operation:claim", func(t *Tenant) (claim, *kernel.Error) {
		effect, claimed := t.prepareOperationClaim(id, now)
		return claim{effect, claimed}, nil
	}, func(t *Tenant, candidate preparedAccepted, value claim, refusal *kernel.Error) (claim, *kernel.Error) {
		if len(candidate.entry.Body) > 0 {
			busy := t.outbound.Any(func(x actions.Queued) bool { return x.ID == id && x.Sending })
			if busy {
				return claim{}, nil
			}
			value.claimed = t.finishOperationClaim(candidate)
		}
		return value, refusal
	})
	return value.effect, value.claimed && refusal == nil
}

func (t *Tenant) prepareOperationClaim(id string, now time.Time) (platform.Effect, bool) {
	if t.businessStopped() {
		return platform.Effect{}, false
	}
	r, raw, ok := t.operationClaims().Prepare(id, now)
	if !ok {
		return platform.Effect{}, false
	}
	candidate := preparedAccepted{entry: runtimekernel.AcceptedEntry(controlowner.PlatformApp, t.automation(controlowner.PlatformApp, false).Member, raw, now, nil), key: r.Key, hash: r.RequestHash}
	if t.stage(candidate) {
		return r.After, true
	}
	return r.After, t.finishOperationClaim(candidate)
}

func (t *Tenant) finishOperationClaim(candidate preparedAccepted) bool {
	saved, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	return err == nil && t.operationClaims().Settle(saved)
}

func (t *Tenant) operationDispatches(now time.Time) []func() {
	var runs []func()
	for _, id := range actions.RunnableIDs(t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.OperationEndpoint }), now, 4) {
		if x, ok := t.claimOperation(id, now); ok {
			runs = append(runs, func() { out := t.executeOperation(x, now); t.settleWithUsage(x.ID, out, nil, time.Now().UTC()) })
		}
	}
	return runs
}

func (d *stagedDecision) CancelOperation(c platform.Caller, r *pb.ChangeRecord, id string) *kernel.Error {
	record, refusal := compute.CheckCancel(d.tenant.ID, c, r, id, d.tenant.operationEffect)
	if refusal != nil || !record {
		return refusal
	}
	if d.OperationCancels == nil {
		d.OperationCancels = map[string]bool{}
	}
	d.OperationCancels[id] = true
	return nil
}

func (d *stagedDecision) savedOperationCancellations() ([]acceptedformat.OperationCancellation, error) {
	return d.tenant.operationCancellations().Saved(d.OperationCancels)
}

func (h hostView) Operation(app, name string, version int) (platform.Operation, int, bool) {
	owner := h.t.App(app)
	if owner == nil {
		return platform.Operation{}, 0, false
	}
	return compute.OperationDefinition(owner, name, version)
}
