package platformserver

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type binding struct {
	provider  platform.App
	provision platform.Provision
}

// bind resolves consumed protocols to providers enabled before the consumer.
func (t *Tenant) bind(i int, a platform.App) error {
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
func (t *Tenant) provider(used string) (platform.App, string, bool) {
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

// consumes reports whether c's app consumes protocol.
func (t *Tenant) consumes(c platform.Caller, protocol string) bool {
	self := t.app(c.App)
	return self != nil && slices.ContainsFunc(self.Manifest().Consumes, func(x platform.Consumption) bool { return x.Protocol == protocol })
}

// invoke calls a protocol action for a consumer: a flow's step, an agent, or a
// decision's request once it is accepted (ADR-0026).
func (t *Tenant) invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	if !t.consumes(c, protocol) {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	provider, schema, ok := t.provider(platform.ProtocolAction(protocol, action))
	if !ok {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} // no provider bound
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	target := &pb.EntityRef{Type: declared.Target, Id: id}
	called := platform.NewCaller(runtime{t}, c.Member, provider.Manifest().ID, c.Replaying, c.Automation)
	record, err := provider.Submit(called, &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: t.authorityOf(declared.Target),
		Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: payload}, now)
	return target, record, err
}

// probe runs a protocol action's policy and rules at the provider, for a
// consumer's rules, applying nothing (Caller.Probe). A replay does not ask
// again: the decision held; only whether a provider is bound is said again.
func (t *Tenant) probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	if c.Replaying {
		if _, _, bound := t.provider(platform.ProtocolAction(protocol, action)); !bound {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return nil
	}
	was := t.probing
	t.probing = true
	defer func() { t.probing = was }()
	_, _, err := t.invoke(c, protocol, action, id, payload, "probe", "", now)
	return err
}

// request is a protocol action an accepted decision asked for (Caller.Request).
type request struct {
	platform.Request
	caller platform.Caller
	record *pb.ChangeRecord
	n      int
}

// answer runs a decision's request at the provider, as the member who decided,
// and submits the answer to the requesting app's reply action on the
// decision's target (ADR-0026 D2). Both are journaled as submissions of their
// apps: a replay runs what was recorded and never asks the provider again.
func (t *Tenant) answer(q request, now time.Time) {
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	answer := platform.Answer{Call: q.Target, Action: q.Action, Outcome: "accepted"}
	ref, done, err := t.invoke(q.caller, q.Protocol, q.Action, q.Target, platform.Raw(q.Payload), key, s.GetIdempotencyKey(), now)
	if err != nil {
		answer.Outcome, answer.Code = "refused", err.Code.String()
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
		t.journal(t.app(t.owner["action:"+done.GetSubmission().GetSchema().GetName()].Manifest().ID), q.caller.Member, done.GetSubmission(), now)
	}
	app := t.app(q.caller.App)
	payload, _ := json.Marshal(answer)
	c := t.automation(q.caller.App, false)
	reply := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key, CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := app.Submit(c, reply, now); err != nil {
		log.Printf("tenant %s: %s refused the answer %s to its request %s: %v", t.ID, q.caller.App, payload, key, err) // a defect of the app
		return
	}
	t.journal(app, c.Member, reply, now)
}

// query reads a protocol read from every provider, the bound one first (Caller.Query).
func (t *Tenant) query(c platform.Caller, protocol, read string) ([]platform.ProviderResult, *kernel.Error) {
	if !t.consumes(c, protocol) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	all := t.providers(protocol)
	if b, ok := t.bound(protocol); ok {
		first := func(x binding) int { return map[bool]int{true: 0, false: 1}[x.provider == b.provider] }
		slices.SortStableFunc(all, func(x, y binding) int { return first(x) - first(y) })
	}
	var out []platform.ProviderResult
	for _, b := range all {
		called := platform.NewCaller(runtime{t}, c.Member, b.provider.Manifest().ID, c.Replaying, c.Automation)
		result, err := b.provider.Read(called, b.provision.Reads[read])
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ProviderResult{Provider: b.provider.Manifest().ID, Type: b.entityType(), Result: result})
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
func (t *Tenant) Invoke(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	provider, schema, ok := t.provider(platform.ProtocolAction(protocol, action))
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
func (t *Tenant) Query(m platform.Member, protocol, read string) (any, *kernel.Error) {
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
func (t *Tenant) protocolEvents(e platform.Event) []string {
	var out []string
	if a := t.app(e.App); a != nil {
		for _, pv := range a.Manifest().Provides {
			for event, schema := range pv.Events {
				if schema == e.Record.GetSubmission().GetSchema().GetName() {
					out = append(out, platform.ProtocolAction(pv.Protocol.ID(), event))
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// protocolEvent finds a protocol event's declaration by "<protocol id>#<event>".
func (t *Tenant) protocolEvent(name string) (platform.ProtocolEvent, bool) {
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
	return platform.ProtocolEvent{}, false
}

// ProtocolInfo describes a protocol in a tenant: who provides it, who consumes it, which provider is bound.
type ProtocolInfo struct {
	ID        string                   `json:"id"`
	Actions   []string                 `json:"actions"`
	Reads     []string                 `json:"reads"`
	Events    []platform.ProtocolEvent `json:"events"`
	Providers []string                 `json:"providers"`
	Consumers []string                 `json:"consumers"`
	Bound     string                   `json:"bound,omitempty"`
}

func (t *Tenant) Protocols() []ProtocolInfo {
	byID := map[string]*ProtocolInfo{}
	var order []string
	info := func(p platform.Protocol) *ProtocolInfo {
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
