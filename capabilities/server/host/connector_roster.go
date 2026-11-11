package host

import (
	"maps"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type connectorRoster struct {
	kernel    *kernel.Connectors
	byID      map[string]*pb.ConnectorDescriptor
	lastError map[string]ConnectorError
}

func newConnectorRoster() connectorRoster {
	return connectorRoster{kernel: kernel.NewConnectors(), byID: map[string]*pb.ConnectorDescriptor{}, lastError: map[string]ConnectorError{}}
}

func (r *connectorRoster) register(d *pb.ConnectorDescriptor) error {
	if err := r.kernel.Register(d); err != nil {
		return err
	}
	r.byID[d.GetConnectorId()] = d
	return nil
}

func (r *connectorRoster) refused(member, input string, err error, now time.Time) {
	if r.byID[member] != nil {
		r.lastError[member] = ConnectorError{At: now, Input: input, Error: err.Error()}
	}
}

func (r *connectorRoster) snapshot(s *tenantState) {
	s.LastError = maps.Clone(r.lastError)
}

func (r *connectorRoster) restore(tenant string, descriptors []*pb.ConnectorDescriptor, s *tenantState) {
	r.kernel.Restore(descriptors, s.Marks)
	r.byID = map[string]*pb.ConnectorDescriptor{}
	for _, d := range descriptors {
		if d.GetTenantId() == tenant {
			r.byID[d.GetConnectorId()] = d
		}
	}
	r.lastError = s.LastError
	if r.lastError == nil {
		r.lastError = map[string]ConnectorError{}
	}
}
