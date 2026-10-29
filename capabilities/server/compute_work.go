package platformserver

import (
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"

	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (t *Tenant) applyOperationClaim(raw []byte) (acceptedformat.OperationClaim, error) {
	saved, err := acceptedformat.DecodeOperationClaim(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID {
		return saved, fmt.Errorf("invalid operation ownership tenant")
	}
	t.opsMu.Lock()
	i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == saved.Before.ID })
	if i < 0 {
		t.opsMu.Unlock()
		return saved, fmt.Errorf("operation claim has no accepted intent")
	}
	current := t.outbound[i]
	same, _ := platform.CanonicalDigest(current.Effect)
	expected, _ := platform.CanonicalDigest(saved.Before)
	t.opsMu.Unlock()
	if same != expected {
		return saved, fmt.Errorf("operation claim predecessor differs")
	}
	works := t.forkWorkState()
	var prior json.RawMessage
	if old, err := works.Get(saved.Before.ID); err == nil {
		prior, _ = protojson.Marshal(old)
	}
	priorHash, _ := platform.CanonicalDigest(prior)
	savedHash, _ := platform.CanonicalDigest(saved.PriorWork)
	if priorHash != savedHash {
		return saved, fmt.Errorf("operation K9 predecessor differs")
	}
	if old, err := works.Get(saved.Before.ID); err == nil && old.GetState() == pb.WorkState_WORK_STATE_RUNNING {
		if works.Cancel(saved.Before.ID) != nil {
			return saved, fmt.Errorf("operation owner cannot be invalidated")
		}
	}
	generation, _, refusal := works.Start(saved.Before.ID, "host-compute")
	if refusal != nil || generation != saved.After.Generation || saved.Key != fmt.Sprintf("operation-claim:%s:%d", saved.Before.ID, generation) {
		return saved, fmt.Errorf("operation generation differs")
	}
	after := saved.Before
	after.Generation = generation
	actual, _ := platform.CanonicalDigest(after)
	frozen, _ := platform.CanonicalDigest(saved.After)
	var work pb.Work
	if actual != frozen || protojson.Unmarshal(saved.Work, &work) != nil {
		return saved, fmt.Errorf("invalid operation claim transition")
	}
	check, _ := works.Get(saved.Before.ID)
	if !proto.Equal(check, &work) {
		return saved, fmt.Errorf("invalid operation claim work")
	}
	t.works = works
	t.opsMu.Lock()
	current.Effect = saved.After
	current.sending = true
	t.opsMu.Unlock()
	return saved, nil
}
func (t *Tenant) claimOperation(id string, now time.Time) (platform.Effect, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.businessStopped() {
		return platform.Effect{}, false
	}
	t.opsMu.Lock()
	i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == id })
	if i < 0 || t.outbound[i].sending || platform.EffectSettled(t.outbound[i].State) || t.outbound[i].Due.After(now) {
		t.opsMu.Unlock()
		return platform.Effect{}, false
	}
	before := t.outbound[i].Effect
	t.opsMu.Unlock()
	if t.overQuota(before.App, now) {
		return platform.Effect{}, false
	}
	works := t.forkWorkState()
	var prior json.RawMessage
	if work, err := works.Get(id); err == nil {
		prior, _ = protojson.Marshal(work)
		if work.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			if works.Cancel(id) != nil {
				return platform.Effect{}, false
			}
		}
	}
	generation, _, refusal := works.Start(id, "host-compute")
	if refusal != nil {
		return platform.Effect{}, false
	}
	work, _ := works.Get(id)
	rawWork, _ := protojson.Marshal(work)
	after := before
	after.Generation = generation
	r := acceptedformat.OperationClaim{Version: 1, Kind: "operation-claim", Tenant: t.ID, App: controlowner.PlatformApp, At: now.UTC(), Key: fmt.Sprintf("operation-claim:%s:%d", id, generation), Before: before, After: after, PriorWork: prior, Work: rawWork}
	r.RequestHash, _ = platform.CanonicalDigest([]any{before, prior})
	r.Digest, _ = acceptedformat.OperationClaimDigest(r)
	raw, _ := json.Marshal(r)
	principal, _ := json.Marshal(t.automation(controlowner.PlatformApp, false).Member)
	saved, err := t.acceptResult(Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: now}, r.Key, r.RequestHash)
	if err != nil {
		return platform.Effect{}, false
	}
	raw = saved
	if _, err := t.applyOperationClaim(raw); err != nil {
		t.quarantine(err)
		return platform.Effect{}, false
	}
	t.acceptedResultApplied()
	t.spend(before.App, now)
	return after, true
}
func (t *Tenant) operationCurrent(id string, generation uint32) bool {
	work, err := t.works.Get(id)
	if err != nil || work.GetGeneration() != generation || work.GetState() != pb.WorkState_WORK_STATE_RUNNING {
		return false
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	return slices.ContainsFunc(t.outbound, func(x *effect) bool {
		return x.ID == id && x.Generation == generation && !platform.EffectSettled(x.State)
	})
}
func (t *Tenant) operationDispatches(now time.Time) []func() {
	t.opsMu.Lock()
	ids := []string{}
	active := 0
	for _, x := range t.outbound {
		if x.Endpoint == acceptedformat.OperationEndpoint && x.sending {
			active++
		}
	}
	for _, x := range t.outbound {
		if x.Endpoint == acceptedformat.OperationEndpoint && !x.sending && !platform.EffectSettled(x.State) && !x.Due.After(now) && len(ids)+active < 4 {
			ids = append(ids, x.ID)
		}
	}
	t.opsMu.Unlock()
	var runs []func()
	for _, id := range ids {
		if x, ok := t.claimOperation(id, now); ok {
			runs = append(runs, func() { out := t.executeOperation(x, now); t.settleWithUsage(x.ID, out, nil, time.Now().UTC()) })
		}
	}
	return runs
}
func (t *Tenant) prepareOperationFinish(before platform.Effect, out platform.Outcome) (platform.Works, json.RawMessage, json.RawMessage, error) {
	if before.Endpoint != acceptedformat.OperationEndpoint {
		return nil, nil, nil, nil
	}
	if out.Generation != before.Generation || !t.operationCurrent(before.ID, out.Generation) {
		return nil, nil, nil, fmt.Errorf("stale operation completion")
	}
	works := t.forkWorkState()
	prior, _ := works.Get(before.ID)
	rawPrior, _ := protojson.Marshal(prior)
	if err := works.Finish(before.ID, out.Generation, out.Result != "delivered"); err != nil {
		return nil, nil, nil, err
	}
	finished, _ := works.Get(before.ID)
	rawWork, _ := protojson.Marshal(finished)
	return works, rawPrior, rawWork, nil
}
