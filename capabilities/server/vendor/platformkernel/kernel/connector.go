package kernel

import (
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Connectors implements K8 (contract/spec/K8-connectors.md) for one receiver.
type Connectors struct {
	descriptors map[[2]string]*pb.ConnectorDescriptor // (tenant, connector)
	cursors     map[[2]string]string
	lastSeen    map[[2]string]time.Time
}

func NewConnectors() *Connectors {
	return &Connectors{descriptors: map[[2]string]*pb.ConnectorDescriptor{}, cursors: map[[2]string]string{}, lastSeen: map[[2]string]time.Time{}}
}

func (c *Connectors) Register(d *pb.ConnectorDescriptor) *Error {
	if d.GetTenantId() == "" || d.GetConnectorId() == "" || len(d.GetDataClasses()) == 0 ||
		d.GetDirection() == pb.ConnectorDirection_CONNECTOR_DIRECTION_UNSPECIFIED {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // N1
	}
	key := [2]string{d.GetTenantId(), d.GetConnectorId()}
	if c.descriptors[key] != nil {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	c.descriptors[key] = d
	return nil
}

// Deliver accepts one batch about dataClass; poll pages move the cursor from→to (N2, N3).
func (c *Connectors) Deliver(tenant, connector, dataClass, cursorFrom, cursorTo string, now time.Time) *Error {
	key := [2]string{tenant, connector}
	d := c.descriptors[key]
	if d == nil {
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	if d.GetDisabled() || !slices.Contains(d.GetDataClasses(), dataClass) {
		return errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	}
	if d.GetDirection() == pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH {
		if cursorFrom != "" || cursorTo != "" {
			return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
	} else {
		if cursorFrom != c.cursors[key] {
			return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
		c.cursors[key] = cursorTo
	}
	c.lastSeen[key] = now
	return nil
}

func (c *Connectors) Heartbeat(tenant, connector string, now time.Time) *Error {
	key := [2]string{tenant, connector}
	if c.descriptors[key] == nil {
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	c.lastSeen[key] = now
	return nil
}

// Status reports health at now (N4).
func (c *Connectors) Status(tenant, connector string, now time.Time) (*pb.ConnectorStatus, *Error) {
	key := [2]string{tenant, connector}
	d := c.descriptors[key]
	if d == nil {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	s := &pb.ConnectorStatus{ConnectorId: connector, Cursor: c.cursors[key], Health: pb.ConnectorHealth_CONNECTOR_HEALTH_OK}
	seen, ok := c.lastSeen[key]
	if ok {
		s.LastSeen = timestamppb.New(seen)
	}
	switch {
	case d.GetDisabled():
		s.Health = pb.ConnectorHealth_CONNECTOR_HEALTH_DISABLED
	case !ok || now.Sub(seen) > d.GetHeartbeat().AsDuration():
		s.Health = pb.ConnectorHealth_CONNECTOR_HEALTH_STALE
	}
	return s, nil
}

// Descriptors lists a tenant's connectors.
func (c *Connectors) Descriptors(tenant string) []*pb.ConnectorDescriptor {
	var out []*pb.ConnectorDescriptor
	for key, d := range c.descriptors {
		if key[0] == tenant {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b *pb.ConnectorDescriptor) int {
		if a.GetConnectorId() < b.GetConnectorId() {
			return -1
		}
		return 1
	})
	return out
}
