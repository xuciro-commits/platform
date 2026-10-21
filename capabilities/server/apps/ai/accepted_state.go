package ai

import (
	"encoding/json"
	"maps"
	"slices"
	"time"

	"platformserver/internal/aistate"
	"platformserver/platform"
)

func (a *AI) AcceptedLedger() *platform.Ledger { return a.ledger }

// Configuration and metered usage are authoritative. Calls already in flight
// remain live execution state and are released by committed usage, never by
// restoring an older configuration image.
func (a *AI) ForkAcceptedState() (platform.App, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fork := &AI{ledger: a.ledger,
		providers: slices.Clone(a.providers), models: slices.Clone(a.models),
		limits: maps.Clone(a.limits), usage: slices.Clone(a.usage), daily: maps.Clone(a.daily),
		recent: cloneCalls(a.recent), inflight: cloneCalls(a.inflight)}
	return fork, nil
}

func cloneCalls(source map[string][]time.Time) map[string][]time.Time {
	out := make(map[string][]time.Time, len(source))
	for member, calls := range source {
		out[member] = slices.Clone(calls)
	}
	return out
}

func (a *AI) AcceptedState() (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return json.Marshal(aistate.State{Providers: a.providers, Models: a.models, Limits: a.limits, Usage: a.usage, Daily: a.daily})
}

func (*AI) ValidateAcceptedState(raw json.RawMessage) error {
	_, err := aistate.Decode(raw)
	return err
}

func (a *AI) ApplyAcceptedState(raw json.RawMessage) error {
	state, err := aistate.Decode(raw)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.providers, a.models, a.limits = state.Providers, state.Models, state.Limits
	a.usage, a.daily = state.Usage, state.Daily
	if a.limits == nil {
		a.limits = map[string]Limit{}
	}
	if a.daily == nil {
		a.daily = map[string]int{}
	}
	return nil
}

var _ platform.AcceptedStateApp = (*AI)(nil)
