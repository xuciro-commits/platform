package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/platform"
)

type acceptedState struct {
	App    string          `json:"app"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

func (d *stagedDecision) stateApp(app platform.App) platform.App {
	owner, ok := app.(platform.AcceptedStateApp)
	if !ok {
		return app
	}
	id := app.Manifest().ID
	if existing := d.states[id]; existing != nil {
		return existing
	}
	before, err := owner.AcceptedState()
	if err != nil {
		unsupportedStagedEffect()
	}
	fork, err := owner.ForkAcceptedState()
	copy, ok := fork.(platform.AcceptedStateApp)
	if err != nil || !ok || fork.Manifest().ID != id {
		unsupportedStagedEffect()
	}
	originalLedger, originalOK := app.(platform.ResultApp)
	privateLedger, privateOK := fork.(platform.ResultApp)
	if !originalOK || !privateOK || originalLedger.AcceptedLedger() != privateLedger.AcceptedLedger() {
		unsupportedStagedEffect()
	}
	if d.states == nil {
		d.states = map[string]platform.AcceptedStateApp{}
		d.stateBases = map[string]json.RawMessage{}
	}
	d.states[id], d.stateBases[id] = copy, slices.Clone(before)
	return fork
}

func (d *stagedDecision) savedStates() ([]acceptedState, error) {
	var out []acceptedState
	for _, id := range slices.Sorted(maps.Keys(d.states)) {
		owner := d.states[id]
		image, err := owner.AcceptedState()
		if err != nil {
			return nil, err
		}
		if err := owner.ValidateAcceptedState(image); err != nil {
			return nil, err
		}
		before, err := canonicalDigest(d.stateBases[id])
		if err != nil {
			return nil, err
		}
		after, err := canonicalDigest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			out = append(out, acceptedState{App: id, Before: before, Image: image})
		}
	}
	return out, nil
}

func (t *Tenant) validateAcceptedStates(states []acceptedState) error {
	seen := map[string]bool{}
	for _, state := range states {
		owner, ok := t.app(state.App).(platform.AcceptedStateApp)
		if !ok || seen[state.App] || state.Before == "" || len(state.Image) == 0 {
			return fmt.Errorf("accepted result has a duplicate or unknown state owner %s", state.App)
		}
		seen[state.App] = true
		if err := owner.ValidateAcceptedState(state.Image); err != nil {
			return err
		}
		prior, err := owner.AcceptedState()
		if err != nil {
			return err
		}
		hash, err := canonicalDigest(prior)
		if err != nil || hash != state.Before && !t.legacyConsolePredecessor(state, prior) {
			return fmt.Errorf("accepted state predecessor differs for %s", state.App)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedStates(states []acceptedState) error {
	for _, state := range states {
		if err := t.app(state.App).(platform.AcceptedStateApp).ApplyAcceptedState(state.Image); err != nil {
			return err
		}
	}
	return nil
}
