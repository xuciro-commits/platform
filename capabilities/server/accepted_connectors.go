package platformserver

import (
	"fmt"
	"time"

	"platformkernel/kernel"
)

// A connector's cursor and last-seen mark belong to the same durable result
// as the decisions caused by its delivery. Before guards both fields, including
// push deliveries (which have no cursor transition).
type acceptedConnectorDelivery struct {
	Connector string    `json:"connector"`
	DataClass string    `json:"dataClass"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	At        time.Time `json:"at"`
	Before    string    `json:"before"`
}

func markFor(marks []kernel.ConnectorMark, tenant, connector string) kernel.ConnectorMark {
	for _, mark := range marks {
		if mark.Tenant == tenant && mark.Connector == connector {
			return mark
		}
	}
	return kernel.ConnectorMark{}
}

func (t *Tenant) validateAcceptedDeliveries(deliveries []acceptedConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	descriptors, marks := t.connectors.State()
	private := kernel.NewConnectors()
	private.Restore(descriptors, marks)
	for _, delivery := range deliveries {
		if delivery.Connector == "" || delivery.DataClass == "" || delivery.At.IsZero() || delivery.Before == "" {
			return fmt.Errorf("accepted connector delivery is incomplete")
		}
		_, current := private.State()
		before, err := canonicalDigest(markFor(current, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor differs", delivery.Connector)
		}
		if err := private.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s delivery: %w", delivery.Connector, err)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedDeliveries(deliveries []acceptedConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, delivery := range deliveries {
		_, marks := t.connectors.State()
		before, err := canonicalDigest(markFor(marks, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor changed during application", delivery.Connector)
		}
		if err := t.connectors.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s changed during application: %w", delivery.Connector, err)
		}
	}
	return nil
}
