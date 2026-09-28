package mes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (*Plant) AcceptedInputs() []string { return []string{"states"} }

func (*Plant) DecodeAcceptedInput(raw json.RawMessage) (any, error) {
	var record pb.FactRecord
	err := protojson.Unmarshal(raw, &record)
	if err == nil && (record.GetFactId() == "" || record.GetFact() == nil) {
		err = fmt.Errorf("accepted equipment observation has no fact")
	}
	return &record, err
}

// The equipment fact log, opaque downtime identities and derived downtime
// belong to the plant, not the host's record store. Clone only these owners:
// the staged caller owns connector marks and notifications separately.
func (p *Plant) ForkAcceptedState() (platform.App, error) {
	raw, err := p.AcceptedState()
	if err != nil {
		return nil, err
	}
	fork := New(p.tenant, p.master)
	fork.ledger = p.ledger // one ledger identity; the input does not submit a K4 decision
	if err := fork.ApplyAcceptedState(raw); err != nil {
		return nil, err
	}
	return fork, nil
}

func (p *Plant) AcceptedState() (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	facts, err := platform.SnapshotFacts(p.facts, p.tenant)
	if err != nil {
		return nil, err
	}
	return json.Marshal(plantState{Facts: facts, Identity: p.identity.State(),
		Downtime: p.downtime, NextEvent: p.nextEvent})
}

func (p *Plant) ValidateAcceptedState(raw json.RawMessage) error {
	var state plantState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("equipment state has trailing data")
	}
	facts, err := platform.Unprotos[*pb.FactRecord](state.Facts)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, record := range facts {
		if record.GetFactId() == "" || ids[record.GetFactId()] ||
			record.GetFact().GetTenantId() != p.tenant ||
			record.GetFact().GetSchema().GetName() != schemaStates ||
			record.GetRecordedTime() == nil {
			return fmt.Errorf("equipment state contains an invalid fact")
		}
		ids[record.GetFactId()] = true
	}
	if state.NextEvent < 0 {
		return fmt.Errorf("equipment state has an invalid event generation")
	}
	events := map[string]bool{}
	for resource, list := range state.Downtime {
		for _, item := range list {
			if resource == "" || item.Resource != resource || item.ID == "" ||
				resourceOfEvent(item.ID) != resource || item.Start.IsZero() || events[item.ID] {
				return fmt.Errorf("equipment state contains an invalid downtime event")
			}
			events[item.ID] = true
		}
	}
	return nil
}

func (p *Plant) ApplyAcceptedState(raw json.RawMessage) error {
	if err := p.ValidateAcceptedState(raw); err != nil {
		return err
	}
	var state plantState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := platform.RestoreFacts(p.facts, p.tenant, state.Facts); err != nil {
		return err
	}
	p.identity.Restore(state.Identity)
	p.downtime, p.nextEvent = state.Downtime, state.NextEvent
	if p.downtime == nil {
		p.downtime = map[string][]Downtime{}
	}
	return nil
}
