package actions

import (
	"encoding/json"
	"fmt"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type EffectSettlement struct {
	Tenant      string
	App         string
	Queued      func(id string) (Queued, bool)
	Usage       func(platform.AIUsage, platform.Effect) platform.AIUsage
	NoReviewer  func() bool
	Snapshot    func() (json.RawMessage, error)
	Finish      func(platform.Effect, platform.Outcome) (*kernel.Works, json.RawMessage, json.RawMessage, error)
	Failed      func(before acceptedformat.EffectState, err error)
	Stage       func(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect, out platform.Outcome, usage *platform.AIUsage) error
	Quarantined func() bool
	Reset       func()
	Principal   func() platform.Member
	Prepared    func(body, principal []byte, key, hash string)
}

func (s EffectSettlement) Settle(id string, out platform.Outcome, usage *platform.AIUsage, at time.Time) {
	pending, found := s.Queued(id)
	var before acceptedformat.EffectState
	if found {
		before = acceptedformat.EffectState{Effect: pending.Effect, Since: pending.Since}
	}
	if !found || out.Effect != id {
		return
	}
	if platform.EffectSettled(before.State) && before.State != "discarded" {
		return
	}
	if usage != nil {
		attributed := s.Usage(*usage, before.Effect)
		usage = &attributed
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (out.Result == "rejected" || out.Result == "unknown") && s.NoReviewer() {
		out.PublicMessage = "The model call stopped; no eligible reviewer is configured"
	}
	if out.Detail != "" && out.PublicMessage == "" {
		out.PublicMessage = "The last delivery attempt failed"
	}
	after := before.Effect
	acceptedformat.MarkEffect(&after, before.Since, out, at)
	result := acceptedformat.Effect{Version: 5, Kind: "effect-result", Tenant: s.Tenant, App: s.App,
		Key: acceptedformat.EffectResultKey(before), At: at.UTC(), Before: before,
		After: acceptedformat.EffectState{Effect: after, Since: before.Since}, Outcome: out, Usage: usage}
	var err error
	_, result.WorkBefore, result.Work, err = s.Finish(before.Effect, out)
	if err != nil {
		return
	}
	if usage != nil && s.Snapshot == nil {
		s.Failed(before, fmt.Errorf("model usage is not metered: the AI module is disabled"))
		usage, result.Usage = nil, nil
	}
	if usage != nil {
		state, snapshotErr := s.Snapshot()
		if snapshotErr == nil {
			result.UsageBefore, snapshotErr = platform.CanonicalDigest(state)
		}
		if snapshotErr != nil {
			s.Failed(before, snapshotErr)
			return
		}
	}
	if result.RequestHash, err = acceptedformat.EffectResultHash(before, out, result.At); err != nil {
		s.Failed(before, err)
		return
	}
	if err = s.Stage(&result, before, after, out, usage); err != nil {
		if s.Quarantined() {
			return
		}
		result.CallbackError, result.After.CallbackError = err.Error(), err.Error()
		result.Rows, result.Member, result.Changes, result.Unique = nil, "", nil, nil
		result.Notices, result.Intents, result.States, result.Control = nil, nil, nil, nil
		s.Reset()
	}
	result.Digest, err = acceptedformat.DigestEffect(result)
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(result)
	}
	if err == nil {
		_, err = acceptedformat.DecodeEffect(raw)
	}
	if err != nil {
		s.Failed(before, err)
		return
	}
	principal, _ := json.Marshal(s.Principal())
	s.Prepared(raw, principal, result.Key, result.RequestHash)
}
