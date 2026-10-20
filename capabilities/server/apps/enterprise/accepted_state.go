package enterprise

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
)

func (e *Enterprise) AcceptedLedger() *platform.Ledger { return e.ledger }

// The enterprise model includes nested tagged values, layouts, pins and
// calendars. Decode the owner's complete model so none of them alias live data.
func (e *Enterprise) ForkAcceptedState() (platform.App, error) {
	raw, err := e.AcceptedState()
	if err != nil {
		return nil, err
	}
	fork := &Enterprise{tenant: e.tenant, ledger: e.ledger}
	if err := fork.ApplyAcceptedState(raw); err != nil {
		return nil, err
	}
	return fork, nil
}

func (e *Enterprise) AcceptedState() (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.Marshal(e.model)
}

func (*Enterprise) ValidateAcceptedState(raw json.RawMessage) error {
	var model enterprisemodel.Model
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&model); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("accepted enterprise state has trailing data")
	}
	// Historical imported/federated models can contain external endpoints and
	// older stereotypes. Validate identity, preserving those accepted semantics.
	ids := map[string]bool{}
	for _, element := range model.Elements {
		if element.ID == "" || ids[element.ID] {
			return fmt.Errorf("accepted enterprise state has an invalid element")
		}
		ids[element.ID] = true
	}
	ids = map[string]bool{}
	for _, relationship := range model.Relationships {
		if relationship.ID == "" || ids[relationship.ID] {
			return fmt.Errorf("accepted enterprise state has an invalid relationship")
		}
		ids[relationship.ID] = true
	}
	ids = map[string]bool{}
	for _, view := range model.Views {
		if view.ID == "" || ids[view.ID] {
			return fmt.Errorf("accepted enterprise state has an invalid view")
		}
		ids[view.ID] = true
	}
	return nil
}

func (e *Enterprise) ApplyAcceptedState(raw json.RawMessage) error {
	if err := e.ValidateAcceptedState(raw); err != nil {
		return err
	}
	var model enterprisemodel.Model
	if err := json.Unmarshal(raw, &model); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.model = model
	return nil
}

var _ platform.AcceptedStateApp = (*Enterprise)(nil)
