package kernel

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"

	"google.golang.org/protobuf/proto"
)

type ConnectorView struct {
	ID          string          `json:"id"`
	Direction   string          `json:"direction"`
	DataClasses []string        `json:"dataClasses"`
	Heartbeat   string          `json:"heartbeat"`
	Health      string          `json:"health"`
	LastSeen    *time.Time      `json:"lastSeen,omitempty"`
	Disabled    bool            `json:"disabled"`
	LastError   *ConnectorError `json:"lastError,omitempty"`
}

type ConnectorError struct {
	At    time.Time `json:"at"`
	Input string    `json:"input"`
	Error string    `json:"error"`
}

type ConnectorRoster struct {
	mu        sync.Mutex
	tenant    string
	kernel    *contract.Connectors
	byID      map[string]*pb.ConnectorDescriptor
	lastError map[string]ConnectorError
}

func NewConnectorRoster(tenant string) *ConnectorRoster {
	return &ConnectorRoster{tenant: tenant, kernel: contract.NewConnectors(), byID: map[string]*pb.ConnectorDescriptor{}, lastError: map[string]ConnectorError{}}
}

func (r *ConnectorRoster) Register(descriptors ...*pb.ConnectorDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range descriptors {
		d = proto.Clone(d).(*pb.ConnectorDescriptor)
		d.TenantId = r.tenant
		if err := r.kernel.Register(d); err != nil {
			return fmt.Errorf("tenant %s: connector %s: %v", r.tenant, d.GetConnectorId(), err)
		}
		r.byID[d.GetConnectorId()] = d
	}
	return nil
}

func (r *ConnectorRoster) Refused(member, input string, err error, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID[member] != nil {
		r.lastError[member] = ConnectorError{At: now, Input: input, Error: err.Error()}
	}
}

func (r *ConnectorRoster) Views(now time.Time) []ConnectorView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ConnectorView{}
	for _, d := range r.kernel.Descriptors(r.tenant) {
		s, _ := r.kernel.Status(r.tenant, d.GetConnectorId(), now)
		v := ConnectorView{ID: d.GetConnectorId(), Direction: strings.ToLower(strings.TrimPrefix(d.GetDirection().String(), "CONNECTOR_DIRECTION_")),
			DataClasses: d.GetDataClasses(), Heartbeat: d.GetHeartbeat().AsDuration().String(), Disabled: d.GetDisabled(),
			Health: strings.ToLower(strings.TrimPrefix(s.GetHealth().String(), "CONNECTOR_HEALTH_"))}
		if s.GetLastSeen() != nil {
			seen := s.GetLastSeen().AsTime()
			v.LastSeen = &seen
		}
		if e, ok := r.lastError[d.GetConnectorId()]; ok {
			v.LastError = &e
		}
		out = append(out, v)
	}
	return out
}

func (r *ConnectorRoster) Granted(member, app, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	connector := r.byID[member]
	return connector != nil && connector.GetTenantId() == r.tenant && (name == "heartbeat" || slices.ContainsFunc(connector.GetDataClasses(), func(class string) bool { return strings.HasPrefix(class, app+".") }))
}

func (r *ConnectorRoster) Known(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id] != nil
}

func (r *ConnectorRoster) SetDisabled(id string, off bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.byID[id]; d != nil {
		d.Disabled = off
	}
}

func (r *ConnectorRoster) Heartbeat(tenant, id string, now time.Time) *contract.Error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kernel.Heartbeat(tenant, id, now)
}

func (r *ConnectorRoster) State() ([]*pb.ConnectorDescriptor, []contract.ConnectorMark) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kernel.State()
}

func (r *ConnectorRoster) LastErrors() map[string]ConnectorError {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.lastError)
}

func (r *ConnectorRoster) Clone() *contract.Connectors {
	r.mu.Lock()
	defer r.mu.Unlock()
	return CloneConnectors(r.kernel)
}

func (r *ConnectorRoster) Fork() *ConnectorRoster {
	out := NewConnectorRoster(r.tenant)
	out.kernel = r.Clone()
	return out
}

func (r *ConnectorRoster) RestoreMarks(marks []contract.ConnectorMark) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kernel.Restore(nil, marks)
}

func (r *ConnectorRoster) Install(descriptors []*pb.ConnectorDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, marks := r.kernel.State()
	byID := map[string]*pb.ConnectorDescriptor{}
	for _, desc := range descriptors {
		if desc.GetTenantId() != r.tenant || byID[desc.GetConnectorId()] != nil {
			return fmt.Errorf("control connector identity differs")
		}
		byID[desc.GetConnectorId()] = desc
	}
	r.kernel = contract.NewConnectors()
	r.kernel.Restore(descriptors, marks)
	r.byID = byID
	return nil
}

func (r *ConnectorRoster) Use(c *contract.Connectors) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kernel = c
	r.byID = map[string]*pb.ConnectorDescriptor{}
	descriptors, _ := c.State()
	for _, descriptor := range descriptors {
		r.byID[descriptor.GetConnectorId()] = descriptor
	}
}

func (r *ConnectorRoster) Restore(descriptors []*pb.ConnectorDescriptor, marks []contract.ConnectorMark, lastError map[string]ConnectorError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kernel.Restore(descriptors, marks)
	r.byID = map[string]*pb.ConnectorDescriptor{}
	for _, d := range descriptors {
		if d.GetTenantId() == r.tenant {
			r.byID[d.GetConnectorId()] = d
		}
	}
	r.lastError = lastError
	if r.lastError == nil {
		r.lastError = map[string]ConnectorError{}
	}
}
