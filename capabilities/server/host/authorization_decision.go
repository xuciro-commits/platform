package host

import (
	"context"
	acceptedformat "platformserver/kernel/acceptedformat"
	dataowner "platformserver/services/data"

	"platformserver/kernel/records"
	"reflect"
	"slices"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"platformserver/services/security"
)

func (r runtime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	if at.IsZero() {
		at = Now()
	}
	decision := r.t.authorizeFrom(r.t.Projection, c, q, at, false)
	return decision
}
func (d *stagedDecision) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	c.Member = d.tenant.authorizationMember(c.Member)
	if at.IsZero() {
		at = d.At
	}
	decision := d.tenant.authorizeFrom(d.Records, c, q, at, false)
	app := c.App
	if owner := d.tenant.App(d.tenant.owner["action:"+q.Action]); owner != nil {
		app = owner.Manifest().ID
	} else if et := d.Records.Type(q.Type); et != nil {
		app = et.Info().App
	}
	d.Authorizations = append(d.Authorizations, acceptedformat.AuthorizationEvidence{Request: q, Decision: decision, Roles: slices.Clone(c.RolesIn(app))})
	return decision
}

func (d *stagedDecision) RecordAuthorization(c platform.Caller, q platform.AuthzRequest, decision platform.AuthzDecision) {
	c.Member = d.tenant.authorizationMember(c.Member)
	d.Authorizations = append(d.Authorizations, acceptedformat.AuthorizationEvidence{Request: q, Decision: decision, Roles: slices.Clone(c.RolesHere())})
	if d.TraceEnabled() {
		d.RecordDecisionTrace(platform.DecisionTrace{Kind: "authorization", Request: &q, Authorization: &decision})
	}
}

type authorizationDetails = security.NativeDetails

func (t *Tenant) authorizeNative(c platform.Caller, permission, kind string, allowed []string, declared bool, details ...authorizationDetails) platform.AuthzDecision {
	if c.ReadScoped() {
		return t.authorizer().Native(c, permission, kind, allowed, declared && t.resourceActionActive(permission), details...)
	}
	decision, _ := recordRead(t, context.Background(), func(t *Tenant) (platform.AuthzDecision, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, c.Member, c.App, c.Replaying, c.Automation)
		return t.authorizer().Native(c, permission, kind, allowed, declared && t.resourceActionActive(permission), details...), nil
	})
	return decision
}

func (t *Tenant) authorizeCapability(c platform.Caller, owner, permission string, allowed []string, declared bool, detail authorizationDetails) platform.AuthzDecision {
	c = platform.RouteCaller(c, owner)
	c.Automation = false
	decision := t.authorizeNative(c, permission, "action", allowed, declared, detail)
	if draft := c.StagedDecision(); draft != nil {
		draft.RecordAuthorization(c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Action: permission}, decision)
	}
	return decision
}

func (d *stagedDecision) authorizeNative(c platform.Caller, permission, kind string, allowed []string, declared bool) *kernel.Error {
	decision := d.tenant.authorizeNative(c, permission, kind, allowed, declared)
	d.Authorizations = append(d.Authorizations, acceptedformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: kind, Action: permission}, Decision: decision, Roles: slices.Clone(c.RolesHere())})
	return authorizationRefusal(decision)
}

func authorizationRefusal(decision platform.AuthzDecision) *kernel.Error {
	return objects.AuthorizationRefusal(decision)
}

func (t *Tenant) authorizeEntry(m platform.Member, app, permission, kind string, at time.Time) platform.AuthzDecision {
	return t.authorizer().Entry(m, app, permission, kind, at)
}

func (t *Tenant) authorizeFrom(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool) (decision platform.AuthzDecision) {
	if t.simulationTrace != nil {
		defer func() {
			t.simulationTrace(platform.DecisionTrace{Kind: "authorization", Request: &q, Authorization: &decision})
		}()
	}
	if held || c.ReadScoped() {
		return t.authorizeDepth(store, c, q, at, held, 0, newAuthorizationWalk())
	}
	decision, _ = recordRead(t, context.Background(), func(t *Tenant) (platform.AuthzDecision, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, c.Member, c.App, c.Replaying, c.Automation)
		return t.authorizeDepth(t.Projection, c, q, at, false, 0, newAuthorizationWalk()), nil
	})
	return decision
}

type authorizationWalk = security.Traversal

func newAuthorizationWalk() *authorizationWalk { return security.NewTraversal() }

func (t *Tenant) authorizeDepth(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool, depth int, walk *authorizationWalk) platform.AuthzDecision {
	return t.authorizer().Authorize(store, c, q, at, held, depth, walk)
}

func (t *Tenant) connectorInputGranted(m platform.Member, app, name string) bool {
	return t.connectors.Granted(m.ID, app, name)
}

func (t *Tenant) visibleIn(store *records.Store, m platform.Member, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleForCallerIn(store, platform.NewCaller(runtime{t: t}, m, et.Info().App, false, false), et, at)
}

func (t *Tenant) visibleForCallerIn(store *records.Store, c platform.Caller, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.Info().Type}
	if refusal := authorizationRefusal(t.authorizeFrom(store, c, question, at, true)); refusal != nil {
		return nil, refusal
	}
	c.Member = t.authorizationMember(c.Member)
	if c.Automation && c.App == et.Info().App {
		return nil, nil
	}
	policy, err := t.authorizer().ScopePolicy(store, c, et.Info(), at, 0, newAuthorizationWalk())
	if err != nil {
		return func(reflect.Value) bool { return false }, nil
	}
	if et.Info().Type == dataowner.DatasetVersionType {
		index := t.markingsIn(store, true)
		return func(value reflect.Value) bool { return index.Visible(c.Member, value) && policy.Match(value) }, nil
	}
	if policy == nil && !build.ProjectDelegated(c.Member) {
		return nil, nil
	}
	return func(value reflect.Value) bool {
		id := records.Header(value).ID
		allowed, projectRead := t.ResourcePermission(store, c.Member, et.Info().Type, id, "read", true)
		return allowed && (projectRead || policy.Match(value))
	}, nil
}
