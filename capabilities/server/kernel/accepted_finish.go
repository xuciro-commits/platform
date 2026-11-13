package kernel

import (
	"fmt"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type EffectFinish struct {
	Settlement
	Commit   func() ([]byte, error)
	Conflict func(error) bool
	Release  func()
	Apply    func([]byte) (acceptedformat.Effect, bool, error)
}

func (h EffectFinish) Finish(id, key, hash string) {
	committed, err := h.Commit()
	if err != nil {
		if h.Conflict(err) {
			h.Quarantine(fmt.Errorf("effect %s conflicts with a previously committed outcome", id))
			return
		}
		h.Release()
		return
	}
	saved, applied, err := h.Apply(committed)
	if err != nil || saved.Key != key || saved.RequestHash != hash || !applied {
		h.Quarantine(fmt.Errorf("apply committed effect %s: %v (applied=%t)", id, err, applied))
		return
	}
	h.Settled(saved.At, saved.App, len(saved.Changes) != 0 || len(saved.Rows) != 0 || len(saved.States) != 0 || saved.Notices != nil)
}

type AgentFinish struct {
	Settlement
	Commit func() ([]byte, error)
	Apply  func([]byte) error
}

func (h AgentFinish) Finish(key, hash string) {
	committed, err := h.Commit()
	if err != nil {
		return
	}
	saved, err := acceptedformat.DecodeAgent(committed)
	if err != nil || saved.Key != key || saved.RequestHash != hash {
		h.Quarantine(fmt.Errorf("committed agent answer differs: %v", err))
		return
	}
	if err = h.Apply(committed); err != nil {
		h.Quarantine(err)
		return
	}
	h.Settled(saved.At, platform.AgentApp, true)
}
