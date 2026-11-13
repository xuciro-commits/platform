package host

import (
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) applyOperationClaim(raw []byte) (acceptedformat.OperationClaim, error) {
	saved, err := acceptedformat.DecodeOperationClaim(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID {
		return saved, fmt.Errorf("invalid operation ownership tenant")
	}
	current, ok := t.outbound.Get(saved.Before.ID)
	if !ok {
		return saved, fmt.Errorf("operation claim has no accepted intent")
	}
	same, _ := platform.CanonicalDigest(current.Effect)
	expected, _ := platform.CanonicalDigest(saved.Before)
	if same != expected {
		return saved, fmt.Errorf("operation claim predecessor differs")
	}
	works := t.forkWorkState()
	if err := compute.ReplayClaim(works, saved.Before, saved.After, saved.Key, saved.PriorWork, saved.Work); err != nil {
		return saved, err
	}
	t.works = works
	t.outbound.Update(saved.Before.ID, func(x *actions.Queued) { x.Effect, x.Sending = saved.After, true })
	return saved, nil
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
	queued, ok := t.outbound.Get(id)
	if !ok || queued.Sending || platform.EffectSettled(queued.State) || queued.Due.After(now) {
		return platform.Effect{}, false
	}
	before := queued.Effect
	generation, prior, rawWork, ok := compute.StartClaim(t.forkWorkState(), id)
	if !ok {
		return platform.Effect{}, false
	}
	after := before
	after.Generation = generation
	r := acceptedformat.OperationClaim{Version: 1, Kind: "operation-claim", Tenant: t.ID, App: controlowner.PlatformApp, At: now.UTC(), Key: fmt.Sprintf("operation-claim:%s:%d", id, generation), Before: before, After: after, PriorWork: prior, Work: rawWork}
	r.RequestHash, _ = platform.CanonicalDigest([]any{before, prior})
	r.Digest, _ = acceptedformat.OperationClaimDigest(r)
	raw, _ := json.Marshal(r)
	principal, _ := json.Marshal(t.automation(controlowner.PlatformApp, false).Member)
	candidate := preparedAccepted{entry: Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: now}, key: r.Key, hash: r.RequestHash}
	if t.preparedResult != nil {
		*t.preparedResult = candidate
		return after, true
	}
	return after, t.finishOperationClaim(candidate)
}

func (t *Tenant) finishOperationClaim(candidate preparedAccepted) bool {
	saved, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return false
	}
	if _, err := t.applyOperationClaim(saved); err != nil {
		t.quarantine(err)
		return false
	}
	t.acceptedResultApplied()
	return true
}
func (t *Tenant) operationCurrent(id string, generation uint32) bool {
	work, err := t.works.Get(id)
	if err != nil || work.GetGeneration() != generation || work.GetState() != pb.WorkState_WORK_STATE_RUNNING {
		return false
	}
	return t.outbound.Any(func(x actions.Queued) bool {
		return x.ID == id && x.Generation == generation && !platform.EffectSettled(x.State)
	})
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
func (t *Tenant) prepareOperationFinish(before platform.Effect, out platform.Outcome) (*kernel.Works, json.RawMessage, json.RawMessage, error) {
	if before.Endpoint != acceptedformat.OperationEndpoint {
		return nil, nil, nil, nil
	}
	if out.Generation != before.Generation || !t.operationCurrent(before.ID, out.Generation) {
		return nil, nil, nil, fmt.Errorf("stale operation completion")
	}
	works := t.forkWorkState()
	rawPrior, rawWork, err := compute.FinishWork(works, before.ID, out.Generation, out.Result != "delivered")
	if err != nil {
		return nil, nil, nil, err
	}
	return works, rawPrior, rawWork, nil
}
