package platformserver

import (
	"fmt"
	"slices"
	"strings"
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

// ProtocolAction names a protocol action in Action.Uses: "<protocol id>#<action>".
func ProtocolAction(protocol, action string) string { return protocol + "#" + action }

type binding struct {
	provider  App
	provision Provision
}

// bind resolves consumed protocols to providers enabled before the consumer.
func (t *Tenant) bind(i int, a App) error {
	for _, c := range a.Manifest().Consumes {
		var found []binding
		for _, p := range t.apps[:i] {
			for _, pv := range p.Manifest().Provides {
				if pv.Protocol.ID() == c.Protocol {
					found = append(found, binding{p, pv})
				}
			}
		}
		switch {
		case len(found) == 0 && !c.Optional:
			return fmt.Errorf("tenant %s: %s consumes %s, which no app before it provides", t.ID, a.Manifest().ID, c.Protocol)
		case len(found) > 0:
			if b, ok := t.bindings[c.Protocol]; ok && b.provider != found[0].provider {
				continue // bound for an earlier consumer; one provider per protocol per tenant
			}
			t.bindings[c.Protocol] = found[0] // the first provider, until an administrator chooses another
		}
	}
	return nil
}

// bound is the provider the tenant binds protocol to; an administrator may
// choose another (platform.protocol.bind), so it is read under opsMu.
func (t *Tenant) bound(protocol string) (binding, bool) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	b, ok := t.bindings[protocol]
	return b, ok
}

// providers are every enabled provision of protocol, in the tenant's app order.
func (t *Tenant) providers(protocol string) []binding {
	var out []binding
	for _, a := range t.apps {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				out = append(out, binding{a, pv})
			}
		}
	}
	return out
}

// resolve is the tenant's provider of a protocol: the bound one, or the first
// provider when no consumer has bound it yet.
func (t *Tenant) resolve(protocol string) (binding, bool) {
	if b, ok := t.bound(protocol); ok {
		return b, true
	}
	for _, a := range t.apps {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				return binding{a, pv}, true
			}
		}
	}
	return binding{}, false
}

// provider resolves "<protocol id>#<action>" to the provider and its action schema.
func (t *Tenant) provider(used string) (App, string, bool) {
	protocol, action, ok := strings.Cut(used, "#")
	if !ok {
		return nil, "", false
	}
	b, bound := t.resolve(protocol)
	if !bound {
		return nil, "", false
	}
	schema, mapped := b.provision.Actions[action]
	return b.provider, schema, mapped
}

func (c Caller) consumes(protocol string) bool {
	self := c.tenant.app(c.App)
	return self != nil && slices.ContainsFunc(self.Manifest().Consumes, func(x Consumption) bool { return x.Protocol == protocol })
}

// Invoke calls a protocol action on the tenant's provider, as the member with its
// role there; id names the new or existing entity. It returns the entity the
// provider acted on. Only a consumer of the protocol may invoke it.
func (c Caller) Invoke(protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	if c.tenant == nil || !c.consumes(protocol) {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.tenant.invoke(c, protocol, action, id, payload, key, correlation, now)
}

func (t *Tenant) invoke(c Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	provider, schema, ok := t.provider(ProtocolAction(protocol, action))
	if !ok {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} // no provider bound
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	target := &pb.EntityRef{Type: declared.Target, Id: id}
	called := t.caller(c.Member, provider, c.Replaying)
	called.Automation = c.Automation
	record, err := provider.Submit(called, &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: t.authorityOf(declared.Target),
		Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: payload}, now)
	return target, record, err
}

// Answer is one provider's result of a protocol read, with the type of the
// entities it holds, so consumers match their links exactly.
type Answer struct {
	Provider string
	Type     string // e.g. "hotel.reservation"
	Result   any
}

// Query reads a protocol read from every provider of the tenant, the bound one
// first: switching the binding sends new calls elsewhere, but what the other
// providers hold stays visible (#99). None when no app provides it.
func (c Caller) Query(protocol, read string) ([]Answer, *kernel.Error) {
	if c.tenant == nil || !c.consumes(protocol) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	all := c.tenant.providers(protocol)
	if b, ok := c.tenant.bound(protocol); ok {
		first := func(x binding) int { return map[bool]int{true: 0, false: 1}[x.provider == b.provider] }
		slices.SortStableFunc(all, func(x, y binding) int { return first(x) - first(y) })
	}
	var out []Answer
	for _, b := range all {
		called := c.tenant.caller(c.Member, b.provider, c.Replaying)
		called.Automation = c.Automation
		result, err := b.provider.Read(called, b.provision.Reads[read])
		if err != nil {
			return nil, err
		}
		out = append(out, Answer{Provider: b.provider.Manifest().ID, Type: b.entityType(), Result: result})
	}
	return out, nil
}

// entityType is the type of the entities the provider's protocol actions act on.
func (b binding) entityType() string {
	for _, schema := range b.provision.Actions {
		if a, ok := b.provider.Manifest().Actions.Action(schema); ok {
			return a.Target
		}
	}
	return ""
}

// Bound reports whether the tenant has a provider for protocol.
func (c Caller) Bound(protocol string) bool {
	if c.tenant == nil {
		return false
	}
	_, ok := c.tenant.bound(protocol)
	return ok
}

// rebind makes provider the tenant's provider of protocol (a platform decision).
func (t *Tenant) rebind(protocol, provider string) bool {
	for _, b := range t.providers(protocol) {
		if b.provider.Manifest().ID == provider {
			t.opsMu.Lock()
			t.bindings[protocol] = b
			t.opsMu.Unlock()
			return true
		}
	}
	return false
}

// Invoke calls a protocol action as a member (HTTP, MCP, conformance tests),
// through the tenant's provider; it is journaled as the provider's submission.
func (t *Tenant) Invoke(m Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	provider, schema, ok := t.provider(ProtocolAction(protocol, action))
	if !ok {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	target := &pb.EntityRef{Type: declared.Target, Id: id}
	record, err := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: t.authorityOf(declared.Target),
		Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return target, record, err
}

// Query reads a protocol read as a member holding a role in the bound provider.
func (t *Tenant) Query(m Member, protocol, read string) (any, *kernel.Error) {
	b, bound := t.resolve(protocol)
	if !bound {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if m.Roles[b.provider.Manifest().ID] == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return b.provider.Read(t.caller(m, b.provider, false), b.provision.Reads[read])
}

func (t *Tenant) authorityOf(dataClass string) string {
	for _, d := range t.Declarations() {
		if d.GetDataClass() == dataClass {
			return d.GetAuthorityId()
		}
	}
	return ""
}

// protocolEvents are the protocol events a provider's accepted decision is,
// whether or not anyone consumes them.
func (t *Tenant) protocolEvents(e Event) []string {
	var out []string
	if a := t.app(e.App); a != nil {
		for _, pv := range a.Manifest().Provides {
			for event, schema := range pv.Events {
				if schema == e.Record.GetSubmission().GetSchema().GetName() {
					out = append(out, ProtocolAction(pv.Protocol.ID(), event))
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// protocolEvent finds a protocol event's declaration by "<protocol id>#<event>".
func (t *Tenant) protocolEvent(name string) (ProtocolEvent, bool) {
	protocol, event, _ := strings.Cut(name, "#")
	for _, a := range t.apps {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				for _, e := range pv.Protocol.Events {
					if e.Name == event {
						return e, true
					}
				}
			}
		}
	}
	return ProtocolEvent{}, false
}

// ProtocolInfo describes a protocol in a tenant: who provides it, who consumes it, which provider is bound.
type ProtocolInfo struct {
	ID        string          `json:"id"`
	Actions   []string        `json:"actions"`
	Reads     []string        `json:"reads"`
	Events    []ProtocolEvent `json:"events"`
	Providers []string        `json:"providers"`
	Consumers []string        `json:"consumers"`
	Bound     string          `json:"bound,omitempty"`
}

func (t *Tenant) Protocols() []ProtocolInfo {
	byID := map[string]*ProtocolInfo{}
	var order []string
	info := func(p Protocol) *ProtocolInfo {
		if byID[p.ID()] == nil {
			i := &ProtocolInfo{ID: p.ID(), Reads: p.Reads, Events: p.Events, Providers: []string{}, Consumers: []string{}}
			for _, a := range p.Actions {
				i.Actions = append(i.Actions, a.Schema)
			}
			byID[p.ID()], order = i, append(order, p.ID())
		}
		return byID[p.ID()]
	}
	for _, a := range t.apps {
		for _, pv := range a.Manifest().Provides {
			i := info(pv.Protocol)
			i.Providers = append(i.Providers, a.Manifest().ID)
		}
	}
	for _, a := range t.apps {
		for _, c := range a.Manifest().Consumes {
			if byID[c.Protocol] == nil {
				byID[c.Protocol], order = &ProtocolInfo{ID: c.Protocol, Providers: []string{}}, append(order, c.Protocol)
			}
			byID[c.Protocol].Consumers = append(byID[c.Protocol].Consumers, a.Manifest().ID)
		}
	}
	out := []ProtocolInfo{}
	for _, id := range order {
		if b, ok := t.bound(id); ok {
			byID[id].Bound = b.provider.Manifest().ID
		}
		out = append(out, *byID[id])
	}
	return out
}
