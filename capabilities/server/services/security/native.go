package security

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type NativeDetails struct {
	Store      *records.Store
	At         time.Time
	Sources    []string
	Definition string
}

func (a Authorizer) Native(c platform.Caller, permission, kind string, allowed []string, declared bool, details ...NativeDetails) platform.AuthzDecision {
	c.Member = a.Member(c.Member)
	pass := passGate("not restricted by this owner")
	context := &platform.AuthorizationContext{Tenant: platform.AuthorizationGate{Allowed: c.Tenant == a.sources.Tenant(), Reason: "The operation belongs to another tenant"}, Principal: platform.AuthorizationGate{Allowed: c.ID != "" && c.Active(), Reason: "The operation principal is invalid or inactive"}, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	if current, known := a.sources.LookupMember(c.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	if c.Agent && a.sources.AgentSuspended(c.ID) {
		context.Principal.Allowed = false
	}
	owner := a.sources.App(c.App)
	if owner == nil || !declared || !a.sources.OwnerActive(c.App) {
		context.Verb = &platform.AuthorizationGate{Reason: "The operation is unavailable in its owner declaration"}
	}
	var policies []platform.AuthorizationPolicy
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
			store = a.sources.Store()
		}
		at := detail.At
		if at.IsZero() {
			at = a.sources.Now()
		}
		walk := NewTraversal()
		for _, source := range detail.Sources {
			if name, named := strings.CutPrefix(source, "read:"); named {
				if !a.sources.NamedReadAllowed(c.Member, name) {
					context.Derived = platform.AuthorizationGate{Reason: "The original source is outside the caller's permissions"}
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
			decision := a.Authorize(store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, at, false, 0, walk)
			if !valid || id == "" || !decision.Allow || len(decision.Withheld) > 0 {
				context.Derived = platform.AuthorizationGate{Reason: "The original source is outside the caller's permissions"}
				break
			}
		}
	}
	return a.sources.Engine().Decide(platform.AuthorizationInput{Kind: kind, Subject: platform.AuthorizationSubject{ID: c.ID, App: c.App, Roles: c.RolesHere(), Agent: c.Agent, Automation: c.Automation}, Permission: permission, Allowed: allowed, Attributes: map[string]string{"member": c.ID, "app": c.App}, Context: context}, policies...)
}

func (a Authorizer) Entry(m platform.Member, app, permission, kind string, at time.Time) platform.AuthzDecision {
	m = a.Member(m)
	pass := passGate("not restricted by this owner")
	context := &platform.AuthorizationContext{Tenant: platform.AuthorizationGate{Allowed: m.Tenant == a.sources.Tenant(), Reason: "The operation belongs to another tenant"}, Principal: platform.AuthorizationGate{Allowed: m.ID != "" && m.Active(), Reason: "The operation principal is invalid or inactive"}, Row: pass, Fields: pass, Derived: pass, Markings: pass}
	if current, known := a.sources.LookupMember(m.ID); known && !current.Active() {
		context.Principal.Allowed = false
	}
	allowed := []string{}
	var policies []platform.AuthorizationPolicy
	if owner := a.sources.App(app); owner != nil {
		allowed = owner.Manifest().AllRoles()
		if kind == "read" && slices.Contains(owner.Manifest().Everyone, permission) {
			allowed = []string{platform.AnyMember}
		}
		policies = owner.Manifest().Actions.AuthorizationPolicies()
		context.Definition, _ = platform.CanonicalDigest([]any{app, owner.Manifest().Version, permission, kind, allowed})
		if name, input := strings.CutPrefix(permission, "input:"); input {
			if a.sources.InputGranted(m, app, name) {
				allowed = []string{platform.AnyMember}
			}
		}
	}
	return a.sources.Engine().Decide(platform.AuthorizationInput{Kind: kind, Subject: platform.AuthorizationSubject{ID: m.ID, App: app, Roles: m.RolesIn(app), Agent: m.Agent}, Permission: permission, Allowed: allowed, Attributes: map[string]string{"member": m.ID, "app": app}, Context: context}, policies...)
}
