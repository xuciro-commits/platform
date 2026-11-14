package host

import (
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/security"
)

func (t *Tenant) Permissions() []security.AppPermissions {
	return security.PermissionsReport{Apps: t.LiveApps, Holders: func(app, role string) int {
		if t.identityOwner == nil {
			return 0
		}
		return len(t.identityOwner.Holding(app, role))
	}}.Permissions()
}

func (t *Tenant) ExplainOperation(request platform.AuthzRequest, at time.Time) (platform.AuthzDecision, *kernel.Error) {
	m, known := t.member(request.Principal)
	if !known {
		m = platform.Member{ID: request.Principal, Tenant: request.Tenant}
	}
	app := ""
	if info, exists := t.Entity(request.Type); exists {
		app = info.App
	}
	return t.authorizeFrom(t.Projection, t.objectCaller(m, app), request, at, false), nil
}

func (t *Tenant) Explain(member, permission string) (security.Explanation, *kernel.Error) {
	return security.PermissionExplainer{Member: t.member, App: t.App, Apps: t.LiveApps, Caller: t.objectCaller,
		Entry: func(m platform.Member, app, name string) platform.AuthzDecision {
			return t.authorizeEntry(m, app, name, "read", Now())
		}}.Explain(member, permission)
}
