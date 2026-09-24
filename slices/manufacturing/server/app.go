package mes

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

// Manifest declares the plant as the "mes" app (ADR-0010): its actions, its
// reads, and its connector inputs; batches and pages are journaled, heartbeats
// are not (health reads stale after a restart until the next one).
func (p *Plant) Manifest() platformserver.Manifest {
	return platformserver.Manifest{ID: "mes", Version: "1", Actions: p.ledger.Catalog,
		Reads:  []string{"master", "orders", "sfcs", "planned-orders", "downtime", "connectors"},
		Inputs: map[string]bool{"states": true, "planned-orders": true, "heartbeat": false}}
}

func (p *Plant) Read(_ platformserver.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "master":
		return p.Master(), nil
	case "orders":
		return p.Orders(), nil
	case "sfcs":
		return p.SFCs(), nil
	case "planned-orders":
		return p.Planned(), nil
	case "downtime":
		return p.Downtime(), nil
	}
	return p.Connectors(time.Now()), nil
}

func (p *Plant) Input(c platformserver.Caller, name string, body []byte, now time.Time) (any, *kernel.Error) {
	switch name {
	case "states":
		var b StateBatch
		if json.Unmarshal(body, &b) != nil {
			return nil, invalid
		}
		return p.DeliverStates(c, b, now)
	case "planned-orders":
		var page PlannedPage
		if json.Unmarshal(body, &page) != nil {
			return nil, invalid
		}
		return nil, p.DeliverPlanned(c, page, now)
	case "heartbeat":
		return nil, p.Heartbeat(c, now)
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}
