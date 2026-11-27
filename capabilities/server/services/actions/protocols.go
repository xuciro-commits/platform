package actions

import (
	"encoding/json"
	"fmt"
	"maps"
	runtimeprotocol "platformserver/kernel/protocol"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type Binding struct {
	Provider  platform.Service
	Provision platform.Provision
	Qualified bool
}

// ID names the implementation: the providing app, or app@type when that app has
// more than one record type implementing the same contract.
func (b Binding) ID() string {
	if b.Qualified {
		return b.App() + "@" + b.EntityType()
	}
	return b.App()
}

// App is the app that provides the implementation.
func (b Binding) App() string { return b.Provider.Manifest().ID }

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
	epoch     uint64
}

func NewBindings(providers map[string]string) *Bindings {
	return &Bindings{providers: maps.Clone(providers)}
}

func (b *Bindings) All() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.providers)
}

func (b *Bindings) Clone() *Bindings {
	clone := NewBindings(b.All())
	clone.epoch = b.Epoch()
	return clone
}

func (b *Bindings) Epoch() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.epoch
}

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
	b.epoch++
}

func (b *Bindings) unset(protocol string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.providers, protocol)
	b.epoch++
}

func (b *Bindings) Bind(prior []platform.Service, a platform.Service) error {
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
	LiveApps() []platform.Service
	OwnerActive(id string) bool
	App(id string) platform.Service
	AuthorityOf(dataClass string) string
	StageSubmission(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error)
}

type Router struct {
	Host        ProtocolHost
	Bindings    *Bindings
	Tenant      string
	AssetActive func(platform.AssetRef) bool
}

func (r Router) DecideBinding(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Provider string }
	json.Unmarshal(s.GetPayload(), &p)
	if p.Provider == "" {
		return func(*pb.ChangeRecord) { r.Bindings.unset(id) }, nil
	}
	if app, name, named := runtimeprotocol.SplitImplementationIdentity(p.Provider); named {
		if !r.AssetActive(platform.AssetRef{App: app, Kind: runtimeprotocol.AssetImplementation, Name: name}) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		for _, a := range r.Host.LiveApps() {
			gate, ok := a.(interface {
				ConnectorBindable(string, string) (bool, error)
			})
			if !ok || a.Manifest().ID != app {
				continue
			}
			known, err := gate.ConnectorBindable(id, p.Provider)
			if !known {
				break
			}
			if err != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: err.Error()}
			}
			provider := p.Provider
			return func(*pb.ChangeRecord) { r.Bindings.set(id, provider) }, nil
		}
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !slices.ContainsFunc(r.Providers(id), func(b Binding) bool { return b.ID() == p.Provider }) {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) { r.Rebind(id, p.Provider) }, nil
}

func (r Router) Bound(protocol string) (Binding, bool) {
	id, ok := r.Bindings.get(protocol)
	if !ok {
		return Binding{}, false
	}
	app, typ, _ := strings.Cut(id, "@")
	provider := r.Host.App(app)
	if provider == nil {
		return Binding{}, false
	}
	if !r.Host.OwnerActive(app) {
		return Binding{}, false
	}
	provisions := provisionsOf(provider, protocol)
	for _, current := range provisions {
		b := Binding{Provider: provider, Provision: current, Qualified: len(provisions) > 1}
		if typ == "" || b.EntityType() == typ {
			if !r.provisionActive(provider, current) {
				return Binding{}, false
			}
			return b, true
		}
	}
	return Binding{}, false
}

func (r Router) provisionActive(a platform.Service, pv platform.Provision) bool {
	b := Binding{Provider: a, Provision: pv}
	return r.AssetActive(platform.AssetRef{App: b.App(), Kind: platform.AssetObject, Name: b.EntityType()})
}

func provisionsOf(a platform.Service, protocol string) []platform.Provision {
	var out []platform.Provision
	for _, pv := range a.Manifest().Provides {
		if pv.Protocol.ID() == protocol {
			out = append(out, pv)
		}
	}
	return out
}

// Providers lists every declarative implementation of the contract; an app with two
// record types implementing it offers two, told apart by type.
func (r Router) Providers(protocol string) []Binding {
	var out []Binding
	for _, a := range r.Host.LiveApps() {
		provisions := provisionsOf(a, protocol)
		for _, pv := range provisions {
			if r.provisionActive(a, pv) {
				out = append(out, Binding{a, pv, len(provisions) > 1})
			}
		}
	}
	return out
}

func (r Router) Declared(protocol string) bool {
	for _, a := range r.Host.LiveApps() {
		if declared, ok := a.(interface {
			ProtocolContracts() []runtimeprotocol.ProtocolContract
		}); ok && slices.ContainsFunc(declared.ProtocolContracts(), func(c runtimeprotocol.ProtocolContract) bool { return c.ID() == protocol }) {
			return true
		}
	}
	return false
}

func (r Router) Unavailable(protocol string) *platform.Error {
	if r.Declared(protocol) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Capability {protocol} has no bound implementation here", protocol)
	}
	return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (r Router) Resolve(protocol string) (Binding, bool) {
	if b, ok := r.Bound(protocol); ok {
		return b, true
	}
	if _, explicit := r.Bindings.get(protocol); explicit || r.Declared(protocol) {
		return Binding{}, false
	}
	if all := r.Providers(protocol); len(all) > 0 {
		return all[0], true
	}
	return Binding{}, false
}

func (r Router) Provider(used string) (platform.Service, string, bool) {
	protocol, action, ok := strings.Cut(used, "#")
	if !ok {
		return nil, "", false
	}
	if provider, bound := r.Bindings.get(protocol); bound {
		if app, name, named := runtimeprotocol.SplitImplementationIdentity(provider); named {
			owner := r.Host.App(app)
			served := slices.ContainsFunc(r.implementations()[app], func(i runtimeprotocol.ProtocolImplementation) bool {
				return i.Name == name && i.Contract == protocol && slices.ContainsFunc(i.Requests, func(q runtimeprotocol.ImplementationRequest) bool { return q.Action == action })
			})
			if !served || owner == nil {
				return nil, "", false
			}
			return owner, runtimeprotocol.SchemaCapabilityCallSend, true
		}
	}
	b, bound := r.Resolve(protocol)
	if !bound {
		return nil, "", false
	}
	schema, mapped := b.Provision.Actions[action]
	if !mapped {
		return nil, "", false
	}
	return b.Provider, schema, true
}

func (r Router) ProtocolProvider(protocol string) (string, bool) {
	b, ok := r.Resolve(protocol)
	if !ok {
		return "", false
	}
	return b.App(), true
}

func (r Router) Rebind(protocol, provider string) bool {
	if app, name, named := runtimeprotocol.SplitImplementationIdentity(provider); named {
		if !slices.ContainsFunc(r.implementations()[app], func(i runtimeprotocol.ProtocolImplementation) bool { return i.Name == name && i.Contract == protocol }) {
			return false
		}
	} else if !slices.ContainsFunc(r.Providers(protocol), func(b Binding) bool { return b.ID() == provider }) {
		return false
	}
	r.Bindings.set(protocol, provider)
	return true
}

func (r Router) consumes(c platform.Caller, protocol string) bool {
	self := r.Host.App(c.App)
	return self != nil && slices.ContainsFunc(self.Manifest().Consumes, func(x platform.Consumption) bool { return x.Protocol == protocol })
}

type call func(Binding, platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error)

func (r Router) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time, original ...string) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error) {
	if !r.consumes(c, protocol) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{app} does not consume {protocol}", c.App, protocol)
	}
	if provider, bound := r.Bindings.get(protocol); bound {
		if _, _, named := runtimeprotocol.SplitImplementationIdentity(provider); named {
			return r.callConnector(c, protocol, action, payload, key, correlation, now)
		}
	}
	b, ok := r.Resolve(protocol)
	pinned := len(original) > 0 && original[0] != ""
	if pinned {
		ok = false
		for _, provider := range r.Providers(protocol) {
			if provider.ID() == original[0] || !provider.Qualified && provider.App() == original[0] {
				b, ok = provider, true
				break
			}
		}
	}
	if !ok && r.Declared(protocol) {
		return nil, nil, r.Unavailable(protocol)
	}
	if _, mapped := b.Provision.Actions[action]; !ok || !mapped {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "No app provides {protocol} here", protocol)
	}
	send := func(b Binding, c platform.Caller) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error) {
		schema := b.Provision.Actions[action]
		declared, _ := b.Provider.Manifest().Actions.Action(schema)
		target := &pb.EntityRef{Type: declared.Target, Id: id}
		called := platform.RouteCaller(c, b.App())
		sub := &pb.Submission{TenantId: r.Tenant, PrincipalId: c.ID, Authority: r.Host.AuthorityOf(declared.Target),
			Target: target, Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: payload}
		var record *pb.ChangeRecord
		var err *platform.Error
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

func (r Router) ConnectorSubmission(principal, protocol, action string, payload []byte, key, correlation string) (*pb.Submission, platform.Service, bool) {
	implementation, bound := r.Bindings.get(protocol)
	app, _, named := runtimeprotocol.SplitImplementationIdentity(implementation)
	owner, served, mapped := r.Provider(runtimeprotocol.ProtocolAction(protocol, action))
	if !bound || !named || owner == nil || !mapped || owner.Manifest().ID != app {
		return nil, nil, false
	}
	raw := json.RawMessage(payload)
	if len(payload) == 0 || !json.Valid(payload) {
		raw = json.RawMessage("{}")
	}
	body, _ := json.Marshal(map[string]any{"protocol": protocol, "action": action, "implementation": implementation, "payload": raw})
	target := &pb.EntityRef{Type: runtimeprotocol.CapabilityCallType, Id: runtimeprotocol.CapabilityCallID(protocol, action, key)}
	return &pb.Submission{TenantId: r.Tenant, PrincipalId: principal, Authority: r.Host.AuthorityOf(target.GetType()), Target: target,
		Schema: &pb.SchemaRef{Name: served, Version: 1}, IdempotencyKey: key, CorrelationId: correlation, Payload: body}, owner, true
}

func (r Router) IsBoundConnector(protocol, implementation string) bool {
	selected, bound := r.Bindings.get(protocol)
	return bound && selected == implementation
}

func (r Router) Submission(principal, protocol, action, id string, payload []byte, key string) (*pb.Submission, bool) {
	if sub, _, ok := r.ConnectorSubmission(principal, protocol, action, payload, key, ""); ok {
		return sub, true
	}
	provider, schema, ok := r.Provider(runtimeprotocol.ProtocolAction(protocol, action))
	if !ok {
		return nil, false
	}
	declared, _ := provider.Manifest().Actions.Action(schema)
	return &pb.Submission{TenantId: r.Tenant, PrincipalId: principal, Authority: r.Host.AuthorityOf(declared.Target), Target: &pb.EntityRef{Type: declared.Target, Id: id},
		Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: payload}, true
}

func (r Router) callConnector(c platform.Caller, protocol, action string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error) {
	sub, owner, ok := r.ConnectorSubmission(c.ID, protocol, action, payload, key, correlation)
	if !ok {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The bound connector does not serve {protocol}#{action}", protocol, action)
	}
	called := platform.RouteCaller(c, owner.Manifest().ID)
	if c.Staging() {
		record, err := r.Host.StageSubmission(called, sub, now)
		return sub.GetTarget(), record, err
	}
	record, err := platform.Decide(called, owner, sub, now)
	return sub.GetTarget(), record, err
}

func (r Router) holder(c platform.Caller, bound Binding, protocol, action string, send call) Binding {
	knows := func(b Binding) bool {
		err := platform.ProbeDecision(c, func(probe platform.Caller) *platform.Error {
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

func (r Router) Probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *platform.Error {
	if c.Replaying {
		if _, _, bound := r.Provider(runtimeprotocol.ProtocolAction(protocol, action)); !bound {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return nil
	}
	return platform.ProbeDecision(c, func(probe platform.Caller) *platform.Error {
		_, _, err := r.Invoke(probe, protocol, action, id, payload, "probe", "", now)
		return err
	})
}

func (r Router) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *platform.Error) {
	if len(provider) > 1 {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !r.consumes(c, protocol) {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	all := r.Providers(protocol)
	if b, ok := r.Bound(protocol); ok {
		first := func(x Binding) int { return map[bool]int{true: 0, false: 1}[x.ID() == b.ID()] }
		slices.SortStableFunc(all, func(x, y Binding) int { return first(x) - first(y) })
	}
	var out []platform.ProviderResult
	for _, b := range all {
		if len(provider) > 0 && b.ID() != provider[0] && b.App() != provider[0] {
			continue
		}
		called := platform.RouteCaller(c, b.App())
		name, ok := b.Provision.Reads[read]
		if !ok || !slices.Contains(b.Provision.Protocol.Reads, read) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		result, err := platform.CallRead(b.Provider, called, name)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ProviderResult{Provider: b.App(), Type: b.EntityType(), Result: result})
	}
	return out, nil
}

func (r Router) BoundProvider(c platform.Caller, protocol string) (string, *platform.Error) {
	if !r.consumes(c, protocol) {
		return "", &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if b, ok := r.Resolve(protocol); ok {
		return b.App(), nil
	}
	return "", nil
}

func (r Router) Events(e platform.Event) []string {
	var out []string
	if a := r.Host.App(e.App); a != nil {
		for _, pv := range a.Manifest().Provides {
			for event, schema := range pv.Events {
				if schema == e.Record.GetSubmission().GetSchema().GetName() {
					out = append(out, runtimeprotocol.ProtocolAction(pv.Protocol.ID(), event))
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
	Contract        *platform.Protocol       `json:"contract,omitempty"`
	ID              string                   `json:"id"`
	Title           string                   `json:"title,omitempty"`
	Description     string                   `json:"description,omitempty"`
	Declared        bool                     `json:"declared,omitempty"`
	Actions         []string                 `json:"actions"`
	Reads           []string                 `json:"reads"`
	Events          []platform.ProtocolEvent `json:"events"`
	Providers       []string                 `json:"providers"`
	Consumers       []string                 `json:"consumers"`
	Bound           string                   `json:"bound,omitempty"`
	Implementations []ImplementationInfo     `json:"implementations,omitempty"`
}

type ImplementationInfo struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Origins    []string `json:"origins"`
	Credential string   `json:"credential,omitempty"`
	State      string   `json:"state,omitempty"`
}

func (r Router) connectorState(protocol, identity string) string {
	app, _, _ := runtimeprotocol.SplitImplementationIdentity(identity)
	for _, a := range r.Host.LiveApps() {
		if gate, ok := a.(interface {
			ConnectorState(string, string) string
		}); ok && a.Manifest().ID == app {
			return gate.ConnectorState(protocol, identity)
		}
	}
	return ""
}

func (r Router) implementations() map[string][]runtimeprotocol.ProtocolImplementation {
	out := map[string][]runtimeprotocol.ProtocolImplementation{}
	for _, a := range r.Host.LiveApps() {
		if declared, ok := a.(interface {
			ProtocolImplementations() []runtimeprotocol.ProtocolImplementation
		}); ok {
			app := a.Manifest().ID
			for _, impl := range declared.ProtocolImplementations() {
				if r.AssetActive(platform.AssetRef{App: app, Kind: runtimeprotocol.AssetImplementation, Name: impl.Name}) {
					out[app] = append(out[app], impl)
				}
			}
		}
	}
	return out
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
			if !r.provisionActive(a, pv) {
				continue
			}
			i := info(pv.Protocol)
			b := Binding{Provider: a, Provision: pv, Qualified: len(provisionsOf(a, pv.Protocol.ID())) > 1}
			i.Providers = append(i.Providers, b.ID())
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
	var contracts []runtimeprotocol.ProtocolContract
	for _, a := range r.Host.LiveApps() {
		if declared, ok := a.(interface {
			ProtocolContracts() []runtimeprotocol.ProtocolContract
		}); ok {
			contracts = append(contracts, declared.ProtocolContracts()...)
		}
	}
	for _, contract := range contracts {
		p := contract.Protocol()
		id := p.ID()
		if byID[id] == nil {
			byID[id], order = &ProtocolInfo{ID: id, Providers: []string{}, Consumers: []string{}}, append(order, id)
		}
		i := byID[id]
		i.Contract, i.Title, i.Description, i.Declared = &p, contract.Title, contract.Description, true
		i.Actions, i.Reads, i.Events = nil, p.Reads, p.Events
		for _, a := range p.Actions {
			i.Actions = append(i.Actions, a.Schema)
		}
	}
	for app, list := range r.implementations() {
		for _, i := range list {
			if byID[i.Contract] == nil {
				byID[i.Contract], order = &ProtocolInfo{ID: i.Contract, Providers: []string{}, Consumers: []string{}}, append(order, i.Contract)
			}
			byID[i.Contract].Implementations = append(byID[i.Contract].Implementations, ImplementationInfo{
				ID: runtimeprotocol.ImplementationIdentity(app, i.Name), Title: i.Title, Kind: i.Kind, Origins: slices.Clone(i.Origins), Credential: i.Credential,
				State: r.connectorState(i.Contract, runtimeprotocol.ImplementationIdentity(app, i.Name))})
		}
	}
	out := []ProtocolInfo{}
	for _, id := range order {
		slices.SortFunc(byID[id].Implementations, func(x, y ImplementationInfo) int { return strings.Compare(x.ID, y.ID) })
		if b, ok := r.Bound(id); ok {
			byID[id].Bound = b.ID()
		} else if provider, ok := r.Bindings.get(id); ok {
			if app, name, named := runtimeprotocol.SplitImplementationIdentity(provider); named && slices.ContainsFunc(r.implementations()[app], func(i runtimeprotocol.ProtocolImplementation) bool { return i.Name == name && i.Contract == id }) {
				byID[id].Bound = provider
			}
		}
		out = append(out, *byID[id])
	}
	return out
}

func Explained(err *platform.Error, a platform.Service, schema, target string) *platform.Error {
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

func (r Router) ReadName(protocol, read string) (string, *platform.Error) {
	b, bound := r.Resolve(protocol)
	if !bound {
		return "", &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	name, declared := b.Provision.Reads[read]
	if !declared || !slices.Contains(b.Provision.Protocol.Reads, read) {
		return "", &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return name, nil
}

func (r Router) BoundContracts() map[string]bool {
	out := map[string]bool{}
	for _, info := range r.Protocols() {
		out[info.ID] = info.Bound != "" || !info.Declared && len(info.Providers) > 0
	}
	return out
}
