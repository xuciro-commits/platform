package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The async claim is an accepted-result of the existing K9 owner. The effect
// remains the sole queue item; this payload only freezes its current generation.
type acceptedOperationClaim struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	Key         string          `json:"key"`
	At          time.Time       `json:"at"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
	Before      platform.Effect `json:"before"`
	After       platform.Effect `json:"after"`
	PriorWork   json.RawMessage `json:"priorWork,omitempty"`
	Work        json.RawMessage `json:"work"`
}

func operationClaimDigest(r acceptedOperationClaim) (string, error) {
	r.Digest = ""
	return canonicalDigest(r)
}

// Decode before journal admission without applying the K9 transition.
func decodeOperationClaim(raw []byte) (acceptedOperationClaim, error) {
	var saved acceptedOperationClaim
	if _, err := platform.DecodeValue(raw, maxAcceptedResultBytes); err != nil {
		return saved, err
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved, err
	}
	digest, err := operationClaimDigest(saved)
	hash, _ := canonicalDigest([]any{saved.Before, saved.PriorWork})
	if err != nil || saved.Kind != "operation-claim" || saved.Version != 1 || saved.Tenant == "" || saved.App != PlatformApp || saved.RequestHash != hash || saved.Digest != digest || saved.Before.Endpoint != operationEndpoint || saved.Before.ID == "" || settled(saved.Before.State) || saved.At.IsZero() {
		return saved, fmt.Errorf("invalid operation ownership result")
	}
	if saved.After.Generation == 0 || saved.Key != fmt.Sprintf("operation-claim:%s:%d", saved.Before.ID, saved.After.Generation) {
		return saved, fmt.Errorf("operation claim identity differs")
	}
	return saved, nil
}

func (t *Tenant) applyOperationClaim(raw []byte) (acceptedOperationClaim, error) {
	saved, err := decodeOperationClaim(raw)
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
	same, _ := canonicalDigest(current.Effect)
	expected, _ := canonicalDigest(saved.Before)
	t.opsMu.Unlock()
	if same != expected {
		return saved, fmt.Errorf("operation claim predecessor differs")
	}
	works := t.forkWorkState()
	var prior json.RawMessage
	if old, err := works.Get(saved.Before.ID); err == nil {
		prior, _ = protojson.Marshal(old)
	}
	priorHash, _ := canonicalDigest(prior)
	savedHash, _ := canonicalDigest(saved.PriorWork)
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
	actual, _ := canonicalDigest(after)
	frozen, _ := canonicalDigest(saved.After)
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
	if t.quarantined() {
		return platform.Effect{}, false
	}
	t.opsMu.Lock()
	i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == id })
	if i < 0 || t.outbound[i].sending || settled(t.outbound[i].State) || t.outbound[i].Due.After(now) {
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
	r := acceptedOperationClaim{Version: 1, Kind: "operation-claim", Tenant: t.ID, App: PlatformApp, At: now.UTC(), Key: fmt.Sprintf("operation-claim:%s:%d", id, generation), Before: before, After: after, PriorWork: prior, Work: rawWork}
	r.RequestHash, _ = canonicalDigest([]any{before, prior})
	r.Digest, _ = operationClaimDigest(r)
	raw, _ := json.Marshal(r)
	principal, _ := json.Marshal(t.automation(PlatformApp, false).Member)
	if t.AcceptResult != nil {
		saved, err := t.AcceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: now}, r.Key, r.RequestHash)
		if err != nil {
			return platform.Effect{}, false
		}
		raw = saved
	} else {
		if app := t.app(PlatformApp); app != nil {
			t.record(app, "accepted-result", t.automation(PlatformApp, false).Member, raw, now)
		}
	}
	if _, err := t.applyOperationClaim(raw); err != nil {
		t.quarantine(err)
		return platform.Effect{}, false
	}
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
	return slices.ContainsFunc(t.outbound, func(x *effect) bool { return x.ID == id && x.Generation == generation && !settled(x.State) })
}
func (t *Tenant) operationDispatches(now time.Time) []func() {
	t.opsMu.Lock()
	ids := []string{}
	active := 0
	for _, x := range t.outbound {
		if x.Endpoint == operationEndpoint && x.sending {
			active++
		}
	}
	for _, x := range t.outbound {
		if x.Endpoint == operationEndpoint && !x.sending && !settled(x.State) && !x.Due.After(now) && len(ids)+active < 4 {
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
func (t *Tenant) prepareOperationFinish(before platform.Effect, out platform.Outcome) (*kernel.Works, json.RawMessage, json.RawMessage, error) {
	if before.Endpoint != operationEndpoint {
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
