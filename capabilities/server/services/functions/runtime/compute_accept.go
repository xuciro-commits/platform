package compute

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type OperationClaims struct {
	Tenant     string
	App        string
	Queued     func(id string) (effect platform.Effect, sending bool, ok bool)
	Claimed    func(id string, after platform.Effect)
	Live       func(id string, generation uint32) bool
	Works      func() *kernel.Works
	Fork       func() *kernel.Works
	Install    func(*kernel.Works)
	Quarantine func(error)
	Applied    func()
}

func (c OperationClaims) Apply(raw []byte) (acceptedformat.OperationClaim, error) {
	saved, err := acceptedformat.DecodeOperationClaim(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != c.Tenant {
		return saved, fmt.Errorf("invalid operation ownership tenant")
	}
	current, _, ok := c.Queued(saved.Before.ID)
	if !ok {
		return saved, fmt.Errorf("operation claim has no accepted intent")
	}
	same, _ := platform.CanonicalDigest(current)
	expected, _ := platform.CanonicalDigest(saved.Before)
	if same != expected {
		return saved, fmt.Errorf("operation claim predecessor differs")
	}
	works := c.Fork()
	if err := ReplayClaim(works, saved.Before, saved.After, saved.Key, saved.PriorWork, saved.Work); err != nil {
		return saved, err
	}
	c.Install(works)
	c.Claimed(saved.Before.ID, saved.After)
	return saved, nil
}

func (c OperationClaims) Prepare(id string, now time.Time) (acceptedformat.OperationClaim, []byte, bool) {
	before, sending, ok := c.Queued(id)
	if !ok || sending || platform.EffectSettled(before.State) || before.Due.After(now) {
		return acceptedformat.OperationClaim{}, nil, false
	}
	generation, prior, rawWork, ok := StartClaim(c.Fork(), id)
	if !ok {
		return acceptedformat.OperationClaim{}, nil, false
	}
	after := before
	after.Generation = generation
	r := acceptedformat.OperationClaim{Version: 1, Kind: "operation-claim", Tenant: c.Tenant, App: c.App, At: now.UTC(), Key: fmt.Sprintf("operation-claim:%s:%d", id, generation), Before: before, After: after, PriorWork: prior, Work: rawWork}
	r.RequestHash, _ = platform.CanonicalDigest([]any{before, prior})
	r.Digest, _ = acceptedformat.OperationClaimDigest(r)
	raw, _ := json.Marshal(r)
	return r, raw, true
}

func (c OperationClaims) Settle(committed []byte) bool {
	if _, err := c.Apply(committed); err != nil {
		c.Quarantine(err)
		return false
	}
	c.Applied()
	return true
}

func (c OperationClaims) Revalidated(id string, o platform.Outcome, allowed func(platform.Effect) bool) platform.Outcome {
	if o.Generation == 0 || o.Result != "delivered" {
		return o
	}
	x, _, _ := c.Queued(id)
	if !allowed(x) {
		o.Result, o.Detail, o.Answer, o.Digest = "rejected", "Operation permission or source access was revoked", nil, ""
	}
	return o
}

func (c OperationClaims) Current(id string, generation uint32) bool {
	work, err := c.Works().Get(id)
	if err != nil || work.GetGeneration() != generation || work.GetState() != pb.WorkState_WORK_STATE_RUNNING {
		return false
	}
	return c.Live(id, generation)
}

func (c OperationClaims) PrepareFinish(before platform.Effect, out platform.Outcome) (*kernel.Works, json.RawMessage, json.RawMessage, error) {
	if before.Endpoint != acceptedformat.OperationEndpoint {
		return nil, nil, nil, nil
	}
	if out.Generation != before.Generation || !c.Current(before.ID, out.Generation) {
		return nil, nil, nil, fmt.Errorf("stale operation completion")
	}
	works := c.Fork()
	rawPrior, rawWork, err := FinishWork(works, before.ID, out.Generation, out.Result != "delivered")
	if err != nil {
		return nil, nil, nil, err
	}
	return works, rawPrior, rawWork, nil
}

type OperationCancellations struct {
	Tenant  string
	Effect  func(id string) (platform.Effect, bool)
	Discard func(id string, after platform.Effect)
	Works   func() *kernel.Works
	Fork    func() *kernel.Works
	Cancel  func(id string)
}

func (c OperationCancellations) One(id string) (acceptedformat.OperationCancellation, error) {
	x, ok := c.Effect(id)
	if !ok || x.Endpoint != acceptedformat.OperationEndpoint {
		return acceptedformat.OperationCancellation{}, fmt.Errorf("operation cancellation has no intent")
	}
	after := x
	after.State, after.Error = "discarded", "Cancelled"
	after.Output = nil
	r := acceptedformat.OperationCancellation{Before: x, After: after}
	var err error
	r.PriorWork, r.Work, err = CancelWork(c.Fork(), id)
	return r, err
}

func (c OperationCancellations) Saved(cancels map[string]bool) ([]acceptedformat.OperationCancellation, error) {
	ids := []string{}
	for id := range cancels {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := []acceptedformat.OperationCancellation{}
	for _, id := range ids {
		r, err := c.One(id)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}

func (c OperationCancellations) Validate(saved []acceptedformat.OperationCancellation) error {
	seen := map[string]bool{}
	for _, r := range saved {
		if seen[r.Before.ID] {
			return fmt.Errorf("duplicate operation cancellation")
		}
		seen[r.Before.ID] = true
		if r.Before.Endpoint != acceptedformat.OperationEndpoint || platform.EffectSettled(r.Before.State) || !strings.HasPrefix(r.Before.ID, c.Tenant+":") {
			return fmt.Errorf("invalid operation cancellation")
		}
		expected, err := c.One(r.Before.ID)
		if err != nil {
			return err
		}
		actualHash, _ := platform.CanonicalDigest(r)
		expectedHash, _ := platform.CanonicalDigest(expected)
		if actualHash != expectedHash {
			return fmt.Errorf("operation cancellation predecessor differs")
		}
	}
	return nil
}

func (c OperationCancellations) Apply(saved []acceptedformat.OperationCancellation) error {
	for _, r := range saved {
		if len(r.Work) > 0 {
			if err := RestoreWork(c.Works(), r.Work); err != nil {
				return err
			}
		}
		c.Discard(r.Before.ID, r.After)
		c.Cancel(r.Before.ID)
	}
	return nil
}
