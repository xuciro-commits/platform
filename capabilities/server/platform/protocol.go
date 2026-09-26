package platform

import (
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Protocol is a named, versioned interface apps provide and consume (ADR-0011):
// actions (payload fields and meaning), reads (result shape, by convention the
// protocol package's types) and events. Consumers depend on a protocol, never on
// the app that provides it.
type Protocol struct {
	Name    string
	Version int
	Actions []Action // Schema is the protocol action's short name; Roles stay the provider's
	Reads   []string
	Events  []ProtocolEvent
}

type ProtocolEvent struct {
	Name  string `json:"name"`
	Title string `json:"title"` // how the timeline tells it, e.g. "Booking canceled"
}

// ID is "<name>/<version>", e.g. "lodging.booking/1".
func (p Protocol) ID() string { return fmt.Sprintf("%s/%d", p.Name, p.Version) }

// Provision maps a protocol onto the provider's own actions, reads and events
// (each keyed by the protocol's name, valued by the provider's).
type Provision struct {
	Protocol Protocol
	Actions  map[string]string // protocol action → provider action schema (same payload)
	Reads    map[string]string // protocol read → provider read (returning the protocol's types)
	Events   map[string]string // protocol event → provider action schema whose decisions are that event
}

// Consumption declares a protocol an app uses; an optional one may have no provider.
type Consumption struct {
	Protocol string // ID
	Optional bool
}

// ProtocolAction names a protocol action or event: "<protocol id>#<name>".
func ProtocolAction(protocol, action string) string { return protocol + "#" + action }

// ProviderResult is one provider's result of a protocol read, with the type of
// the entities it holds, so consumers match their links exactly.
type ProviderResult struct {
	Provider string
	Type     string // e.g. "pms.reservation"
	Result   any
}

// Invoke calls a protocol action on the tenant's provider, as the member with its
// role there; id names the new or existing entity. It returns the entity the
// provider acted on. Only a consumer of the protocol may invoke it.
func (c Caller) Invoke(protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	if c.rt == nil {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Invoke(c, protocol, action, id, payload, key, correlation, now)
}

// Query reads a protocol read from every provider of the tenant, the bound one
// first: switching the binding sends new calls elsewhere, but what the other
// providers hold stays visible (#99). Only a consumer of the protocol may query it.
func (c Caller) Query(protocol, read string) ([]ProviderResult, *kernel.Error) {
	if c.rt == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Query(c, protocol, read)
}
