package host

import (
	"context"
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"

	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"platformserver/services/security"
)

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
	} else if et := d.records.Type(q.Type); et != nil {
		app = et.Info().App
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

type authorizationDetails = security.NativeDetails

func (t *Tenant) authorizeReading(decide func() platform.AuthzDecision) platform.AuthzDecision {
	if t.quarantined() {
		return platform.AuthzDecision{}
	}
	if !t.currentReadScopes || t.records.Current().View() != nil {
		return decide()
	}
	var decision platform.AuthzDecision
	_, _ = coordinatedRecordRead(t, context.Background(), func() (struct{}, *kernel.Error) {
		decision = decide()
		return struct{}{}, nil
	})
	return decision
}

func (t *Tenant) authorizeNative(c platform.Caller, permission, kind string, allowed []string, declared bool, details ...authorizationDetails) platform.AuthzDecision {
	return t.authorizeReading(func() platform.AuthzDecision {
		return t.authorizer().Native(c, permission, kind, allowed, declared && t.resourceActionActive(permission), details...)
	})
}

func (t *Tenant) authorizeCapability(c platform.Caller, owner, permission string, allowed []string, declared bool, detail authorizationDetails) platform.AuthzDecision {
	c = platform.RouteCaller(c, owner)
	c.Automation = false
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
	return objects.AuthorizationRefusal(decision)
}

func (t *Tenant) authorizeEntry(m platform.Member, app, permission, kind string, at time.Time) platform.AuthzDecision {
	return t.authorizer().Entry(m, app, permission, kind, at)
}

func (t *Tenant) authorizationMember(m platform.Member) platform.Member {
	m = m.WithAuthorization(t.services.Authorization)
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

func (t *Tenant) authorizeFrom(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool) (decision platform.AuthzDecision) {
	if t.simulationTrace != nil {
		defer func() {
			t.simulationTrace(platform.DecisionTrace{Kind: "authorization", Request: &q, Authorization: &decision})
		}()
	}
	if held {
		return t.authorizeDepth(store, c, q, at, held, 0, newAuthorizationWalk())
	}
	return t.authorizeReading(func() platform.AuthzDecision {
		return t.authorizeDepth(store, c, q, at, false, 0, newAuthorizationWalk())
	})
}

type authorizationWalk = security.Traversal

func newAuthorizationWalk() *authorizationWalk { return security.NewTraversal() }

func (t *Tenant) authorizeDepth(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool, depth int, walk *authorizationWalk) platform.AuthzDecision {
	return t.authorizer().Authorize(store, c, q, at, held, depth, walk)
}

func (t *Tenant) authorizer() security.Authorizer {
	authorizer := security.Authorizer{
		Store: t.records, Now: Now, AgentSuspended: t.suspended, OwnerActive: t.ownerActive, NamedReadAllowed: t.mayCallRead, InputGranted: t.connectorInputGranted,
		Tenant: t.ID, Decider: t.services.Authorization, Member: t.authorizationMember, LookupMember: t.member, App: t.app,
		ActionOwner: func(action string) platform.App {
			if !t.resourceActionActive(action) {
				return nil
			}
			return t.owner["action:"+action]
		},
		KnowledgeReadable: t.knowledgeSourceReadable, ScopePolicy: t.scopePolicy, SourceWithheld: t.sourceWithheld,
		ProjectAccess: t.resourcePermission,
		Markings: func(store *records.Store, held bool) security.RecordMarkings {
			return securityMarkings{t.markingsIn(store, held)}
		},
	}
	if t.directory != nil {
		authorizer.ElementExists = func(id string, at time.Time) bool {
			_, exists := t.directory.Element(id, at.UTC().Format(time.DateOnly))
			return exists
		}
	}
	return authorizer
}

func (t *Tenant) connectorInputGranted(m platform.Member, app, name string) bool {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	connector := t.connectors.byID[m.ID]
	return connector != nil && connector.GetTenantId() == t.ID && (name == "heartbeat" || slices.ContainsFunc(connector.GetDataClasses(), func(class string) bool { return strings.HasPrefix(class, app+".") }))
}

type securityMarkings struct{ markingIndex }

func (m securityMarkings) Row(typ string, row *records.Row) string { return m.row(typ, row) }
func (m securityMarkings) Compartments(typ string, row *records.Row) []string {
	return m.compartments(typ, row)
}
func (m securityMarkings) Highest(typ string) string { return m.highest(typ) }

func (t *Tenant) sourceWithheld(store *records.Store, c platform.Caller, et *records.Type, value reflect.Value, fields []string, at time.Time, depth int, walk *authorizationWalk) []string {
	w := t.objectOwner(store).NarrowerIn(store, c.Member, at, true)
	all := []string{}
	for _, d := range et.Info().Derived {
		if !d.All {
			all = append(all, w.Sources(value, d)...)
		}
	}
	withheld := []string{}
	for _, alias := range et.Info().Fields {
		if alias.AliasOf == "" || len(fields) > 0 && !slices.Contains(fields, alias.Name) {
			continue
		}
		request := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: et.Info().Type, Record: records.Header(value).ID, Fields: []string{alias.AliasOf}}
		grant := t.authorizeDepth(store, c, request, at, true, depth+1, walk)
		if !grant.Allow || len(grant.Withheld) > 0 {
			withheld = append(withheld, alias.Name)
		}
	}
	for _, d := range et.Info().Derived {
		names := []string{}
		if len(d.List) > 0 {
			names = append(names, fieldName(et.Info().Go, d.List))
		} else {
			for _, path := range d.Fields {
				names = append(names, fieldName(et.Info().Go, path))
			}
		}
		if len(fields) > 0 && !slices.ContainsFunc(names, func(name string) bool { return slices.Contains(fields, name) }) {
			continue
		}
		refs := all
		if !d.All {
			refs = w.Sources(value, d)
		}
		denied := len(refs) == 0 && slices.ContainsFunc(names, func(name string) bool {
			field, known := et.Info().Field(name)
			return known && field.SourceType == "generated" && !value.FieldByIndex(field.Index).IsZero()
		})
		for _, ref := range refs {
			if ref == et.Info().Type+"/"+records.Header(value).ID {
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

func (t *Tenant) visibleIn(store *records.Store, m platform.Member, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleForCallerIn(store, platform.NewCaller(runtime{t}, m, et.Info().App, false, false), et, at)
}

func (t *Tenant) scopePolicy(store *records.Store, c platform.Caller, info platform.EntityInfo, at time.Time, depth int, walk *authorizationWalk) (*records.RowFilter, *kernel.Error) {
	through := func(v reflect.Value) bool {
		typ, id, _ := strings.Cut(info.Scope.Through(v.Interface()), "/")
		return t.authorizeDepth(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, at, true, depth+1, walk).Allow
	}
	return records.ScopePolicy(info, c.Member, t.directory, at, through, c.Rules())
}

func (t *Tenant) visibleForCallerIn(store *records.Store, c platform.Caller, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.Info().Type}
	if refusal := authorizationRefusal(t.authorizeFrom(store, c, question, at, true)); refusal != nil {
		return nil, refusal
	}
	c.Member = t.authorizationMember(c.Member)
	if c.Automation && c.App == et.Info().App {
		return func(reflect.Value) bool { return true }, nil
	}
	policy, err := t.scopePolicy(store, c, et.Info(), at, 0, newAuthorizationWalk())
	if err != nil {
		return func(reflect.Value) bool { return false }, nil
	}
	if et.Info().Type == build.DatasetVersionType {
		index := t.markingsIn(store, true)
		return func(value reflect.Value) bool { return index.visible(c.Member, value) && policy.Match(value) }, nil
	}
	return func(value reflect.Value) bool {
		id := records.Header(value).ID
		allowed, projectRead := t.resourcePermission(store, c.Member, et.Info().Type, id, "read", true)
		return allowed && (projectRead || policy.Match(value))
	}, nil
}
