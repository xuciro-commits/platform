package platform

import (
	"encoding/json"
	"maps"
	"platformserver/platform/authz"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Action declares one business action a domain offers (ADR-0057): the submission
// schema that carries it, what it acts on, the capability it belongs to, and a
// description that screens and automation callers (AI agents included) read.
// Roles say who may call it; attribute conditions stay in the domain's policy.
type Action struct {
	Adoption      *ProposalAdoption `json:"adoption,omitempty"`
	SchemaVersion *SchemaVersion    `json:"schemaVersion,omitempty"`
	Schema        string            `json:"schema"`
	Target        string            `json:"target"`
	Capability    string            `json:"capability"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Payload       []Field           `json:"payload"`
	Roles         []string          `json:"-"`
	// Uses names protocol actions this one invokes (ProtocolAction); a caller
	// is offered it only when it may call the bound provider's (ADR-0006).
	Uses []string `json:"uses,omitempty"`
	// Approval holds the action until its approvers agree (ADR-0007).
	Approval *Approval `json:"-"`
	// NeedsApproval tells callers the action waits for approval (set by NewCatalog).
	NeedsApproval bool `json:"needsApproval,omitempty"`
	// New marks an action that makes a new record of Target, whose ID the
	// caller gives: generated views offer it on the type's list; every other
	// action on Target is offered on each record's page (F-33).
	New bool `json:"new,omitempty"`
	// IDPrefix is the suggested prefix for a new record's editable ID.
	IDPrefix string `json:"idPrefix,omitempty"`
	// Automation marks an action only the platform's own apps take, as
	// automation: it has no roles and is in no member's catalog.
	Automation bool `json:"automation,omitempty"`
	// DeferRequired lets a lifecycle owner return its own business-language
	// message for missing required inputs after the generic type checks.
	DeferRequired bool `json:"-"`
}

// Approval is the chain of approvers an action waits for (ADR-0007–D4):
// levels in order; the last approval runs the held submission again, and its
// rules decide at that time.
type Approval struct {
	Levels []ApprovalLevel
	// Pending, on a lifecycle transition, is the state the record waits in
	// while its approvers decide; Rejected is where a rejection leaves it
	// (else where it was). A withdrawn request, or one its rules refuse when
	// run, returns it to where it was (F-38).
	Pending, Rejected string
}

// Suffixes of the actions a transition with a pending state generates for
// the work app alone: the record held, rejected, or returned.
const (
	ApprovalHeld     = ".held"
	ApprovalRejected = ".rejected"
	ApprovalReturned = ".returned"
)

// ApprovalLevel names its approvers: holders of Role in the requester's units
// or above them in Structure (a manager, a department head), holders of
// AppRole in the action's app, or a named Member. All asks every approver;
// otherwise any one decides. When, if set, says whether the level applies to
// this submission (an amount, a number of days). Due is how long its task may
// wait before it escalates.
type ApprovalLevel struct {
	Title           string
	Structure, Role string
	AppRole         string
	Member          string
	All             bool
	When            func(c Caller, s *pb.Submission) bool
	Due             time.Duration
	// WorkingDays, when set, is the due time in working days of the
	// requester's calendar instead (ADR-0038).
	WorkingDays int
}

// Field describes one payload field of an action.
type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
	// Choices are the values the field takes; Ref, the entity type whose
	// record's ID it names. The host refuses others, and forms offer a list or
	// a picker of the records the member may read (ADR-0038, F-36).
	Choices []string `json:"choices,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	// Stereotype narrows a reference to the enterprise model (Ref
	// "enterprise.element") to one UAF stereotype (ADR-0022 D8).
	Stereotype string `json:"stereotype,omitempty"`
	// Stereotypes accepts any of the named enterprise-model families.
	Stereotypes []string `json:"stereotypes,omitempty"`
	// Default initializes a newly opened form; it never overwrites its draft.
	Default json.RawMessage `json:"default,omitempty"`
	// From names a read of the declaring app whose items the field chooses
	// from, when the values are not records here (an ERP's planned orders a
	// plant reads through a protocol): Key is the item's value, Label what
	// people read. Forms offer the list; the app checks the value (#129).
	From        string            `json:"from,omitempty"`
	Key         string            `json:"key,omitempty"`
	Label       string            `json:"label,omitempty"`
	Constraints *InputConstraints `json:"constraints,omitempty"`
	Range       *DateRange        `json:"range,omitempty"`
	Group       string            `json:"group,omitempty"`
	// Manual allows explicit unbound protocol sources, never read failures.
	Manual bool `json:"manual,omitempty"`
}

// InputConstraints are pure input checks, applied after authorization (ADR-0036).
type InputConstraints struct {
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	ExclusiveMin bool     `json:"exclusiveMin,omitempty"`
	ExclusiveMax bool     `json:"exclusiveMax,omitempty"`
	MinLength    *int     `json:"minLength,omitempty"`
	MaxLength    *int     `json:"maxLength,omitempty"`
	Before       string   `json:"before,omitempty"`
	After        string   `json:"after,omitempty"`
	Inclusive    bool     `json:"inclusive,omitempty"`
	// DateTime retains the original lodging date-or-local-time input contract.
	DateTime bool `json:"dateTime,omitempty"`
}

type DateRange struct {
	End       string `json:"end"`
	Inclusive bool   `json:"inclusive,omitempty"`
}

type FieldIssue struct {
	BusinessCode string                     `json:"businessCode,omitempty"`
	Record       string                     `json:"record,omitempty"`
	Constraint   string                     `json:"constraint,omitempty"`
	Translations map[string]string          `json:"translations,omitempty"`
	Values       map[string]json.RawMessage `json:"values,omitempty"`
	Reason       string                     `json:"reason,omitempty"`
	Code         string                     `json:"code"`
	Message      string                     `json:"message"`
	Path         []string                   `json:"path"`
	RelatedPaths [][]string                 `json:"relatedPaths,omitempty"`
}

// InputOptions is a bounded app read; Manual is true only for an unbound source.
type InputOptions struct {
	Manual bool          `json:"manual,omitempty"`
	Items  []InputOption `json:"items"`
}
type InputOption struct {
	ID     string `json:"id" source:"record"`
	Name   string `json:"name" source:"record" from:"Source#name"`
	Unit   string `json:"unit,omitempty" source:"record"`
	Source string `json:"-"`
}

// Catalog holds a domain's actions and which capabilities are deactivated for a
// deployment (start-up configuration, not runtime installation).
type Catalog struct {
	// mu guards both: an app may declare an action after composition (the
	// generated actions of an object a tenant published, ADR-0048) while
	// members read their catalog.
	mu       sync.RWMutex
	actions  []Action
	disabled map[string]bool
	custom   map[string][]string   // tenant-defined roles → the schemas they may call (ADR-0027 §3.3)
	policies func() []authz.Policy // the tenant's policies, asked at decision time (ADR-0027 §3.4)
}

// DefineRole declares a tenant's custom role: a name and the enabled actions
// it may call. Declaring it again replaces it; no schemas removes it.
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

// UsePolicies gives the catalog the tenant's policies to decide with.
func (c *Catalog) UsePolicies(policies func() []authz.Policy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.policies = policies
}

func (c *Catalog) AuthorizationPolicies() []authz.Policy {
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

// WithActions selects immutable declarations while retaining today's tenant
// controls. A retained execution cannot restore revoked roles or capabilities.
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

// Disable deactivates a capability; false if no action belongs to it.
func (c *Catalog) Disable(capability string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.ContainsFunc(c.actions, func(a Action) bool { return a.Capability == capability }) {
		return false
	}
	c.disabled[capability] = true
	return true
}

// Add declares actions after composition: the generated actions of an object a
// tenant defined and published (ADR-0048). An action already declared is
// replaced, so publishing an object again keeps one declaration of each.
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

// ReplaceTarget installs one owner's complete object action set. Retired
// schemas stay in the ledger's historical registry, outside the live catalog.
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

// All are the declared actions, active or not.
func (c *Catalog) All() []Action {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.actions)
}

// Action is the declaration of schema, active or not.
func (c *Catalog) Action(schema string) (Action, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	if i < 0 {
		return Action{}, false
	}
	return c.actions[i], true
}

// Enabled reports whether schema is a declared action of an active capability.
func (c *Catalog) Enabled(schema string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	return i >= 0 && !c.disabled[c.actions[i].Capability]
}

// AnyMember in an action's Roles offers it to every member; the app's attribute
// conditions then decide (the platform's links and timeline check the entities' apps).
const AnyMember = "*"

// Permits reports whether role may call the enabled action schema.
func (c *Catalog) Permits(role, schema string) bool {
	return slices.ContainsFunc(c.For(role), func(a Action) bool { return a.Schema == schema })
}

// PermitsAny reports whether any of roles may call schema (a member with several grants, ADR-0027).
func (c *Catalog) PermitsAny(roles []string, schema string) bool {
	return slices.ContainsFunc(c.ForRoles(roles), func(a Action) bool { return a.Schema == schema })
}

// Decide asks the engine whether the caller's roles let them call schema
// (ADR-0027 §3.4); a disabled capability is no action at all. Modern recovery
// applies saved results, while owner automation still passes the engine.
func (c *Catalog) Decide(caller Caller, schema string) authz.Verdict {
	return c.DecideOn(caller, schema, "")
}

// DecideOn decides schema on a target ("<type>/<id>"), which policies may
// read as attributes: member, app, agent, target type.
func (c *Catalog) DecideOn(caller Caller, schema, target string) authz.Verdict {
	if _, ok := caller.rt.(AuthorizationRuntime); ok && !caller.Replaying {
		typ, id, _ := strings.Cut(target, "/")
		kind := "action"
		if strings.HasSuffix(schema, ".create") {
			kind = "create"
		}
		return caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: kind, Type: typ, Record: id, Action: schema}, time.Time{})
	}
	request, policies, ok := c.AuthorizationRequest(caller, schema, target)
	if !ok {
		return authz.Verdict{Rule: "none", Reason: "no action " + schema, Layers: []authz.Layer{}}
	}
	return authz.Decide(request, policies...)
}

// DecideSubmission includes the fields supplied to generated record writes.
// Custom actions retain their declaration's operation contract.
func (c *Catalog) DecideSubmission(caller Caller, s *pb.Submission, at time.Time) authz.Verdict {
	if _, hosted := caller.rt.(AuthorizationRuntime); !hosted || caller.Replaying {
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

// AuthorizationRequest resolves declaration facts without making a decision.
func (c *Catalog) AuthorizationRequest(caller Caller, schema, target string) (authz.Request, []authz.Policy, bool) {
	a, ok := c.Action(schema)
	if !ok || !c.Enabled(schema) {
		return authz.Request{}, nil, false
	}
	allowed := slices.Clone(a.Roles)
	for i, r := range allowed {
		if r == AnyMember {
			allowed[i] = authz.AnyMember
		}
	}
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
	var rules []authz.Policy
	if policies != nil {
		rules = policies()
	}
	if !caller.InScope(schema) { // a personal token reaches only what it was issued for (ADR-0028 §5)
		rules = append([]authz.Policy{{ID: "token-scope", Effect: "deny", Permission: schema}}, rules...)
	}
	return authz.Request{Subject: authz.Subject{ID: caller.ID, App: caller.App, Roles: caller.RolesHere(), Agent: caller.Agent, Automation: caller.Automation},
		Permission: schema, Resource: target, Allowed: allowed, Attributes: attrs}, rules, true
}

// For is the catalog a caller with role receives: enabled actions it may call.
func (c *Catalog) For(role string) []Action { return c.ForRoles([]string{role}) }

// ForRoles is the catalog of a caller holding roles: the union of what each may call.
func (c *Catalog) ForRoles(roles []string) []Action {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Action{}
	for _, a := range c.actions {
		if c.disabled[a.Capability] {
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

// Roles are the roles an app defines: every role some action grants.
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

// Custom reports whether role is one the tenant defined.
func (c *Catalog) Custom(role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.custom[role]
	return ok
}

// CapabilityInfo is one capability of an app: its actions and whether it is active.
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
