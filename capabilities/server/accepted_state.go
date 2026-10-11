package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/internal/host"
	"platformserver/platform"
)

type acceptedState struct {
	App    string          `json:"app"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

func (d *stagedDecision) stateApp(app platform.App) platform.App {
	id := app.Manifest().ID
	if existing := d.privateApps[id]; existing != nil {
		return existing
	}
	var fork platform.App
	var before json.RawMessage
	var err error
	owner, ownsState := app.(platform.AcceptedStateApp)
	if ownsState {
		before, err = owner.AcceptedState()
		if err == nil {
			fork, err = owner.ForkAcceptedState()
		}
	} else if execution, ok := app.(platform.DecisionForkApp); ok {
		fork, err = execution.ForkDecision()
	} else {
		return app
	}
	if err != nil || fork == nil || fork.Manifest().ID != id || fork.AcceptedLedger() != app.AcceptedLedger() {
		unsupportedStagedEffect()
	}
	if d.privateApps == nil {
		d.privateApps = map[string]platform.App{}
	}
	d.privateApps[id] = fork
	host.Attach(fork, hostView{t: d.tenant, app: fork, decision: d})
	if ownsState {
		copy, ok := fork.(platform.AcceptedStateApp)
		if !ok {
			unsupportedStagedEffect()
		}
		if d.states == nil {
			d.states = map[string]platform.AcceptedStateApp{}
			d.stateBases = map[string]json.RawMessage{}
		}
		d.states[id], d.stateBases[id] = copy, slices.Clone(before)
	}
	return fork
}

func (d *stagedDecision) attachPrivateHosts() {
	for _, app := range d.privateApps {
		host.Attach(app, hostView{t: d.tenant, app: app, decision: d})
	}
}

// Version choices belong to the private Flow instance that actually decided.
// They are saved with the result before the live registry is changed.
func (d *stagedDecision) flowVersions() map[string]int {
	if private, ok := d.privateApps[platform.FlowApp].(host.Processes); ok {
		return private.Versions()
	}
	return nil
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
		if err != nil || hash != state.Before && !t.legacyConsolePredecessor(state, prior) && !t.legacyCoreSeedPredecessor(state, prior) {
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
