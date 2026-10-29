package platform

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform/authz"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// ActionInputSchema describes the original action input for model and MCP
// callers. Standard create/edit inputs use the same writable entity fields as
// generated forms, including nested rows and fields with dedicated editors.
// A nil roles slice describes a compiled agent's declared tool; member-facing
// callers pass their original roles. This is a projection, not a validator.
func ActionInputSchema(action Action, entity *EntityInfo, roles []string) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, field := range action.Payload {
		var columns []FieldInfo
		if entity != nil && entity.Type == action.Target && field.Type == "lines" {
			if original, ok := entity.Field(field.Name); ok && original.Type == "lines" {
				columns = original.Fields
			}
		}
		properties[field.Name] = inputFieldSchema(field, columns, roles)
		if field.Required {
			required = append(required, field.Name)
		}
	}
	if entity != nil && action.Target == entity.Type && slices.Contains(entity.Standard, action.Schema) &&
		(action.Schema == entity.Type+".create" || action.Schema == entity.Type+".edit") {
		properties, required = map[string]any{}, []string{}
		create := action.Schema == entity.Type+".create"
		for _, field := range entity.Fields {
			if field.ReadOnly || roles != nil && (!field.ReadsAny(roles) || !field.WritesAny(roles)) {
				continue
			}
			input := Field{Name: field.Name, Type: field.Type, Description: field.Help, Required: create && field.Required && field.When == nil,
				Choices: field.Choices, Ref: field.Ref, Stereotype: field.Stereotype, Stereotypes: field.Stereotypes, Constraints: field.Constraints}
			if i := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == field.Name }); i >= 0 {
				// The action owns requiredness (for example fields filled by its
				// create transaction); entity metadata supplies the full shape.
				original := action.Payload[i]
				input.Required, input.Description, input.Default = original.Required, original.Description, original.Default
				input.From, input.Key, input.Label = original.From, original.Key, original.Label
			}
			properties[field.Name] = inputFieldSchema(input, field.Fields, roles)
			if field.When != nil {
				properties[field.Name].(map[string]any)["x-platform-when"] = field.When
			}
			if input.Required {
				required = append(required, field.Name)
			}
		}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func inputFieldSchema(field Field, columns []FieldInfo, roles []string) map[string]any {
	schema := map[string]any{}
	switch field.Type {
	case "integer":
		schema["type"] = "integer"
	case "number", "decimal":
		schema["type"] = "number"
	case "boolean":
		schema["type"] = "boolean"
	case "string[]", "tags", "references":
		schema["type"], schema["items"] = "array", map[string]any{"type": "string"}
	case "money":
		schema = map[string]any{"type": "object", "properties": map[string]any{"amount": map[string]any{"type": "integer"}, "currency": map[string]any{"type": "string"}}, "required": []string{"amount", "currency"}, "additionalProperties": false}
	case "lines":
		row, required := map[string]any{}, []string{}
		for _, column := range columns {
			if column.ReadOnly || roles != nil && (!column.ReadsAny(roles) || !column.WritesAny(roles)) {
				continue
			}
			row[column.Name] = inputFieldSchema(Field{Name: column.Name, Type: column.Type, Description: column.Help,
				Choices: column.Choices, Ref: column.Ref, Stereotype: column.Stereotype, Stereotypes: column.Stereotypes, Constraints: column.Constraints}, column.Fields, roles)
			if column.When != nil {
				row[column.Name].(map[string]any)["x-platform-when"] = column.When
			}
			if column.Required && column.When == nil {
				required = append(required, column.Name)
			}
		}
		items := map[string]any{}
		if len(columns) > 0 {
			items = map[string]any{"type": "object", "properties": row, "required": required, "additionalProperties": false}
		}
		schema["type"], schema["items"] = "array", items
	case "json": // An opaque JSON value must not be advertised as text.
	default:
		schema["type"] = "string"
	}
	if field.Description != "" {
		schema["description"] = field.Description
	}
	if json.Valid(field.Default) {
		schema["default"] = slices.Clone(field.Default)
	}
	if len(field.Choices) > 0 {
		if schema["type"] == "array" {
			schema["items"].(map[string]any)["enum"] = slices.Clone(field.Choices)
		} else {
			schema["enum"] = slices.Clone(field.Choices)
		}
	}
	if field.Type == "date" && (field.Constraints == nil || !field.Constraints.DateTime) {
		schema["format"] = "date"
	} else if field.Type == "datetime" {
		schema["format"] = "date-time"
	}
	if field.Ref != "" {
		schema["x-platform-reference"] = field.Ref
	}
	if field.Stereotype != "" {
		schema["x-platform-stereotype"] = field.Stereotype
	}
	if len(field.Stereotypes) > 0 {
		schema["x-platform-stereotypes"] = slices.Clone(field.Stereotypes)
	}
	if field.From != "" {
		schema["x-platform-source"] = map[string]string{"read": field.From, "key": field.Key, "label": field.Label}
	}
	if c := field.Constraints; c != nil {
		for _, bound := range []struct {
			name  string
			value *float64
		}{{map[bool]string{false: "minimum", true: "exclusiveMinimum"}[c.ExclusiveMin], c.Min}, {map[bool]string{false: "maximum", true: "exclusiveMaximum"}[c.ExclusiveMax], c.Max}} {
			if bound.value != nil {
				schema[bound.name] = *bound.value
			}
		}
		if c.MinLength != nil {
			schema["minLength"] = *c.MinLength
		}
		if c.MaxLength != nil {
			schema["maxLength"] = *c.MaxLength
		}
		if c.Before != "" || c.After != "" || c.DateTime {
			schema["x-platform-constraints"] = c
		}
	}
	return schema
}

// Action declares one business action a domain offers: the submission
// schema that carries it, what it acts on, the capability it belongs to, and a
// description that screens and automation callers (AI agents included) read.
// Roles say who may call it; attribute conditions stay in the domain's policy.
type Action struct {
	// recordEdit is set only by EntityActions. Its unchanged enterprise values
	// are retained references, not newly selected action parameters.
	recordEdit    bool
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
	// is offered it only when it may call the bound provider's.
	Uses []string `json:"uses,omitempty"`
	// Approval holds the action until its approvers agree.
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
	// ConfirmationOnly hides a receipt transition from ordinary catalogs.
	// It still runs as the initiating member with current permissions.
	ConfirmationOnly bool `json:"confirmationOnly,omitempty"`
	// DeferRequired lets a lifecycle owner return its own business-language
	// message for missing required inputs after the generic type checks.
	DeferRequired bool `json:"-"`
}

// Approval is the chain of approvers an action waits for (–D4):
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
	// requester's calendar instead.
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
	// a picker of the records the member may read (F-36).
	Choices []string `json:"choices,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	// Fields are the columns of a "lines" field: a child table the form edits
	// row by row. Empty for every other type.
	Fields []Field `json:"fields,omitempty"`
	// Stereotype narrows a reference to the enterprise model (Ref
	// "enterprise.element") to one UAF stereotype.
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

// InputConstraints are pure input checks, applied after authorization.
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
	// DateTime allows either a date or a local date-time in a date input.
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
	// generated actions of an object a tenant published) while
	// members read their catalog.
	mu       sync.RWMutex
	actions  []Action
	disabled map[string]bool
	custom   map[string][]string   // tenant-defined roles → the schemas they may call
	policies func() []authz.Policy // the tenant's policies, asked at decision time
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
// tenant defined and published. An action already declared is
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

// PermitsAny reports whether any of roles may call schema (a member with several grants).
func (c *Catalog) PermitsAny(roles []string, schema string) bool {
	return slices.ContainsFunc(c.ForRoles(roles), func(a Action) bool { return a.Schema == schema })
}

// Decide asks the engine whether the caller's roles let them call schema;
// a disabled capability is no action at all. Modern recovery
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
	_, id, _ := strings.Cut(target, "/")
	if a.ConfirmationOnly && !caller.Replaying && !caller.ConfirmationCompletion(a.Target, id, schema) {
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
	if !caller.InScope(schema) { // a personal token reaches only what it was issued for
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

// StaticControlAction classifies the existing control-plane targets. Dynamic
// pages and model tools may read owner projections but cannot bind these writes.
// Ordinary generated business targets are not control-plane targets.
func StaticControlAction(action Action) bool {
	for _, prefix := range []string{"platform.", "system.", "ai.", "enterprise."} {
		if strings.HasPrefix(action.Target, prefix) {
			return true
		}
	}
	switch action.Target {
	case "agent.run", "agent.switch", "agent.evaluation", "agent.memory", "work.task", "platform.work", "platform.effect", "platform.operation", "platform.job",
		"build.object", "build.page", "build.app", "build.flow", "build.agent", "build.function", "build.code", "build.query", "build.process", "build.connection", "build.pipeline", "build.dataset", "build.datasetversion", "build.evaluation", "build.release", "build.packagesource", "build.alertrule":
		return true
	}
	return false
}

// DecisionTrace is an observation of the original executor in an isolated
// builder run. It never participates in acceptance, grants or persistence.
type DecisionTrace struct {
	Kind          string          `json:"kind"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type,omitempty"`
	ID            string          `json:"id,omitempty"`
	Operation     string          `json:"operation,omitempty"`
	Expression    *Expr           `json:"expression,omitempty"`
	Reads         *ExprContext    `json:"reads,omitempty"`
	Result        *ExprResult     `json:"result,omitempty"`
	Request       *AuthzRequest   `json:"request,omitempty"`
	Authorization *AuthzDecision  `json:"authorization,omitempty"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	Refusal       string          `json:"refusal,omitempty"`
}
type DecisionTraceRecorder interface {
	TraceEnabled() bool
	RecordDecisionTrace(DecisionTrace)
}

func (c Caller) TraceEnabled() bool {
	r, ok := c.rt.(DecisionTraceRecorder)
	return ok && r.TraceEnabled()
}
func (c Caller) Trace(event DecisionTrace) {
	if r, ok := c.rt.(DecisionTraceRecorder); ok && r.TraceEnabled() {
		r.RecordDecisionTrace(event)
	}
}

// Only references reached by the real expression belong in its explanation.
// The original RuleContext has already enforced record/field/derived reads.
func ruleTraceContext(expr Expr, context ExprContext) ExprContext {
	out := ExprContext{Refs: map[string]ExprValue{}, Sets: map[string]ExprCollection{}, Sources: context.Sources, Me: context.Me, Now: context.Now, Today: context.Today}
	var visit func(Expr)
	visit = func(e Expr) {
		if e.Ref != "" {
			if v, ok := context.Refs[e.Ref]; ok {
				out.Refs[e.Ref] = v
			}
		}
		if e.Window != nil {
			if out.Windows == nil {
				out.Windows = map[string]ExprWindowSummary{}
			}
			out.Windows[e.Window.Source] = context.Windows[e.Window.Source]
		}
		if e.Agg != nil {
			out.Sets[e.Agg.Source] = context.Sets[e.Agg.Source]
		}
		for _, arg := range e.Args {
			visit(arg)
		}
		if e.In != nil {
			visit(*e.In)
		}
	}
	visit(expr)
	return out
}

var inputInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(\.\d{1,9})?(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$`)

// CheckInputs validates declarations without evaluating business rules.
func CheckInputs(fields []Field) error {
	byName := map[string]Field{}
	for _, f := range fields {
		if _, ok := byName[f.Name]; ok {
			return fmt.Errorf("duplicate input %q", f.Name)
		}
		byName[f.Name] = f
	}
	seenEnds := map[string]bool{}
	for _, f := range fields {
		if len(f.Group) > 128 {
			return fmt.Errorf("%s group exceeds bounds", f.Name)
		}
		c := f.Constraints
		if c != nil {
			if c.DateTime && f.Type != "date" {
				return fmt.Errorf("%s does not take a date or local date-time", f.Name)
			}
			numeric := f.Type == "integer" || f.Type == "number"
			if (c.Min != nil || c.Max != nil) && !numeric {
				return fmt.Errorf("%s has nonnumeric bounds", f.Name)
			}
			if c.Min != nil && (math.IsNaN(*c.Min) || math.IsInf(*c.Min, 0)) || c.Max != nil && (math.IsNaN(*c.Max) || math.IsInf(*c.Max, 0)) {
				return fmt.Errorf("%s has invalid bounds", f.Name)
			}
			if c.ExclusiveMin && c.Min == nil || c.ExclusiveMax && c.Max == nil || c.Min != nil && c.Max != nil && (*c.Min > *c.Max || *c.Min == *c.Max && (c.ExclusiveMin || c.ExclusiveMax)) {
				return fmt.Errorf("%s has empty bounds", f.Name)
			}
			if (c.MinLength != nil || c.MaxLength != nil) && f.Type != "string" {
				return fmt.Errorf("%s has nontext length", f.Name)
			}
			for _, n := range []*int{c.MinLength, c.MaxLength} {
				if n != nil && (*n < 0 || *n > 4096) {
					return fmt.Errorf("%s has invalid length", f.Name)
				}
			}
			if c.MinLength != nil && c.MaxLength != nil && *c.MinLength > *c.MaxLength {
				return fmt.Errorf("%s has empty length bounds", f.Name)
			}
			for _, name := range []string{c.Before, c.After} {
				if name != "" {
					other, ok := byName[name]
					if !ok || name == f.Name || other.Type != f.Type || !slices.Contains([]string{"date", "datetime", "integer", "number"}, f.Type) {
						return fmt.Errorf("%s has invalid comparison %q", f.Name, name)
					}
				}
			}
		}
		if f.Range != nil {
			if seenEnds[f.Range.End] {
				return fmt.Errorf("%s repeats a date range end", f.Name)
			}
			seenEnds[f.Range.End] = true
			other, ok := byName[f.Range.End]
			if !ok || other.Name == f.Name || f.Type != "date" || other.Type != "date" || other.Range != nil {
				return fmt.Errorf("%s has invalid date range", f.Name)
			}
		}
	}
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(name string) bool {
		if state[name] == 1 {
			return false
		}
		if state[name] == 2 {
			return true
		}
		state[name] = 1
		if c := byName[name].Constraints; c != nil {
			for _, dependency := range []string{c.Before, c.After} {
				if dependency != "" && !visit(dependency) {
					return false
				}
			}
		}
		state[name] = 2
		return true
	}
	for name := range byName {
		if !visit(name) {
			return fmt.Errorf("%s has cyclic input dependencies", name)
		}
	}
	return nil
}

// InputIssues uses the exact submitted values. Empty optional edit fields are
// allowed; relation checks wait for both endpoints. Replay bypasses this check.
func InputIssues(fields []Field, values map[string]any) []FieldIssue {
	issues := []FieldIssue{}
	dependencies := map[string]bool{}
	for _, f := range fields {
		if c := f.Constraints; c != nil {
			dependencies[c.Before] = true
			dependencies[c.After] = true
		}
	}
	valid := map[string]bool{}
	add := func(f Field, code, message string, related string) {
		if len(issues) >= 64 {
			return
		}
		issue := FieldIssue{Code: code, Message: message, Path: []string{f.Name}}
		if related != "" {
			issue.RelatedPaths = [][]string{{related}}
		}
		issues = append(issues, issue)
	}
	for _, f := range fields {
		v, ok := values[f.Name]
		text, isText := v.(string)
		empty := !ok || v == nil || isText && strings.TrimSpace(text) == ""
		if empty && f.Required {
			add(f, "required", "Enter a value.", "")
			continue
		}
		c := f.Constraints
		// Unannotated inputs retain their original type and range checks.
		if c == nil {
			if !dependencies[f.Name] {
				continue
			}
			c = &InputConstraints{}
		}
		if empty {
			continue
		}
		valid[f.Name] = true
		bad := false
		switch f.Type {
		case "string":
			bad = !isText
		case "date":
			_, err := time.Parse(time.DateOnly, text)
			bad = !isText || err != nil || strings.HasPrefix(text, "0000-")
			if bad && c.DateTime {
				_, err = time.Parse("2006-01-02T15:04", text)
				bad = !isText || err != nil || strings.HasPrefix(text, "0000-")
			}
		case "datetime":
			_, err := time.Parse(time.RFC3339Nano, text)
			bad = !isText || err != nil || !inputInstant.MatchString(text) || strings.HasPrefix(text, "0000-") || strings.HasSuffix(text, "-00:00")
		case "integer", "number":
			n, yes := v.(float64)
			bad = !yes || math.IsNaN(n) || math.IsInf(n, 0) || f.Type == "integer" && (math.Trunc(n) != n || math.Abs(n) > 9007199254740991)
		case "boolean":
			_, yes := v.(bool)
			bad = !yes
		}
		if bad {
			valid[f.Name] = false
			add(f, "type", "Enter a valid value.", "")
			continue
		}
		if len(f.Choices) > 0 && !slices.Contains(f.Choices, text) {
			add(f, "choice", "Choose a listed value.", "")
		}
		if isText {
			n := len(utf16.Encode([]rune(text)))
			if c.MinLength != nil && n < *c.MinLength {
				add(f, "minLength", fmt.Sprintf("Use at least %d characters.", *c.MinLength), "")
			}
			if c.MaxLength != nil && n > *c.MaxLength {
				add(f, "maxLength", fmt.Sprintf("Use at most %d characters.", *c.MaxLength), "")
			}
		}
		if n, yes := v.(float64); yes {
			if c.Min != nil && (n < *c.Min || c.ExclusiveMin && n == *c.Min) {
				add(f, "min", fmt.Sprintf("Value must be %s %g.", map[bool]string{false: "at least", true: "greater than"}[c.ExclusiveMin], *c.Min), "")
			}
			if c.Max != nil && (n > *c.Max || c.ExclusiveMax && n == *c.Max) {
				add(f, "max", fmt.Sprintf("Value must be %s %g.", map[bool]string{false: "at most", true: "less than"}[c.ExclusiveMax], *c.Max), "")
			}
		}
	}
	for _, f := range fields {
		c := f.Constraints
		if c == nil || !valid[f.Name] {
			continue
		}
		for _, relation := range []struct {
			name   string
			before bool
		}{{c.Before, true}, {c.After, false}} {
			if relation.name == "" || !valid[relation.name] {
				continue
			}
			v, other := values[f.Name], values[relation.name]
			cmp := 0
			switch x := v.(type) {
			case string:
				y := other.(string)
				cmp = strings.Compare(x, y)
				if f.Type == "datetime" {
					a, _ := time.Parse(time.RFC3339Nano, x)
					b, _ := time.Parse(time.RFC3339Nano, y)
					cmp = a.Compare(b)
				}
			case float64:
				y := other.(float64)
				if x < y {
					cmp = -1
				} else if x > y {
					cmp = 1
				}
			}
			if relation.before && cmp > 0 || !relation.before && cmp < 0 || cmp == 0 && !c.Inclusive {
				word := "after"
				if relation.before {
					word = "before"
				}
				if c.Inclusive {
					word += " or equal to"
				}
				add(f, "relation", fmt.Sprintf("Value must be %s %s.", word, relation.name), relation.name)
			}
		}
	}
	return issues
}

// RefuseFields attaches diagnostics to this decision's private runtime; kernel
// errors and the contract remain unchanged.
func (c Caller) RefuseFields(issues []FieldIssue) *kernel.Error {
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Correct the highlighted fields.")
	if c.inputRefused != nil {
		c.inputRefused(err, issues)
	}
	if c.rt != nil {
		c.rt.InputRefused(err, issues)
	}
	return err
}

// Refusal is a business result, separate from authorization and invariants.
// Record is an ordinary transaction step; Route names an existing transaction.
type Refusal struct {
	When         *Expr              `json:"when"`
	Level        string             `json:"level"`
	Code         string             `json:"code"`
	Message      string             `json:"message"`
	Translations map[string]string  `json:"translations,omitempty"`
	Route        *TxnRef            `json:"route,omitempty"`
	Record       *TxnStep           `json:"record,omitempty"`
	Escalation   *RefusalEscalation `json:"escalation,omitempty"`
}

func (r *Refusal) UnmarshalJSON(raw []byte) error {
	type plain Refusal
	return strictTxn(raw, (*plain)(r))
}

// BusinessRefusalFact captures what the applicant asked and why the business
// rule chose another result. Its values are re-filtered by the host on reads.
type BusinessRefusalFact struct {
	Level        string                     `json:"level"`
	Code         string                     `json:"code"`
	Message      string                     `json:"message"`
	Translations map[string]string          `json:"translations,omitempty"`
	Step         string                     `json:"step,omitempty"`
	Subject      string                     `json:"subject"`
	Action       string                     `json:"action"`
	Member       string                     `json:"member"`
	At           time.Time                  `json:"at"`
	Original     json.RawMessage            `json:"original"`
	Inputs       map[string]json.RawMessage `json:"inputs,omitempty"`
	Record       string                     `json:"record,omitempty"`
	Route        *TxnRef                    `json:"route,omitempty"`
	Escalation   *RefusalEscalation         `json:"escalation,omitempty"`
}

// BusinessRefusalRuntime attaches facts to the existing accepted-result owner.
// A refused decision saves its fact with the answer; successful flags/routes
// save facts with the ordinary atomic batch. This never grants a direct write.
type BusinessRefusalRuntime interface {
	BusinessRefused(*kernel.Error, BusinessRefusalFact)
	CaptureBusinessRefusal(Caller, *pb.ChangeRecord, BusinessRefusalFact) *kernel.Error
}

func (c Caller) RefuseBusiness(fact BusinessRefusalFact) *kernel.Error {
	owner, ok := c.rt.(BusinessRefusalRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	answer := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", fact.Message)
	owner.BusinessRefused(answer, fact)
	return answer
}

func (c Caller) CaptureBusinessRefusal(receipt *pb.ChangeRecord, fact BusinessRefusalFact) *kernel.Error {
	owner, ok := c.rt.(BusinessRefusalRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal facts need the accepted decision owner / 业务拒绝事实须经接受决策归属方")
	}
	return owner.CaptureBusinessRefusal(c, receipt, fact)
}

type BusinessConditionRuntime interface {
	RefuseBusinessCondition(Caller, string, string, any, map[string]any, time.Time) *kernel.Error
}

// A false business precondition is separate from access checks made while
// reading the condition. It retains the same original evaluator and savepoint.
func (c Caller) RefuseBusinessCondition(code, message string, record any, inputs map[string]any, at time.Time) *kernel.Error {
	if owner, ok := c.rt.(BusinessConditionRuntime); ok && c.Staging() && !c.Replaying {
		return owner.RefuseBusinessCondition(c, code, message, record, inputs, at)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
}

// RefusalEscalation asks the existing Work owner to open one review task when
// distinct decisions reach Count in a window starting at its first refusal.
// Repeating an idempotency key never contributes another refusal.
type RefusalEscalation struct {
	Count         int    `json:"count"`
	WindowSeconds int    `json:"windowSeconds"`
	ReviewerRole  string `json:"reviewerRole"`
}

func (e *RefusalEscalation) UnmarshalJSON(raw []byte) error {
	type plain RefusalEscalation
	return strictTxn(raw, (*plain)(e))
}

func (e RefusalEscalation) Check() error {
	if e.Count < 2 || e.Count > 100 || e.WindowSeconds < 1 || e.WindowSeconds > 604800 || e.ReviewerRole == "" || len(e.ReviewerRole) > 64 {
		return fmt.Errorf("Refusal escalation needs 2–100 distinct requests, a bounded window and a reviewer role")
	}
	return nil
}

// RefusalEscalationRequest is an immutable business result projected into a
// normal Work task. It contains no submitted inputs or original field values.
type RefusalEscalationRequest struct {
	Key        string    `json:"key"`
	App        string    `json:"app"`
	Subject    string    `json:"subject"`
	Action     string    `json:"action"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Count      int       `json:"count"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
	Candidates []string  `json:"candidates"`
}

func (r RefusalEscalationRequest) TaskID() string { return r.App + ":refusal:" + r.Key }

type RefusalEscalationRuntime interface {
	PendingRefusalEscalations(Caller, time.Time) ([]RefusalEscalationRequest, *kernel.Error)
}

func PendingRefusalEscalations(c Caller, at time.Time) ([]RefusalEscalationRequest, *kernel.Error) {
	rt, ok := c.rt.(RefusalEscalationRuntime)
	if !ok || !c.Staging() || c.Replaying || !c.Automation || c.App != WorkApp || c.ID != "app:"+WorkApp {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal escalation requires the private Work owner")
	}
	return rt.PendingRefusalEscalations(c, at)
}

type BusinessInputChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}
type BusinessRefusalEntry struct {
	Key          string                `json:"key"`
	App          string                `json:"app"`
	Type         string                `json:"type"`
	ID           string                `json:"id"`
	Action       string                `json:"action"`
	Member       string                `json:"member"`
	At           time.Time             `json:"at"`
	Category     string                `json:"category"`
	Code         string                `json:"code"`
	Message      string                `json:"message"`
	State        string                `json:"state"`
	Inputs       json.RawMessage       `json:"inputs,omitempty"`
	Issues       []FieldIssue          `json:"issues,omitempty"`
	Facts        []BusinessRefusalFact `json:"facts,omitempty"`
	CorrectionOf string                `json:"correctionOf,omitempty"`
	Changes      []BusinessInputChange `json:"changes,omitempty"`
}
type BusinessRefusalPage struct {
	Records []BusinessRefusalEntry `json:"records"`
	Total   int                    `json:"total"`
}
type RefusalCodeCount struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Type     string `json:"type"`
	Count    int    `json:"count"`
}

type Delivery struct {
	At         time.Time `json:"at"`
	App        string    `json:"app"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Subscriber string    `json:"subscriber"`
	Outcome    string    `json:"outcome"` // "ok" or an error code
	Attempt    int       `json:"attempt,omitempty"`
}
type AuditEntry struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"` // an action's schema, or "input:<name>"
	Target string    `json:"target,omitempty"`
}
type PersonalRead struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	Type   string    `json:"type"`
	IDs    []string  `json:"ids"`
	Fields []string  `json:"fields"`
}

func EffectSettled(state string) bool {
	return state == "delivered" || state == "rejected" || state == "failed" || state == "discarded" || state == "compensated"
}
