package actions

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/kernel/acceptedformat"
)

func (e *Endpoints) InstallImage(raw json.RawMessage) error {
	var endpoints []*Endpoint
	if err := acceptedformat.DecodeControl(raw, &endpoints); err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, endpoint := range endpoints {
		if endpoint == nil || endpoint.ID == "" || ids[endpoint.ID] {
			return fmt.Errorf("control endpoint identity differs")
		}
		ids[endpoint.ID] = true
	}
	e.Restore(endpoints)
	return nil
}

func (o *Outbox) States() []acceptedformat.EffectState {
	effects := []acceptedformat.EffectState{}
	for _, e := range o.Select(nil) {
		effects = append(effects, acceptedformat.EffectState{Effect: e.Effect, Since: e.Since})
	}
	return effects
}

func (o *Outbox) InstallImage(raw json.RawMessage) error {
	var states []acceptedformat.EffectState
	if err := acceptedformat.DecodeControl(raw, &states); err != nil {
		return err
	}
	ids := map[string]bool{}
	effects := make([]Queued, 0, len(states))
	for _, state := range states {
		if state.ID == "" || ids[state.ID] || !slices.Contains([]string{"held", "pending", "retrying", "unknown", "compensated", "delivered", "failed", "rejected", "discarded"}, state.State) {
			return fmt.Errorf("control effect identity or state differs")
		}
		ids[state.ID] = true
		effects = append(effects, Queued{Effect: state.Effect, Since: state.Since})
	}
	o.Restore(effects)
	return nil
}

func (o *Outbox) PatchImage(raw json.RawMessage) error {
	var patches []acceptedformat.EffectState
	if err := acceptedformat.DecodeControl(raw, &patches); err != nil {
		return err
	}
	for _, patch := range patches {
		if !o.Update(patch.ID, func(x *Queued) { x.Effect, x.Since = patch.Effect, patch.Since }) {
			return fmt.Errorf("control effect disappeared")
		}
	}
	return nil
}

func InstallBindings(raw json.RawMessage, replace func() (rebind func(protocol, provider string) bool)) error {
	var bindings map[string]string
	if err := acceptedformat.DecodeControl(raw, &bindings); err != nil {
		return err
	}
	rebind := replace()
	for protocol, provider := range bindings {
		if !rebind(protocol, provider) {
			return fmt.Errorf("control protocol provider is missing")
		}
	}
	return nil
}
