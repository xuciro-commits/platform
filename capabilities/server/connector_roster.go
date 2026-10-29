package platformserver

import (
	"maps"
	"platformserver/platform"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type connectorRoster struct {
	kernel    platform.Connectors
	byID      map[string]*pb.ConnectorDescriptor
	lastError map[string]ConnectorError
}

func newConnectorRoster(provider platform.KernelFactory) connectorRoster {
	return connectorRoster{kernel: provider.Connectors(), byID: map[string]*pb.ConnectorDescriptor{}, lastError: map[string]ConnectorError{}}
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
