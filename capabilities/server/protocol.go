package platformserver

import (
	"fmt"
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
				continue
			}
			t.bindings[c.Protocol] = found[0]
		}
	}
	return nil
}

func (t *Tenant) bound(protocol string) (binding, bool) {
	t.opsMu.Lock()
	b, ok := t.bindings[protocol]
	t.opsMu.Unlock()
	if !ok {
		return binding{}, false
	}
	if !t.ownerActive(b.provider.Manifest().ID) {
		return binding{provider: b.provider}, true
	}
	for _, current := range b.provider.Manifest().Provides {
		if current.Protocol.ID() == protocol {
			return binding{provider: b.provider, provision: current}, true
		}
	}
	return binding{provider: b.provider}, true
}

func (t *Tenant) providers(protocol string) []binding {
	var out []binding
	for _, a := range t.liveApps() {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				out = append(out, binding{a, pv})
			}
		}
	}
	return out
}

func (t *Tenant) resolve(protocol string) (binding, bool) {
	if b, ok := t.bound(protocol); ok {
		return b, true
	}
	for _, a := range t.liveApps() {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				return binding{a, pv}, true
			}
		}
	}
	return binding{}, false
}

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

func (t *Tenant) consumes(c platform.Caller, protocol string) bool {
	self := t.app(c.App)
	return self != nil && slices.ContainsFunc(self.Manifest().Consumes, func(x platform.Consumption) bool { return x.Protocol == protocol })
}

func (t *Tenant) invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time, original ...string) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	if !t.consumes(c, protocol) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{app} does not consume {protocol}", c.App, protocol)
	}
	b, ok := t.resolve(protocol)
	pinned := len(original) > 0 && original[0] != ""
	if pinned {
		ok = false
		for _, provider := range t.providers(protocol) {
			if provider.provider.Manifest().ID == original[0] {
				b, ok = provider, true
				break
			}
		}
	}
	if _, mapped := b.provision.Actions[action]; !ok || !mapped {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app provides {protocol} here", protocol)
	}
	call := func(b binding, c platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
		schema := b.provision.Actions[action]
		declared, _ := b.provider.Manifest().Actions.Action(schema)
		target := &pb.EntityRef{Type: declared.Target, Id: id}
		called := platform.RouteCaller(c, b.provider.Manifest().ID)
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: t.authorityOf(declared.Target),
			Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: payload}
		var record *pb.ChangeRecord
		var err *kernel.Error
		if c.Staging() {
			record, err = (hostView{t: t}).Submit(called, sub, now)
		} else {
			record, err = platform.Decide(called, b.provider, sub, now)
		}
		return target, record, explained(err, b.provider, schema, target.GetType()+"/"+id)
	}
	if !pinned {
		b = t.holder(c, b, protocol, action, call)
	}
	return call(b, c)
}

func (t *Tenant) holder(c platform.Caller, bound binding, protocol, action string, call func(binding, platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)) binding {
	knows := func(b binding) bool {
		err := platform.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
			_, _, err := call(b, probe)
			return err
		})
		return err == nil || err.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND
	}
	if knows(bound) {
		return bound
	}
	for _, b := range t.providers(protocol) {
		if _, mapped := b.provision.Actions[action]; mapped && b.provider != bound.provider && knows(b) {
			return b
		}
	}
	return bound
}

func (t *Tenant) probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	if c.Replaying {
		if _, _, bound := t.provider(platform.ProtocolAction(protocol, action)); !bound {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return nil
	}
	return platform.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, _, err := t.invoke(probe, protocol, action, id, payload, "probe", "", now)
		return err
	})
}

type request struct {
	platform.Request
	caller platform.Caller
	record *pb.ChangeRecord
	n      int
}

func (t *Tenant) query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	if len(provider) > 1 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
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
		if len(provider) > 0 && b.provider.Manifest().ID != provider[0] {
			continue
		}
		called := platform.RouteCaller(c, b.provider.Manifest().ID)
		name, ok := b.provision.Reads[read]
		if !ok || !slices.Contains(b.provision.Protocol.Reads, read) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		result, err := platform.CallRead(b.provider, called, name)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ProviderResult{Provider: b.provider.Manifest().ID, Type: b.entityType(), Result: result})
	}
	return out, nil
}

func (b binding) entityType() string {
	for _, schema := range b.provision.Actions {
		if a, ok := b.provider.Manifest().Actions.Action(schema); ok {
			return a.Target
		}
	}
	return ""
}

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

func (t *Tenant) Invoke(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	target, record, err, _ := t.invokeDiagnosed(m, protocol, action, id, payload, key, now)
	return target, record, err
}

func (t *Tenant) invokeDiagnosed(m platform.Member, protocol, action, id string, payload []byte, key string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	provider, schema, ok := t.provider(platform.ProtocolAction(protocol, action))
	if !ok {
		return nil, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	target := &pb.EntityRef{Type: declared.Target, Id: id}
	record, err, issues := t.submitDiagnosed(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: t.authorityOf(declared.Target),
		Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return target, record, err, issues
}

func (t *Tenant) Query(m platform.Member, protocol, read string) (any, *kernel.Error) {
	b, bound := t.resolve(protocol)
	if !bound {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	name, declared := b.provision.Reads[read]
	if !declared || !slices.Contains(b.provision.Protocol.Reads, read) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.read(m, name)
}

func (t *Tenant) authorityOf(dataClass string) string {
	for _, d := range t.Declarations() {
		if d.GetDataClass() == dataClass {
			return d.GetAuthorityId()
		}
	}
	return ""
}

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

func (t *Tenant) protocolEvent(name string) (platform.ProtocolEvent, bool) {
	protocol, event, _ := strings.Cut(name, "#")
	for _, a := range t.liveApps() {
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

type ProtocolInfo struct {
	Contract  *platform.Protocol       `json:"contract,omitempty"`
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
			i := &ProtocolInfo{ID: p.ID(), Contract: &p, Reads: p.Reads, Events: p.Events, Providers: []string{}, Consumers: []string{}}
			for _, a := range p.Actions {
				i.Actions = append(i.Actions, a.Schema)
			}
			byID[p.ID()], order = i, append(order, p.ID())
		}
		return byID[p.ID()]
	}
	for _, a := range t.liveApps() {
		for _, pv := range a.Manifest().Provides {
			i := info(pv.Protocol)
			i.Providers = append(i.Providers, a.Manifest().ID)
		}
	}
	for _, a := range t.liveApps() {
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

func (t *Tenant) boundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	if !t.consumes(c, protocol) {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if b, ok := t.resolve(protocol); ok {
		return b.provider.Manifest().ID, nil
	}
	return "", nil
}
