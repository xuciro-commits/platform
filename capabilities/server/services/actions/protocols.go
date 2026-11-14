package actions

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Binding struct {
	Provider  platform.App
	Provision platform.Provision
}

func (b Binding) ID() string { return b.Provider.Manifest().ID }

func (b Binding) EntityType() string {
	for _, schema := range b.Provision.Actions {
		if a, ok := b.Provider.Manifest().Actions.Action(schema); ok {
			return a.Target
		}
	}
	return ""
}

type Bindings struct {
	mu        sync.Mutex
	providers map[string]string
}

func NewBindings(providers map[string]string) *Bindings {
	return &Bindings{providers: maps.Clone(providers)}
}

func (b *Bindings) All() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.providers)
}

func (b *Bindings) Clone() *Bindings { return NewBindings(b.All()) }

func (b *Bindings) get(protocol string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	provider, ok := b.providers[protocol]
	return provider, ok
}

func (b *Bindings) set(protocol, provider string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.providers == nil {
		b.providers = map[string]string{}
	}
	b.providers[protocol] = provider
}

func (b *Bindings) Bind(prior []platform.App, a platform.App) error {
	for _, c := range a.Manifest().Consumes {
		var found []string
		for _, p := range prior {
			for _, pv := range p.Manifest().Provides {
				if pv.Protocol.ID() == c.Protocol {
					found = append(found, p.Manifest().ID)
				}
			}
		}
		switch {
		case len(found) == 0 && !c.Optional:
			return fmt.Errorf("%s consumes %s, which no app before it provides", a.Manifest().ID, c.Protocol)
		case len(found) > 0:
			if current, ok := b.get(c.Protocol); ok && current != found[0] {
				continue
			}
			b.set(c.Protocol, found[0])
		}
	}
	return nil
}

type ProtocolHost interface {
	LiveApps() []platform.App
	OwnerActive(id string) bool
	App(id string) platform.App
	AuthorityOf(dataClass string) string
	StageSubmission(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type Router struct {
	Host     ProtocolHost
	Bindings *Bindings
	Tenant   string
}

func (r Router) DecideBinding(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Provider string }
	json.Unmarshal(s.GetPayload(), &p)
	if !slices.ContainsFunc(r.Providers(id), func(b Binding) bool { return b.ID() == p.Provider }) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) { r.Rebind(id, p.Provider) }, nil
}

func (r Router) Bound(protocol string) (Binding, bool) {
	id, ok := r.Bindings.get(protocol)
	if !ok {
		return Binding{}, false
	}
	provider := r.Host.App(id)
	if provider == nil {
		return Binding{}, false
	}
	if !r.Host.OwnerActive(id) {
		return Binding{Provider: provider}, true
	}
	for _, current := range provider.Manifest().Provides {
		if current.Protocol.ID() == protocol {
			return Binding{Provider: provider, Provision: current}, true
		}
	}
	return Binding{Provider: provider}, true
}

func (r Router) Providers(protocol string) []Binding {
	var out []Binding
	for _, a := range r.Host.LiveApps() {
		for _, pv := range a.Manifest().Provides {
			if pv.Protocol.ID() == protocol {
				out = append(out, Binding{a, pv})
			}
		}
	}
	return out
}

func (r Router) Resolve(protocol string) (Binding, bool) {
	if b, ok := r.Bound(protocol); ok {
		return b, true
	}
	if all := r.Providers(protocol); len(all) > 0 {
		return all[0], true
	}
	return Binding{}, false
}

func (r Router) Provider(used string) (platform.App, string, bool) {
	protocol, action, ok := strings.Cut(used, "#")
	if !ok {
		return nil, "", false
	}
	b, bound := r.Resolve(protocol)
	if !bound {
		return nil, "", false
	}
	schema, mapped := b.Provision.Actions[action]
	return b.Provider, schema, mapped
}

func (r Router) ProtocolProvider(protocol string) (string, bool) {
	b, ok := r.Resolve(protocol)
	if !ok {
		return "", false
	}
	return b.ID(), true
}

func (r Router) Rebind(protocol, provider string) bool {
	if !slices.ContainsFunc(r.Providers(protocol), func(b Binding) bool { return b.ID() == provider }) {
		return false
	}
	r.Bindings.set(protocol, provider)
	return true
}

func (r Router) consumes(c platform.Caller, protocol string) bool {
	self := r.Host.App(c.App)
	return self != nil && slices.ContainsFunc(self.Manifest().Consumes, func(x platform.Consumption) bool { return x.Protocol == protocol })
}

type call func(Binding, platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)

func (r Router) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time, original ...string) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	if !r.consumes(c, protocol) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{app} does not consume {protocol}", c.App, protocol)
	}
	b, ok := r.Resolve(protocol)
	pinned := len(original) > 0 && original[0] != ""
	if pinned {
		ok = false
		for _, provider := range r.Providers(protocol) {
			if provider.ID() == original[0] {
				b, ok = provider, true
				break
			}
		}
	}
	if _, mapped := b.Provision.Actions[action]; !ok || !mapped {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app provides {protocol} here", protocol)
	}
	send := func(b Binding, c platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
		schema := b.Provision.Actions[action]
		declared, _ := b.Provider.Manifest().Actions.Action(schema)
		target := &pb.EntityRef{Type: declared.Target, Id: id}
		called := platform.RouteCaller(c, b.ID())
		sub := &pb.Submission{TenantId: r.Tenant, PrincipalId: c.ID, Authority: r.Host.AuthorityOf(declared.Target),
			Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: payload}
		var record *pb.ChangeRecord
		var err *kernel.Error
		if c.Staging() {
			record, err = r.Host.StageSubmission(called, sub, now)
		} else {
			record, err = platform.Decide(called, b.Provider, sub, now)
		}
		return target, record, Explained(err, b.Provider, schema, target.GetType()+"/"+id)
	}
	if !pinned {
		b = r.holder(c, b, protocol, action, send)
	}
	return send(b, c)
}

func (r Router) holder(c platform.Caller, bound Binding, protocol, action string, send call) Binding {
	knows := func(b Binding) bool {
		err := platform.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
			_, _, err := send(b, probe)
			return err
		})
		return err == nil || err.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND
	}
	if knows(bound) {
		return bound
	}
	for _, b := range r.Providers(protocol) {
		if _, mapped := b.Provision.Actions[action]; mapped && b.ID() != bound.ID() && knows(b) {
			return b
		}
	}
	return bound
}

func (r Router) Probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	if c.Replaying {
		if _, _, bound := r.Provider(platform.ProtocolAction(protocol, action)); !bound {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return nil
	}
	return platform.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, _, err := r.Invoke(probe, protocol, action, id, payload, "probe", "", now)
		return err
	})
}

func (r Router) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	if len(provider) > 1 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !r.consumes(c, protocol) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	all := r.Providers(protocol)
	if b, ok := r.Bound(protocol); ok {
		first := func(x Binding) int { return map[bool]int{true: 0, false: 1}[x.ID() == b.ID()] }
		slices.SortStableFunc(all, func(x, y Binding) int { return first(x) - first(y) })
	}
	var out []platform.ProviderResult
	for _, b := range all {
		if len(provider) > 0 && b.ID() != provider[0] {
			continue
		}
		called := platform.RouteCaller(c, b.ID())
		name, ok := b.Provision.Reads[read]
		if !ok || !slices.Contains(b.Provision.Protocol.Reads, read) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		result, err := platform.CallRead(b.Provider, called, name)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ProviderResult{Provider: b.ID(), Type: b.EntityType(), Result: result})
	}
	return out, nil
}

func (r Router) BoundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	if !r.consumes(c, protocol) {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if b, ok := r.Resolve(protocol); ok {
		return b.ID(), nil
	}
	return "", nil
}

func (r Router) Events(e platform.Event) []string {
	var out []string
	if a := r.Host.App(e.App); a != nil {
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

func (r Router) Event(name string) (platform.ProtocolEvent, bool) {
	protocol, event, _ := strings.Cut(name, "#")
	for _, a := range r.Host.LiveApps() {
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

func (r Router) Protocols() []ProtocolInfo {
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
	for _, a := range r.Host.LiveApps() {
		for _, pv := range a.Manifest().Provides {
			i := info(pv.Protocol)
			i.Providers = append(i.Providers, a.Manifest().ID)
		}
	}
	for _, a := range r.Host.LiveApps() {
		for _, c := range a.Manifest().Consumes {
			if byID[c.Protocol] == nil {
				byID[c.Protocol], order = &ProtocolInfo{ID: c.Protocol, Providers: []string{}}, append(order, c.Protocol)
			}
			byID[c.Protocol].Consumers = append(byID[c.Protocol].Consumers, a.Manifest().ID)
		}
	}
	out := []ProtocolInfo{}
	for _, id := range order {
		if b, ok := r.Bound(id); ok {
			byID[id].Bound = b.ID()
		}
		out = append(out, *byID[id])
	}
	return out
}

func Explained(err *kernel.Error, a platform.App, schema, target string) *kernel.Error {
	if err == nil || err.Message != "" {
		return err
	}
	action := schema
	if declared, ok := a.Manifest().Actions.Action(schema); ok && declared.Title != "" {
		action = declared.Title
	}
	text := map[pb.ErrorCode]string{
		pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:     "{action} was refused: a value is missing or out of range",
		pb.ErrorCode_ERROR_CODE_CONFLICT:             "{action} was refused: {target} is not in a state that allows it, or changed since it was read",
		pb.ErrorCode_ERROR_CODE_NOT_FOUND:            "{action} was refused: {target} does not exist, or you may not see it",
		pb.ErrorCode_ERROR_CODE_POLICY_DENIED:        "{action} was refused: you may not do it to {target}",
		pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA:       "{action} is not an action here",
		pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT: "{action} was sent before with other values",
		pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    "{action} names a record that does not exist",
		pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY:        "{action} was refused: another authority decides {target}",
	}[err.Code]
	if text == "" {
		return err
	}
	return platform.Refuse(err.Code, text, action, target)
}

func (r Router) ReadName(protocol, read string) (string, *kernel.Error) {
	b, bound := r.Resolve(protocol)
	if !bound {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	name, declared := b.Provision.Reads[read]
	if !declared || !slices.Contains(b.Provision.Protocol.Reads, read) {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return name, nil
}
