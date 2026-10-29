package platformserver

import (
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"

	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"
)

// Sealing retains each distinct authorization basis and outcome in first-use
// order. Repeated reads still evaluate current permissions at every call.
func (d *stagedDecision) savedAuthorizations() ([]acceptedformat.AuthorizationEvidence, error) {
	var saved []acceptedformat.AuthorizationEvidence
	seen := map[string]bool{}
	for _, item := range d.authorizations {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		key := string(raw)
		if !seen[key] {
			seen[key] = true
			saved = append(saved, item)
		}
	}
	return saved, nil
}

func authorizationPass(reason string) authz.Gate { return authz.Gate{Allowed: true, Reason: reason} }

func (r runtime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	if at.IsZero() {
		at = Now()
	}
	decision := r.t.authorizeFrom(r.t.records, c, q, at, false)
	return decision
}
func (d *stagedDecision) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	c.Member = d.tenant.authorizationMember(c.Member)
	if at.IsZero() {
		at = d.at
	}
	decision := d.tenant.authorizeFrom(d.records, c, q, at, false)
	app := c.App
	if owner := d.tenant.owner["action:"+q.Action]; owner != nil {
		app = owner.Manifest().ID
	} else if et := d.records.types[q.Type]; et != nil {
		app = et.info.App
	}
	d.authorizations = append(d.authorizations, acceptedformat.AuthorizationEvidence{Request: q, Decision: decision, Roles: slices.Clone(c.RolesIn(app))})
	return decision
}

func (d *stagedDecision) RecordAuthorization(c platform.Caller, q platform.AuthzRequest, decision platform.AuthzDecision) {
	c.Member = d.tenant.authorizationMember(c.Member)
	d.authorizations = append(d.authorizations, acceptedformat.AuthorizationEvidence{Request: q, Decision: decision, Roles: slices.Clone(c.RolesHere())})
	if d.TraceEnabled() {
		d.RecordDecisionTrace(platform.DecisionTrace{Kind: "authorization", Request: &q, Authorization: &decision})
	}
}

// Native entry facts are host declarations, not an authorization bypass.
// They retain the same deny precedence and nine layers as record operations.
type authorizationDetails struct {
	Store      *recordStore
	At         time.Time
	Sources    []string
	Definition string
}

func (t *Tenant) authorizeNative(c platform.Caller, permission, kind string, allowed []string, declared bool, details ...authorizationDetails) platform.AuthzDecision {
	c.Member = t.authorizationMember(c.Member)
	pass := authorizationPass("not restricted by this owner")
	context := &authz.Context{Tenant: authz.Gate{Allowed: c.Tenant == t.ID, Reason: "The operation belongs to another tenant"}, Principal: authz.Gate{Allowed: c.ID != "" && c.Active(), Reason: "The operation principal is invalid or inactive"}, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	if current, known := t.member(c.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	if c.Agent && t.suspended(c.ID) {
		context.Principal.Allowed = false
	}
	owner := t.app(c.App)
	if owner == nil || !declared || !t.ownerActive(c.App) {
		context.Verb = &authz.Gate{Reason: "The operation is unavailable in its owner declaration"}
	}
	var policies []authz.Policy
	if owner != nil {
		policies = owner.Manifest().Actions.AuthorizationPolicies()
		context.Definition, _ = platform.CanonicalDigest([]any{c.App, owner.Manifest().Version, permission, allowed})
	}
	if len(details) > 0 {
		detail := details[0]
		if detail.Definition != "" {
			context.Definition = detail.Definition
		}
		store := detail.Store
		if store == nil {
			store = t.records
		}
		at := detail.At
		if at.IsZero() {
			at = Now()
		}
		walk := newAuthorizationWalk()
		for _, source := range detail.Sources {
			if name, named := strings.CutPrefix(source, "read:"); named {
				if !t.mayCallRead(c.Member, name) {
					context.Derived = authz.Gate{Reason: "The original source is outside the caller's permissions"}
					break
				}
				continue
			}
			ref, field, _ := strings.Cut(source, "#")
			typ, id, valid := strings.Cut(ref, "/")
			fields := []string{}
			if field != "" {
				fields = append(fields, field)
			}
			decision := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, at, false, 0, walk)
			if !valid || id == "" || !decision.Allow || len(decision.Withheld) > 0 {
				context.Derived = authz.Gate{Reason: "The original source is outside the caller's permissions"}
				break
			}
		}
	}
	return t.services.Authorization.Decide(authz.Request{Kind: kind, Subject: authz.Subject{ID: c.ID, App: c.App, Roles: c.RolesHere(), Agent: c.Agent, Automation: c.Automation}, Permission: permission, Allowed: allowed, Attributes: map[string]string{"member": c.ID, "app": c.App}, Context: context}, policies...)
}

func (t *Tenant) authorizeCapability(c platform.Caller, owner, permission string, allowed []string, declared bool, detail authorizationDetails) platform.AuthzDecision {
	c = platform.RouteCaller(c, owner)
	c.Automation = false // a saved applicant's roles authorize this computation
	decision := t.authorizeNative(c, permission, "action", allowed, declared, detail)
	if draft := c.AuthorizationRecorder(); draft != nil {
		draft.RecordAuthorization(c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Action: permission}, decision)
	}
	return decision
}

func (d *stagedDecision) authorizeNative(c platform.Caller, permission, kind string, allowed []string, declared bool) *kernel.Error {
	decision := d.tenant.authorizeNative(c, permission, kind, allowed, declared)
	d.authorizations = append(d.authorizations, acceptedformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: kind, Action: permission}, Decision: decision, Roles: slices.Clone(c.RolesHere())})
	return authorizationRefusal(decision)
}

func authorizationRefusal(decision platform.AuthzDecision) *kernel.Error {
	if decision.Allow {
		return nil
	}
	layer := "verb"
	if len(decision.Layers) > 0 {
		layer = decision.Layers[len(decision.Layers)-1].Name
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", layer, decision.Reason)
}

func (t *Tenant) authorizeEntry(m platform.Member, app, permission, kind string, at time.Time) platform.AuthzDecision {
	m = t.authorizationMember(m)
	pass := authorizationPass("not restricted by this owner")
	context := &authz.Context{Tenant: authz.Gate{Allowed: m.Tenant == t.ID, Reason: "The operation belongs to another tenant"}, Principal: authz.Gate{Allowed: m.ID != "" && m.Active(), Reason: "The operation principal is invalid or inactive"}, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	if current, known := t.member(m.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	allowed := []string{}
	var policies []authz.Policy
	if owner := t.app(app); owner != nil {
		allowed = owner.Manifest().AllRoles()
		if kind == "read" && slices.Contains(owner.Manifest().Everyone, permission) {
			allowed = []string{authz.AnyMember}
		}
		policies = owner.Manifest().Actions.AuthorizationPolicies()
		context.Definition, _ = platform.CanonicalDigest([]any{app, owner.Manifest().Version, permission, kind, allowed})
		if name, input := strings.CutPrefix(permission, "input:"); input {
			t.opsMu.Lock()
			connector := t.connectors.byID[m.ID]
			if connector != nil && connector.GetTenantId() == t.ID && (name == "heartbeat" || slices.ContainsFunc(connector.GetDataClasses(), func(class string) bool {
				return strings.HasPrefix(class, app+".")
			})) {
				allowed = []string{authz.AnyMember}
			}
			t.opsMu.Unlock()
		}
	}
	return t.services.Authorization.Decide(authz.Request{Kind: kind, Subject: authz.Subject{ID: m.ID, App: app, Roles: m.RolesIn(app), Agent: m.Agent}, Permission: permission, Allowed: allowed, Attributes: map[string]string{"member": m.ID, "app": app}, Context: context}, policies...)
}

// Retain caller/token narrowing while dropping roles revoked in the directory.
// Service and application-agent principals are declared by their own owners.
func (t *Tenant) authorizationMember(m platform.Member) platform.Member {
	current, known := t.member(m.ID)
	if !known {
		declared := t.identity == nil || m.ID == "host:maintenance"
		if app, service := strings.CutPrefix(m.ID, "app:"); service {
			declared = t.app(app) != nil
		}
		if agent, generated := strings.CutPrefix(m.ID, "agent:"); generated && t.agents != nil {
			_, declared = t.agents.AgentDeclaration(agent)
		}
		if !declared {
			m.Roles, m.Grants, m.Status = map[string]string{}, nil, platform.MemberLeft
		}
		return m
	}
	roles := map[string]string{}
	for app, role := range current.Roles {
		if m.Holds(app, role) {
			roles[app] = role
		}
	}
	grants := slices.DeleteFunc(slices.Clone(current.Grants), func(grant platform.Grant) bool { return !m.Holds(grant.App, grant.Role) })
	m.Roles, m.Grants, m.Status = roles, grants, current.Status
	return m
}

// authorizeFrom is the host's one fact resolver. Scope implementations remain
// with the record owner, but every result passes the engine's fixed order.
// held belongs to read projections and avoids reacquiring the record lock.
func (t *Tenant) authorizeFrom(store *recordStore, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool) (decision platform.AuthzDecision) {
	if t.simulationTrace != nil {
		defer func() {
			t.simulationTrace(platform.DecisionTrace{Kind: "authorization", Request: &q, Authorization: &decision})
		}()
	}
	return t.authorizeDepth(store, c, q, at, held, 0, newAuthorizationWalk())
}

type authorizationWalk struct {
	decisions map[string]platform.AuthzDecision
	active    map[string]bool
}

func newAuthorizationWalk() *authorizationWalk {
	return &authorizationWalk{decisions: map[string]platform.AuthzDecision{}, active: map[string]bool{}}
}

func (t *Tenant) authorizeDepth(store *recordStore, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool, depth int, walk *authorizationWalk) platform.AuthzDecision {
	c.Member = t.authorizationMember(c.Member)
	key := string(platform.Raw(q))
	if decision, known := walk.decisions[key]; known && depth < 16 {
		return decision
	}
	cycle := walk.active[key]
	if !cycle {
		walk.active[key] = true
		defer delete(walk.active, key)
	}
	pass := authorizationPass("not restricted by this owner")
	context := &authz.Context{Tenant: pass, Principal: pass, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	context.Tenant = authz.Gate{Allowed: q.Tenant == t.ID && c.Tenant == t.ID, Reason: "The operation belongs to another tenant"}
	context.Principal = authz.Gate{Allowed: c.ID != "" && q.Principal == c.ID && c.Active(), Reason: "The operation principal is invalid or inactive"}
	if current, known := t.member(c.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	if depth >= 16 || cycle {
		context.Derived = authz.Gate{Reason: "The declared source traversal exceeds 16 segments or contains a cycle"}
	}
	request := authz.Request{Kind: q.Kind, Subject: authz.Subject{ID: c.ID, App: c.App, Roles: c.RolesHere(), Agent: c.Agent, Automation: c.Automation}, Permission: q.Action, Resource: q.Type + "/" + q.Record, Context: context}
	var policies []authz.Policy
	if q.Action != "" {
		context.Verb = &authz.Gate{Reason: "The operation is unavailable in its owner declaration"}
		owner := t.owner["action:"+q.Action]
		if owner != nil {
			facts, rules, ok := owner.Manifest().Actions.AuthorizationRequest(platform.RouteCaller(c, owner.Manifest().ID), q.Action, request.Resource)
			if ok {
				request.Subject, request.Allowed, request.Attributes, policies = facts.Subject, facts.Allowed, facts.Attributes, rules
				context.Verb = nil
			}
		}
	}
	var et *entityType
	var current *records.Row
	func() {
		if !held {
			store.mu.Lock()
			defer store.mu.Unlock()
		}
		et = store.types[q.Type]
		if et != nil {
			current = et.rows.Get(q.Record)
		}
	}()
	if q.Action == "" {
		if et != nil {
			request.Subject.App = et.info.App
			request.Subject.Roles = c.RolesIn(et.info.App)
			request.Allowed = slices.Clone(request.Subject.Roles)
			request.Permission = et.info.Type + "." + q.Kind
			if q.Kind == "derive" {
				request.Permission = et.info.Type + ".read"
			}
			request.Attributes = map[string]string{"member": c.ID, "app": et.info.App, "target": et.info.Type}
			if owner := t.app(et.info.App); owner != nil {
				policies = owner.Manifest().Actions.AuthorizationPolicies()
			}
			if et.info.Scope.Participants != nil || et.info.Scope.Through != nil {
				request.Allowed = []string{authz.AnyMember}
			}
			if et.info.Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				request.Allowed = []string{authz.AnyMember}
			}
		} else {
			request.Allowed = slices.Clone(c.RolesHere())
		}
	}
	if et != nil {
		context.Definition = et.info.Type
		if digest, err := platform.CanonicalDigest(struct {
			Fields                          []platform.FieldInfo
			Derived                         []platform.DerivationInfo
			Withheld                        []int
			Policies                        []authz.Policy
			Default, Owner, Unit, Structure string
			Levels                          map[string]string
		}{et.info.Fields, et.info.Derived, et.info.Withheld, policies, et.info.Scope.Default, et.info.Scope.Owner, et.info.Scope.Unit, et.info.Scope.Structure, et.info.Scope.Levels}); err == nil {
			context.Definition = digest
		}
		if q.Record != "" && q.Kind != "create" && (q.Kind != "action" || current != nil) {
			if c.Automation && c.App == et.info.App {
				context.Row = authorizationPass("the declared owner service boundary")
			} else if et.info.Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				context.Row = authz.Gate{Allowed: t.knowledgeSourceReadable(store, c.Member, q.Record, ""), Reason: "The document is outside the caller's source grants"}
			} else if through := et.info.Scope.Through; through != nil {
				if current != nil && depth < 16 && !cycle {
					typ, id, _ := strings.Cut(through(current.Value.Interface()), "/")
					parent := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, at, true, depth+1, walk)
					context.Row = authz.Gate{Allowed: parent.Allow, Reason: "The owning record is outside the caller's row scope"}
				} else {
					context.Row = authz.Gate{Reason: "The owning record is unavailable or exceeds 16 segments"}
				}
			} else {
				visible, err := records.ScopeVisibility(et.info, c.Member, t.directory, at)
				context.Row = authz.Gate{Allowed: current != nil && err == nil && (visible == nil || visible(current.Value)), Reason: "The record is outside the caller's row scope"}
			}
		}
		for _, name := range q.Fields {
			field, known := et.info.Field(name)
			if q.Action != "" && (q.Kind == "write" || q.Kind == "create") && !slices.Contains(et.info.Standard, q.Action) {
				// A typed owner action may intentionally accept a blind input.
				// read/write tags define the generated form contract, not its payload.
				context.Fields.Reason = "The typed owner action declares its accepted input fields"
				continue
			}
			if !known && q.Action != "" {
				continue
			} // payload validation owns unknown keys
			allowed := known && field.ReadsAny(c.RolesIn(et.info.App)) && !(c.AIContext && field.AIInput != nil && !*field.AIInput)
			if et.info.Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				allowed = t.knowledgeSourceReadable(store, c.Member, q.Record, name)
			}
			if q.Kind == "write" || q.Kind == "create" {
				if field.ReadOnly {
					continue
				} // immutable shape is an input validation failure
				allowed = known && field.WritesAny(c.RolesIn(et.info.App))
				if c.Automation && c.App == et.info.App {
					allowed = true
				}
			}
			if !allowed {
				if slices.Contains([]string{"read", "derive", "export"}, q.Kind) {
					context.Withheld = append(context.Withheld, name)
				} else {
					context.Fields = authz.Gate{Reason: "An involved field is outside the caller's field permissions"}
				}
			}
		}
		if current != nil && depth < 16 && !cycle && slices.Contains([]string{"read", "derive", "export"}, q.Kind) {
			context.Withheld = append(context.Withheld, t.sourceWithheld(store, c, et, current.Value, q.Fields, at, depth, walk)...)
		}
	} else if q.Action == "" {
		if q.Type == "enterprise.element" && t.directory != nil && q.Record != "" {
			request.Allowed = []string{authz.AnyMember}
			_, exists := t.directory.Element(q.Record, at.UTC().Format(time.DateOnly))
			context.Row = authz.Gate{Allowed: exists, Reason: "The enterprise source is unavailable"}
		} else {
			context.Verb = &authz.Gate{Reason: "The record type is unavailable"}
		}
	}
	if q.From != "" {
		ref, field, _ := strings.Cut(q.From, "#")
		readable := false
		func() {
			if !held {
				store.mu.Lock()
				defer store.mu.Unlock()
			}
			typ, id, _ := strings.Cut(ref, "/")
			fields := []string{}
			if field != "" {
				fields = append(fields, field)
			}
			if depth < 16 && !cycle {
				source := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, at, true, depth+1, walk)
				readable = source.Allow && len(source.Withheld) == 0
			}
		}()
		context.Derived = authz.Gate{Allowed: readable, Reason: "The original source is outside the caller's permissions"}
	}
	if q.Kind == "export" && t.restricted(q.Type) {
		context.Markings = authz.Gate{Reason: "Restricted data does not leave as CSV"}
	}
	slices.Sort(context.Withheld)
	context.Withheld = slices.Compact(context.Withheld)
	if len(context.Withheld) > 0 {
		context.Fields.Reason = "Some fields are withheld by field permissions or source grants"
		context.Derived.Reason = "Derived content is limited to readable declared sources"
	}
	decision := t.services.Authorization.Decide(request, policies...)

	if !cycle && depth < 16 {
		walk.decisions[key] = decision
	}
	return decision
}

// sourceWithheld resolves declared provenance before a value is projected.
// A list is reported by its public field name, never by a hidden source ID.
func (t *Tenant) sourceWithheld(store *recordStore, c platform.Caller, et *entityType, value reflect.Value, fields []string, at time.Time, depth int, walk *authorizationWalk) []string {
	w := narrower{store: store, m: c.Member, held: true}
	all := []string{}
	for _, d := range et.info.Derived {
		if !d.All {
			all = append(all, w.sources(value, d)...)
		}
	}
	withheld := []string{}
	for _, alias := range et.info.Fields {
		if alias.AliasOf == "" || len(fields) > 0 && !slices.Contains(fields, alias.Name) {
			continue
		}
		request := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: et.info.Type, Record: records.Header(value).ID, Fields: []string{alias.AliasOf}}
		grant := t.authorizeDepth(store, c, request, at, true, depth+1, walk)
		if !grant.Allow || len(grant.Withheld) > 0 {
			withheld = append(withheld, alias.Name)
		}
	}
	for _, d := range et.info.Derived {
		names := []string{}
		if len(d.List) > 0 {
			names = append(names, fieldName(et.info.Go, d.List))
		} else {
			for _, path := range d.Fields {
				names = append(names, fieldName(et.info.Go, path))
			}
		}
		if len(fields) > 0 && !slices.ContainsFunc(names, func(name string) bool { return slices.Contains(fields, name) }) {
			continue
		}
		refs := all
		if !d.All {
			refs = w.sources(value, d)
		}
		denied := len(refs) == 0 && slices.ContainsFunc(names, func(name string) bool {
			field, known := et.info.Field(name)
			return known && field.SourceType == "generated" && !value.FieldByIndex(field.Index).IsZero()
		})
		for _, ref := range refs {
			// Collection/formula witnesses retain their own owner identity.
			// Its row scope was already checked; it is not a copied source field.
			if ref == et.info.Type+"/"+records.Header(value).ID {
				continue
			}
			if name, named := strings.CutPrefix(ref, "read:"); named {
				owner := t.owner["read:"+name]
				denied = denied || owner == nil || !t.authorizeEntry(c.Member, owner.Manifest().ID, name, "read", at).Allow
				continue
			}
			record, field, _ := strings.Cut(ref, "#")
			typ, id, _ := strings.Cut(record, "/")
			sourceFields := []string{}
			if field != "" {
				sourceFields = append(sourceFields, field)
			}
			source := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: sourceFields}, at, true, depth+1, walk)
			denied = denied || !source.Allow || len(source.Withheld) > 0
			if denied {
				break
			}
		}
		if denied {
			withheld = append(withheld, names...)
		}
	}
	return withheld
}

// visibleIn supplies the original query engine with a per-record decision.
func (t *Tenant) visibleIn(store *recordStore, m platform.Member, et *entityType, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleForCallerIn(store, platform.NewCaller(runtime{t}, m, et.info.App, false, false), et, at)
}

func (t *Tenant) visibleForCallerIn(store *recordStore, c platform.Caller, et *entityType, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.info.Type}
	if refusal := authorizationRefusal(t.authorizeFrom(store, c, question, at, true)); refusal != nil {
		return nil, refusal
	}
	cache := map[string]platform.AuthzDecision{}
	walk := newAuthorizationWalk()
	return func(value reflect.Value) bool {
		id := records.Header(value).ID
		if result, ok := cache[id]; ok {
			return result.Allow
		}
		question.Record = id
		result := t.authorizeDepth(store, c, question, at, true, 0, walk)
		cache[id] = result
		return result.Allow
	}, nil
}
