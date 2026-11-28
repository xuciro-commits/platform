package host

import (
	"maps"
	"net/http"
	"platformserver/kernel/protocol"
	"slices"
	"strings"
	"time"

	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/monitoring"

	controlowner "platformserver/services/security/control"
)

type SupportRead struct {
	Tenant  string                       `json:"tenant"`
	Grant   string                       `json:"grant"`
	At      time.Time                    `json:"at"`
	Health  TenantHealth                 `json:"health"`
	Audit   []protocol.AuditEntry        `json:"audit"`
	Answers []controlowner.SupportAnswer `json:"answers,omitempty"`
}

func (h *Host) hostRoute(mux *http.ServeMux, route transport.Route, f func(http.ResponseWriter, *http.Request, string, *Tenant)) {
	h.routes = append(h.routes, route)
	mux.HandleFunc(route.Pattern, transport.HostAuthorized(route.Pattern, h.tenantByID, func(r *http.Request) (string, platform.AuthzDecision, *platform.Error) {
		subject, authenticated := h.authenticate(bearer(r))
		decision := h.AuthorizationEngine.Decide(controlowner.HostAuthorizationInput(subject, authenticated, h.HostAdmins[subject], route.Pattern, r.PathValue("tenant")))
		return subject, decision, authorizationRefusal(decision)
	}, f))
}

func (h *Host) hostConsoleRoutes(mux *http.ServeMux) {
	h.hostCompositionRoutes(mux)
	h.tenantRoutes(mux)
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/me", Summary: "Whether the caller is a host administrator, and the tenants this console serves", Answer: map[string]any{}},
		transport.HostAnswer(func(subject string, _ *Tenant) any {
			tenants := []string{}
			for _, t := range h.currentTenants() {
				tenants = append(tenants, t.ID)
			}
			slices.Sort(tenants)
			return map[string]any{"subject": subject, "tenants": tenants}
		}))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/overview", Summary: "Host and tenant overview: health, quarantine, lifecycle, isolation and resource use (host administrators)", Answer: controlowner.HostOverview{}},
		transport.HostAnswer(func(string, *Tenant) any {
			out := controlowner.HostOverview{Now: h.Now().UTC(), Tenants: []controlowner.HostTenantView{}}
			for _, t := range h.currentTenants() {
				out.Tenants = append(out.Tenants, h.tenantView(t))
			}
			slices.SortFunc(out.Tenants, func(a, b controlowner.HostTenantView) int { return strings.Compare(a.ID, b.ID) })
			out.Admins = len(h.HostAdmins)
			return out
		}))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}", Summary: "One tenant's console view (host administrators)", Answer: controlowner.HostTenantView{}},
		transport.HostAnswer(func(_ string, t *Tenant) any { return h.tenantView(t) }))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/plans", Summary: "Quota plans available for host operators to assign to workspaces", Answer: []protocol.QuotaPlan{}},
		transport.HostAnswer(func(string, *Tenant) any { return monitoring.DefaultQuotaPlans() }))
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/quota", Summary: "Assign a quota plan or custom soft and hard limits to a tenant, recorded in its audit (host administrators)", Body: protocol.QuotaRequest{}, Answer: controlowner.HostTenantView{}},
		transport.CommandOr(1<<20, http.StatusConflict, func(_ *http.Request, subject string, t *Tenant, body protocol.QuotaRequest) (controlowner.HostTenantView, error) {
			if err := t.setHostQuota(body, subject, h.Now()); err != nil {
				return controlowner.HostTenantView{}, err
			}
			return h.tenantView(t), nil
		}))
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/lifecycle", Summary: "Open, suspend, resume or decommission a tenant, with a reason recorded in its audit (host administrators)", Body: controlowner.LifecycleRequest{}, Answer: controlowner.HostTenantView{}},
		transport.CommandOr(1<<20, http.StatusConflict, func(r *http.Request, subject string, t *Tenant, body controlowner.LifecycleRequest) (controlowner.HostTenantView, error) {
			if err := t.setHostLifecycle(body, subject, h.Now(), transport.HostRequestAuthorization(r, t.ID)...); err != nil {
				return controlowner.HostTenantView{}, err
			}
			return h.tenantView(t), nil
		}))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/artifacts", Summary: "The tenant's trusted releases and saved candidates with their compatibility (host administrators)", Answer: []controlowner.HostArtifactView{}},
		transport.HostAnswer(func(_ string, t *Tenant) any {
			state := t
			if image := t.readImage.Load(); image != nil {
				state = image.state
			} else {
				t.Lane.Lock()
				defer t.Lane.Unlock()
			}
			return controlowner.HostArtifacts(state.releases.Ids(), state.releases.Active(), func(id string) []byte { return state.releases.Raw(id) }, PublicationResolver().ReadCandidate)
		}))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/support", Summary: "Support sessions opened for a tenant (host administrators)", Answer: []protocol.SupportGrant{}},
		transport.HostAnswer(func(_ string, t *Tenant) any { return t.console.SupportGrants() }))
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/support", Summary: "Open an authorized support session for a tenant member, with a reason and an end (host administrators)", Answer: protocol.SupportGrant{}},
		transport.CommandOr(1<<20, http.StatusConflict, func(r *http.Request, subject string, t *Tenant, body controlowner.SupportOpenRequest) (protocol.SupportGrant, error) {
			return t.openSupport(body, subject, h.Now(), transport.HostRequestAuthorization(r, t.ID)...)
		}))
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/support/{grant}", Summary: "Use an authorized support session: read the tenant's health and audit as its granted member; every use is recorded (host administrators)", Answer: SupportRead{}},
		func(w http.ResponseWriter, r *http.Request, subject string, _ *Tenant) {
			grant := r.PathValue("grant")
			for _, t := range h.currentTenants() {
				read, err := t.useSupport(grant, subject, h.Now(), transport.HostRequestAuthorization(r, t.ID)...)
				if err == controlowner.ErrUnknownGrant {
					continue
				}
				transport.AnswerOr(w, http.StatusConflict, read, err)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		})
}

func (h *Host) tenantView(t *Tenant) controlowner.HostTenantView {
	now := h.Now()
	state := t
	image := t.readImage.Load()
	if image != nil {
		state = image.state
	} else {
		t.Lane.Lock()
	}
	quota := monitoring.CurrentQuota(state.Projection)
	view := controlowner.HostTenantView{ID: t.ID, Lifecycle: t.console.State(), Plan: monitoring.PlanOf(quota), Quota: quota, ActiveRelease: state.releases.Active(), Candidates: state.releases.Count()}
	if image == nil {
		t.Lane.Unlock()
	}
	health := t.Health(now)
	view.Started, view.FailedWork, view.Connectors = health.Started, health.Failed, health.Connectors
	view.Apps = []string{}
	for _, a := range t.Apps() {
		view.Apps = append(view.Apps, a.ID)
	}
	slices.Sort(view.Apps)
	if t.IdentityOwner != nil {
		view.Members = t.IdentityOwner.MemberCount()
	}
	view.Audit = len(t.Audit())
	if fault := t.Fault.Load(); fault != nil {
		view.Quarantined, view.Fault = true, fault.Reason
	}
	view.Support = controlowner.ActiveSupport(t.console.SupportGrants(), now)
	return view
}

func (h *Host) environmentRoutes(mux *http.ServeMux) {
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/promotions", Summary: "Promote a sealed release candidate from another tenant into this one, optionally activating it (host administrators)", Answer: releaseowner.PromotionResult{}},
		transport.HostTransfer(h.tenantByID, "from, candidate, key and targetGrant are required", releaseowner.PromotionRequest.Complete, func(b releaseowner.PromotionRequest) string { return b.From },
			func(_ *http.Request, from, to *Tenant, body releaseowner.PromotionRequest) (any, int, error) {
				member, err := to.grantMember(body.TargetGrant, "the support session names no member", h.Now())
				if err != nil {
					return nil, http.StatusForbidden, err
				}
				result, err := PromoteCandidate(from, to, body.Candidate, body.Key, body.Activate, member, h.Now())
				return result, http.StatusConflict, err
			}))
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/migrations", Summary: "Migrate real records from another tenant into this one through its generated actions (host administrators)", Answer: protocol.MigrationResult{}},
		transport.HostTransfer(h.tenantByID, "from, types, sourceGrant and targetGrant are required", controlowner.MigrationRequest.Complete, func(b controlowner.MigrationRequest) string { return b.From },
			func(r *http.Request, from, to *Tenant, body controlowner.MigrationRequest) (any, int, error) {
				source, err := from.grantMember(body.SourceGrant, "the source support session names no member", h.Now())
				if err != nil {
					return nil, http.StatusForbidden, err
				}
				target, err := to.grantMember(body.TargetGrant, "the target support session names no member", h.Now())
				if err != nil {
					return nil, http.StatusForbidden, err
				}
				result, err := MigrateRecords(from, to, source, target, body.Types, h.Now())
				if err == nil {
					err = to.rememberMigration(result, target.ID, h.Now(), transport.HostRequestAuthorization(r, to.ID)...)
				}
				return result, http.StatusConflict, err
			}))
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/migrations", Summary: "What the host console moved in or out of this tenant (host administrators)", Answer: []protocol.MigrationManifest{}},
		transport.HostAnswer(func(_ string, t *Tenant) any { return t.console.Migrations() }))
}

func (h *Host) tenantByID(id string) *Tenant {
	for _, t := range h.currentTenants() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (t *Tenant) setHostLifecycle(request controlowner.LifecycleRequest, subject string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) error {
	if now.IsZero() {
		now = Now()
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	action, err := request.Prepare(t.ID, subject)
	if err != nil {
		return err
	}
	_, err = t.acceptHostAction(action, "host:"+subject, now, func(image *protocol.HostControlImage) (protocol.SupportGrant, error) {
		return protocol.SupportGrant{}, controlowner.ApplyLifecycle(image, request.Action, t.quarantined(), func() bool {
			return missingRuntime(t.desiredRuntime()) || !maps.Equal(t.desiredRuntime(), t.services.Selection()) || !slices.Equal(t.currentDisabledOwners(), t.desiredDisabledOwners()) || slices.Contains(t.desiredDisabledOwners(), controlowner.PlatformApp)
		})
	}, evidence...)
	return err
}

func (t *Tenant) openSupport(request controlowner.SupportOpenRequest, subject string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) (protocol.SupportGrant, error) {
	t.Lane.Lock()
	defer t.Lane.Unlock()
	action, id, err := request.Prepare(t.ID, subject, now)
	if err != nil {
		return protocol.SupportGrant{}, err
	}
	return t.acceptHostAction(action, "host:"+subject, now, func(image *protocol.HostControlImage) (protocol.SupportGrant, error) {
		if _, ok := t.Member(request.Member); !ok {
			return protocol.SupportGrant{}, controlowner.SupportError("no such member in this tenant")
		}
		return controlowner.OpenSupportGrant(image, id, t.ID, request.Member, request.Reason, subject, request.Minutes, now), nil
	}, evidence...)
}

func (t *Tenant) useSupport(id, subject string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) (SupportRead, error) {
	t.Lane.Lock()
	_, err := t.acceptHostAction(controlowner.SupportUseAction(id), "host:"+subject, now, func(image *protocol.HostControlImage) (protocol.SupportGrant, error) {
		return controlowner.UseSupportGrant(image, id, now)
	}, evidence...)
	t.Lane.Unlock()
	if err != nil {
		return SupportRead{}, err
	}
	audit := controlowner.TailAudit(t.Audit(), 20)
	return SupportRead{Tenant: t.ID, Grant: id, At: now.UTC(), Health: t.Health(now), Audit: audit}, nil
}

func (t *Tenant) hostControl() runtimekernel.HostControlOwner {
	return runtimekernel.HostControlOwner{Image: t.console.Image, Restore: t.console.Restore, Audit: &t.AuditLog}
}

func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit protocol.AuditEntry, decide func(*protocol.HostControlImage) (protocol.SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (protocol.SupportGrant, error) {
	answer, applied, err := t.AcceptHostControl(runtimekernel.HostRequest{Tenant: t.ID, Action: action, Member: member, Request: request, At: now, Audit: audit, Evidence: evidence},
		t.hostControl(), decide, t.acceptResult, t.quarantine)
	if applied {
		t.acceptedResultApplied()
		t.changed()
	}
	return answer, err
}

func (t *Tenant) acceptHostAction(a controlowner.HostAction, member string, now time.Time, decide func(*protocol.HostControlImage) (protocol.SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (protocol.SupportGrant, error) {
	return t.acceptHostControl(a.Name, member, a.Request, now, a.Audit, decide, evidence...)
}
