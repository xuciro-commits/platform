package host

import (
	"net/http"
	"platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/security/control"
	"platformserver/services/security/idp"
)

func (h *Host) identityRoutes(rt *routes) {
	entrance := idp.Entrance{Authenticate: h.authenticate, SecondFactor: h.SecondFactor, Now: h.Now, HostAdmins: h.HostAdmins, SelfService: h.SelfService, CanCreate: h.CreateTenant != nil, Issuer: h.Issuer, Workspaces: h.workspaceIdentities}
	transport.IdentityEntrance{Entry: entrance.Entry, Accept: func(r *http.Request, id, subject string) (transport.MeTenant, *platform.Error) {
		t := h.tenantByID(id)
		if t == nil || t.businessStopped() {
			return transport.MeTenant{}, unknown()
		}
		if refusal := h.identitySubmission(t, subject, h.Now(), control.InvitationSubmission); refusal != nil {
			return transport.MeTenant{}, refusal
		}
		return transport.MeTenant{ID: id, Name: t.IdentityOwner.TenantRecord().Name}, nil
	}, Create: func(body protocol.WorkspaceCreate, subject string) (transport.MeTenant, int) {
		spec, ok := idp.WorkspaceSpec(body, subject)
		if !ok {
			return transport.MeTenant{}, 400
		}
		if h.tenantByID(body.ID) != nil {
			return transport.MeTenant{}, 409
		}
		t, err := h.CreateTenant(spec, subject)
		if err != nil {
			return transport.MeTenant{}, 409
		}
		return transport.MeTenant{ID: t.ID, Name: t.IdentityOwner.TenantRecord().Name}, 201
	}}.Routes(func(spec transport.Route, handler http.HandlerFunc) {
		h.routes = append(h.routes, spec)
		rt.mux.HandleFunc(spec.Pattern, handler)
	})
}

func (h *Host) workspaceIdentities() []idp.WorkspaceIdentity {
	return idp.WorkspaceIdentities(h.currentTenants(), func(t *Tenant) idp.WorkspaceIdentity {
		return idp.WorkspaceIdentity{ID: t.ID, Owner: t.IdentityOwner, Stopped: t.businessStopped()}
	})
}
