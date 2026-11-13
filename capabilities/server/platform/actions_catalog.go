package platform

import (
	"encoding/json"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"strings"
	"sync"
	"time"
)

type Catalog struct {
	mu       sync.RWMutex
	actions  []Action
	disabled map[string]bool
	custom   map[string][]string
	policies func() []AuthorizationPolicy
}

func (c *Catalog) DefineRole(role string, schemas []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.custom == nil {
		c.custom = map[string][]string{}
	}
	if len(schemas) == 0 {
		delete(c.custom, role)
		return
	}
	c.custom[role] = slices.Clone(schemas)
}

func (c *Catalog) UsePolicies(policies func() []AuthorizationPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.policies = policies
}

func (c *Catalog) AuthorizationPolicies() []AuthorizationPolicy {
	c.mu.RLock()
	lookup := c.policies
	c.mu.RUnlock()
	if lookup == nil {
		return nil
	}
	return lookup()
}

func NewCatalog(actions ...Action) *Catalog {
	for i := range actions {
		if err := CheckInputs(actions[i].Payload); err != nil {
			panic(err)
		}
		if actions[i].SchemaVersion != nil {
			if err := actions[i].SchemaVersion.Check(); err != nil {
				panic(err)
			}
		}
		actions[i].NeedsApproval = actions[i].Approval != nil
	}
	return &Catalog{actions: actions, disabled: map[string]bool{}}
}

func (c *Catalog) WithActions(actions []Action) *Catalog {
	bound := NewCatalog(actions...)
	c.mu.RLock()
	defer c.mu.RUnlock()
	bound.policies = c.policies
	bound.disabled = maps.Clone(c.disabled)
	bound.custom = make(map[string][]string, len(c.custom))
	for role, schemas := range c.custom {
		bound.custom[role] = slices.Clone(schemas)
	}
	return bound
}

func (c *Catalog) Disable(capability string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.ContainsFunc(c.actions, func(a Action) bool { return a.Capability == capability }) {
		return false
	}
	c.disabled[capability] = true
	return true
}

func (c *Catalog) Add(actions ...Action) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range actions {
		if err := CheckInputs(a.Payload); err != nil {
			panic(err)
		}
		a.NeedsApproval = a.Approval != nil
		if i := slices.IndexFunc(c.actions, func(x Action) bool { return x.Schema == a.Schema }); i >= 0 {
			c.actions[i] = a
			continue
		}
		c.actions = append(c.actions, a)
	}
}

func (c *Catalog) ReplaceTarget(target string, actions []Action) {
	for _, action := range actions {
		if action.Target != target {
			panic("replacement action belongs to another target")
		}
		if err := CheckInputs(action.Payload); err != nil {
			panic(err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actions = slices.DeleteFunc(c.actions, func(action Action) bool { return action.Target == target })
	for _, action := range actions {
		action.NeedsApproval = action.Approval != nil
		c.actions = append(c.actions, action)
	}
}

func (c *Catalog) All() []Action {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.actions)
}

func (c *Catalog) Action(schema string) (Action, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	if i < 0 {
		return Action{}, false
	}
	return c.actions[i], true
}

func (c *Catalog) Enabled(schema string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	return i >= 0 && !c.disabled[c.actions[i].Capability]
}

const AnyMember = "*"

func (c *Catalog) PermitsAny(roles []string, schema string) bool {
	return slices.ContainsFunc(c.ForRoles(roles), func(a Action) bool { return a.Schema == schema })
}

func (c *Catalog) Decide(caller Caller, schema string) AuthzDecision {
	return c.DecideOn(caller, schema, "")
}

func (c *Catalog) DecideOn(caller Caller, schema, target string) AuthzDecision {
	if caller.rt != nil && !caller.Replaying {
		typ, id, _ := strings.Cut(target, "/")
		kind := "action"
		if strings.HasSuffix(schema, ".create") {
			kind = "create"
		}
		return caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: kind, Type: typ, Record: id, Action: schema}, time.Time{})
	}
	request, policies, ok := c.AuthorizationRequest(caller, schema, target)
	if !ok {
		return AuthzDecision{Rule: "none", Reason: "no action " + schema, Layers: []AuthzLayer{}}
	}
	return caller.AuthorizationEngine().Decide(request, policies...)
}

func (c *Catalog) DecideSubmission(caller Caller, s *pb.Submission, at time.Time) AuthzDecision {
	if caller.rt == nil || caller.Replaying {
		return c.DecideOn(caller, s.GetSchema().GetName(), s.GetTarget().GetType()+"/"+s.GetTarget().GetId())
	}
	typ, schema := s.GetTarget().GetType(), s.GetSchema().GetName()
	kind := "action"
	fields := []string{}
	if schema == typ+".create" || schema == typ+".edit" {
		kind = "write"
		if schema == typ+".create" {
			kind = "create"
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(s.GetPayload(), &payload) == nil {
			for field := range payload {
				fields = append(fields, field)
			}
			slices.Sort(fields)
		}
	} else if schema == typ+".archive" {
		kind = "archive"
	}
	return caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: kind, Type: typ, Record: s.GetTarget().GetId(), Fields: fields, Action: schema}, at)
}

func (c *Catalog) AuthorizationRequest(caller Caller, schema, target string) (AuthorizationInput, []AuthorizationPolicy, bool) {
	a, ok := c.Action(schema)
	if !ok || !c.Enabled(schema) {
		return AuthorizationInput{}, nil, false
	}
	_, id, _ := strings.Cut(target, "/")
	if a.ConfirmationOnly && !caller.Replaying && !caller.ConfirmationCompletion(a.Target, id, schema) {
		return AuthorizationInput{}, nil, false
	}
	allowed := slices.Clone(a.Roles)
	c.mu.RLock()
	for role, schemas := range c.custom {
		if slices.Contains(schemas, schema) {
			allowed = append(allowed, role)
		}
	}
	policies := c.policies
	c.mu.RUnlock()
	attrs := map[string]string{"member": caller.ID, "app": caller.App, "target": a.Target}
	if caller.Agent {
		attrs["agent"] = "true"
	}
	var rules []AuthorizationPolicy
	if policies != nil {
		rules = policies()
	}
	if !caller.InScope(schema) {
		rules = append([]AuthorizationPolicy{{ID: "token-scope", Effect: "deny", Permission: schema}}, rules...)
	}
	return AuthorizationInput{Subject: AuthorizationSubject{ID: caller.ID, App: caller.App, Roles: caller.RolesHere(), Agent: caller.Agent, Automation: caller.Automation},
		Permission: schema, Resource: target, Allowed: allowed, Attributes: attrs}, rules, true
}

func (c *Catalog) For(role string) []Action { return c.ForRoles([]string{role}) }

func (c *Catalog) ForRoles(roles []string) []Action {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Action{}
	for _, a := range c.actions {
		if c.disabled[a.Capability] || a.ConfirmationOnly {
			continue
		}
		if slices.Contains(a.Roles, AnyMember) || slices.ContainsFunc(roles, func(r string) bool {
			return r != "" && (slices.Contains(a.Roles, r) || slices.Contains(c.custom[r], a.Schema))
		}) {
			out = append(out, a)
		}
	}
	return out
}

func (c *Catalog) Roles() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string
	for _, a := range c.actions {
		for _, r := range a.Roles {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	for r := range c.custom {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

func (c *Catalog) Custom(role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.custom[role]
	return ok
}

type CapabilityInfo struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Actions []string `json:"actions"`
}

func (c *Catalog) Capabilities() []CapabilityInfo {
	var out []CapabilityInfo
	for _, a := range c.actions {
		i := slices.IndexFunc(out, func(x CapabilityInfo) bool { return x.Name == a.Capability })
		if i < 0 {
			out = append(out, CapabilityInfo{Name: a.Capability, Enabled: !c.disabled[a.Capability]})
			i = len(out) - 1
		}
		out[i].Actions = append(out[i].Actions, a.Schema)
	}
	return out
}
