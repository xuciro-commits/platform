package security

import (
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type RecordMarkings interface {
	Row(string, *records.Row) string
	Compartments(string, *records.Row) []string
	Highest(string) string
}

type Authorizer struct {
	sources Sources
}

func NewAuthorizer(sources Sources) Authorizer {
	return Authorizer{sources: sources}
}

func (a Authorizer) Member(m platform.Member) platform.Member {
	m = m.WithAuthorization(a.sources.Engine())
	current, known := a.sources.LookupMember(m.ID)
	if !known {
		if !a.sources.DeclaredPrincipal(m.ID) {
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

func passGate(reason string) platform.AuthorizationGate {
	return platform.AuthorizationGate{Allowed: true, Reason: reason}
}

type Traversal struct {
	decisions map[string]platform.AuthzDecision
	active    map[string]bool
}

func NewTraversal() *Traversal {
	return &Traversal{decisions: map[string]platform.AuthzDecision{}, active: map[string]bool{}}
}

func (a Authorizer) Authorize(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool, depth int, walk *Traversal) platform.AuthzDecision {
	c.Member = a.Member(c.Member)
	key := string(platform.Raw(q))
	if decision, known := walk.decisions[key]; known && depth < 16 {
		return decision
	}
	cycle := walk.active[key]
	if !cycle {
		walk.active[key] = true
		defer delete(walk.active, key)
	}
	pass := passGate("not restricted by this owner")
	context := &platform.AuthorizationContext{Tenant: pass, Principal: pass, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	context.Tenant = platform.AuthorizationGate{Allowed: q.Tenant == a.sources.Tenant() && c.Tenant == a.sources.Tenant(), Reason: "The operation belongs to another tenant"}
	context.Principal = platform.AuthorizationGate{Allowed: c.ID != "" && q.Principal == c.ID && c.Active(), Reason: "The operation principal is invalid or inactive"}
	if current, known := a.sources.LookupMember(c.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	if depth >= 16 || cycle {
		context.Derived = platform.AuthorizationGate{Reason: "The declared source traversal exceeds 16 segments or contains a cycle"}
	}
	request := platform.AuthorizationInput{Kind: q.Kind, Subject: platform.AuthorizationSubject{ID: c.ID, App: c.App, Roles: c.RolesHere(), Agent: c.Agent, Automation: c.Automation}, Permission: q.Action, Resource: q.Type + "/" + q.Record, Context: context}
	var policies []platform.AuthorizationPolicy
	if q.Action != "" {
		context.Verb = &platform.AuthorizationGate{Reason: "The operation is unavailable in its owner declaration"}
		owner := a.sources.ActionOwner(q.Action)
		if owner != nil {
			facts, rules, ok := owner.Manifest().Actions.AuthorizationRequest(platform.RouteCaller(c, owner.Manifest().ID), q.Action, request.Resource)
			if ok {
				request.Subject, request.Allowed, request.Attributes, policies = facts.Subject, facts.Allowed, facts.Attributes, rules
				context.Verb = nil
			}
		}
	}
	var et *records.Type
	var current *records.Row
	func() {
		if !held {
			store.Lock()
			defer store.Unlock()
		}
		et = store.Type(q.Type)
		if et != nil {
			current = et.Rows().Get(q.Record)
		}
	}()
	if q.Action == "" {
		if et != nil {
			request.Subject.App = et.Info().App
			request.Subject.Roles = c.RolesIn(et.Info().App)
			request.Allowed = slices.Clone(request.Subject.Roles)
			request.Permission = et.Info().Type + "." + q.Kind
			if q.Kind == "derive" {
				request.Permission = et.Info().Type + ".read"
			}
			request.Attributes = map[string]string{"member": c.ID, "app": et.Info().App, "target": et.Info().Type}
			if owner := a.sources.App(et.Info().App); owner != nil {
				policies = owner.Manifest().Actions.AuthorizationPolicies()
			}
			if et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil {
				request.Allowed = []string{platform.AnyMember}
			}
			if et.Info().Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				request.Allowed = []string{platform.AnyMember}
			}
		} else {
			request.Allowed = slices.Clone(c.RolesHere())
		}
	}
	if et != nil {
		context.Definition = et.Info().Type
		if digest, err := platform.CanonicalDigest(struct {
			Fields                          []platform.FieldInfo
			Derived                         []platform.DerivationInfo
			Withheld                        []int
			Policies                        []platform.AuthorizationPolicy
			Default, Owner, Unit, Structure string
			Levels                          map[string]string
		}{et.Info().Fields, et.Info().Derived, et.Info().Withheld, policies, et.Info().Scope.Default, et.Info().Scope.Owner, et.Info().Scope.Unit, et.Info().Scope.Structure, et.Info().Scope.Levels}); err == nil {
			context.Definition = digest
		}
		if q.Record != "" && q.Kind != "create" && (q.Kind != "action" || current != nil) {
			if c.Automation && c.App == et.Info().App {
				context.Row = passGate("the declared owner service boundary")
			} else if et.Info().Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				context.Row = platform.AuthorizationGate{Allowed: a.sources.KnowledgeReadable(store, c.Member, q.Record, ""), Reason: "The document is outside the caller's source grants"}
			} else if et.Info().Scope.Through != nil && (current == nil || depth >= 16 || cycle) {
				context.Row = platform.AuthorizationGate{Reason: "The owning record is unavailable or exceeds 16 segments"}
			} else {
				policy, err := a.ScopePolicy(store, c, et.Info(), at, depth, walk)
				reason := "The record is outside the caller's row scope"
				if et.Info().Scope.Through != nil {
					reason = "The owning record is outside the caller's row scope"
				}
				context.Row = platform.AuthorizationGate{Allowed: current != nil && err == nil && policy.Match(current.Value), Reason: reason}
			}
		}
		if q.Record != "" {
			allowed, projectRead := a.sources.ProjectAccess(store, c.Member, q.Type, q.Record, q.Kind, held)
			if !allowed {
				context.Row = platform.AuthorizationGate{Reason: "The resource is outside the caller's project access"}
			} else if projectRead && slices.Contains([]string{"read", "derive", "export"}, q.Kind) {
				context.Row = passGate("A project role grants access to its resource definition")
			}
		}
		for _, name := range q.Fields {
			field, known := et.Info().Field(name)
			if q.Action != "" && (q.Kind == "write" || q.Kind == "create") && !slices.Contains(et.Info().Standard, q.Action) {
				context.Fields.Reason = "The typed owner action declares its accepted input fields"
				continue
			}
			if !known && q.Action != "" {
				continue
			}
			allowed := known && field.ReadsAny(c.RolesIn(et.Info().App), c.Rules()) && !(c.AIContext && field.AIInput != nil && !*field.AIInput)
			if et.Info().Type == platform.KnowledgeDocumentType && q.Kind == "derive" {
				allowed = a.sources.KnowledgeReadable(store, c.Member, q.Record, name)
			}
			if q.Kind == "write" || q.Kind == "create" {
				if field.ReadOnly {
					continue
				}
				allowed = known && field.WritesAny(c.RolesIn(et.Info().App), c.Rules())
				if c.Automation && c.App == et.Info().App {
					allowed = true
				}
			}
			if !allowed {
				if slices.Contains([]string{"read", "derive", "export"}, q.Kind) {
					context.Withheld = append(context.Withheld, name)
				} else {
					context.Fields = platform.AuthorizationGate{Reason: "An involved field is outside the caller's field permissions"}
				}
			}
		}
		if current != nil && depth < 16 && !cycle && slices.Contains([]string{"read", "derive", "export"}, q.Kind) {
			context.Withheld = append(context.Withheld, a.sourceWithheld(store, c, et, current.Value, q.Fields, at, depth, walk)...)
		}
	} else if q.Action == "" {
		directory := a.sources.Directory()
		if q.Type == "enterprise.element" && directory != nil && q.Record != "" {
			request.Allowed = []string{platform.AnyMember}
			_, exists := directory.Element(q.Record, at.UTC().Format(time.DateOnly))
			context.Row = platform.AuthorizationGate{Allowed: exists, Reason: "The enterprise source is unavailable"}
		} else {
			context.Verb = &platform.AuthorizationGate{Reason: "The record type is unavailable"}
		}
	}
	if q.From != "" {
		ref, field, _ := strings.Cut(q.From, "#")
		readable := false
		func() {
			if !held {
				store.Lock()
				defer store.Unlock()
			}
			typ, id, _ := strings.Cut(ref, "/")
			fields := []string{}
			if field != "" {
				fields = append(fields, field)
			}
			if depth < 16 && !cycle {
				source := a.Authorize(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, at, true, depth+1, walk)
				readable = source.Allow && len(source.Withheld) == 0
			}
		}()
		context.Derived = platform.AuthorizationGate{Allowed: readable, Reason: "The original source is outside the caller's permissions"}
	}
	if et != nil {
		index := a.sources.Markings(store, held)
		if !(c.Automation && c.App == et.Info().App) && !(platform.MarkingSet{Level: index.Row(et.Info().Type, current), Compartments: index.Compartments(et.Info().Type, current)}).Cleared(c.Member) {
			context.Markings = platform.AuthorizationGate{Reason: "The data carries a marking the caller is not cleared for"}
		} else if q.Kind == "export" && index.Highest(et.Info().Type) == "restricted" {
			context.Markings = platform.AuthorizationGate{Reason: "Restricted data does not leave as CSV"}
		}
	}
	slices.Sort(context.Withheld)
	context.Withheld = slices.Compact(context.Withheld)
	if len(context.Withheld) > 0 {
		context.Fields.Reason = "Some fields are withheld by field permissions or source grants"
		context.Derived.Reason = "Derived content is limited to readable declared sources"
	}
	decision := a.sources.Engine().Decide(request, policies...)

	if !cycle && depth < 16 {
		walk.decisions[key] = decision
	}
	return decision
}

func (a Authorizer) ScopePolicy(store *records.Store, c platform.Caller, info platform.EntityInfo, at time.Time, depth int, walk *Traversal) (*records.RowFilter, *kernel.Error) {
	through := func(v reflect.Value) bool {
		typ, id, _ := strings.Cut(info.Scope.Through(v.Interface()), "/")
		return a.Authorize(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, at, true, depth+1, walk).Allow
	}
	return records.ScopePolicy(info, c.Member, a.sources.Directory(), at, through, c.Rules())
}

func (a Authorizer) sourceWithheld(store *records.Store, c platform.Caller, et *records.Type, value reflect.Value, fields []string, at time.Time, depth int, walk *Traversal) []string {
	w := a.sources.DerivationSources(store, c.Member, at, true)
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
		grant := a.Authorize(store, c, request, at, true, depth+1, walk)
		if !grant.Allow || len(grant.Withheld) > 0 {
			withheld = append(withheld, alias.Name)
		}
	}
	for _, d := range et.Info().Derived {
		names := []string{}
		if len(d.List) > 0 {
			names = append(names, platform.FieldName(et.Info().Go, d.List))
		} else {
			for _, path := range d.Fields {
				names = append(names, platform.FieldName(et.Info().Go, path))
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
				denied = denied || !a.sources.NamedReadAllowed(c.Member, name)
				continue
			}
			record, field, _ := strings.Cut(ref, "#")
			typ, id, _ := strings.Cut(record, "/")
			sourceFields := []string{}
			if field != "" {
				sourceFields = append(sourceFields, field)
			}
			source := a.Authorize(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: sourceFields}, at, true, depth+1, walk)
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
