package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

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
	return json.Marshal(aiState{a.providers, a.models, a.limits, a.usage, a.daily})
}

func (*AI) ValidateAcceptedState(raw json.RawMessage) error {
	var state aiState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("accepted AI state has trailing data")
	}
	providers, models := map[string]bool{}, map[string]bool{}
	for _, provider := range state.Providers {
		if provider.ID == "" || providers[provider.ID] {
			return fmt.Errorf("accepted AI state has an invalid provider")
		}
		providers[provider.ID] = true
	}
	for _, model := range state.Models {
		if !providers[model.Provider] || model.Model == "" || models[model.Name()] ||
			model.Access != "everyone" && model.Access != "users" || model.DailyTokens < 0 {
			return fmt.Errorf("accepted AI state has an invalid model")
		}
		models[model.Name()] = true
	}
	for member, limit := range state.Limits {
		if member == "" || member != limit.Member || limit.DailyTokens < 0 || limit.PerMinute < 0 {
			return fmt.Errorf("accepted AI state has an invalid limit")
		}
	}
	if len(state.Usage) > usageKept {
		return fmt.Errorf("accepted AI state exceeds retained usage")
	}
	return nil
}

func (a *AI) ApplyAcceptedState(raw json.RawMessage) error {
	if err := a.ValidateAcceptedState(raw); err != nil {
		return err
	}
	var state aiState
	if err := json.Unmarshal(raw, &state); err != nil {
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
