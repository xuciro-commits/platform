package host

import transport "platformserver/kernel/transport"

import (
	"context"
	"maps"
	"net/http"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/services/security/control"

	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

type HostTenantView struct {
	ID            string    `json:"id"`
	Quarantined   bool      `json:"quarantined,omitempty"`
	Fault         string    `json:"fault,omitempty"`
	Lifecycle     string    `json:"lifecycle,omitempty"`
	Apps          []string  `json:"apps"`
	Members       int       `json:"members"`
	Audit         int       `json:"audit"`
	ActiveRelease string    `json:"activeRelease,omitempty"`
	Candidates    int       `json:"candidates"`
	Support       int       `json:"support"`
	FailedWork    int       `json:"failedWork"`
	Connectors    int       `json:"connectors"`
	Started       time.Time `json:"started,omitempty"`
}

type HostOverview struct {
	Now     time.Time        `json:"now"`
	Tenants []HostTenantView `json:"tenants"`
	Admins  int              `json:"admins"`
}

type LifecycleRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type SupportRead struct {
	Tenant  string                `json:"tenant"`
	Grant   string                `json:"grant"`
	At      time.Time             `json:"at"`
	Health  TenantHealth          `json:"health"`
	Audit   []platform.AuditEntry `json:"audit"`
	Answers []SupportAnswer       `json:"answers,omitempty"`
}

type SupportAnswer struct {
	Read   string `json:"read"`
	Answer any    `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

type hostAuthorizationKey struct{}

func (h *Host) hostAuthorization(subject string, authenticated bool, permission, tenant string) platform.AuthzDecision {
	return h.AuthorizationEngine.Decide(controlowner.HostAuthorizationInput(subject, authenticated, h.HostAdmins[subject], permission, tenant))
}

func hostRequestAuthorization(r *http.Request, tenant string) []acceptedformat.AuthorizationEvidence {
	evidence, found := r.Context().Value(hostAuthorizationKey{}).(acceptedformat.AuthorizationEvidence)
	if !found {
		return nil
	}
	evidence.Request.Tenant = tenant
	return []acceptedformat.AuthorizationEvidence{evidence}
}

func (h *Host) hostRoute(mux *http.ServeMux, route transport.Route, f func(http.ResponseWriter, *http.Request, string, *Tenant)) {
	h.routes = append(h.routes, route)
	mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
		subject, authenticated := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		decision := h.hostAuthorization(subject, authenticated, route.Pattern, r.PathValue("tenant"))
		if refusal := authorizationRefusal(decision); refusal != nil {
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": platform.ErrorBody{Code: refusal.Error(), Message: refusal.Message}})
			return
		}
		var tenant *Tenant
		if id := r.PathValue("tenant"); id != "" {
			for _, t := range h.currentTenants() {
				if t.ID == id {
					tenant = t
					break
				}
			}
			if tenant == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
		question := platform.AuthzRequest{Principal: "host:" + subject, Tenant: r.PathValue("tenant"), Kind: "action", Action: route.Pattern}
		evidence := acceptedformat.AuthorizationEvidence{Request: question, Decision: decision, Roles: []string{"host-admin"}}
		r = r.WithContext(context.WithValue(r.Context(), hostAuthorizationKey{}, evidence))
		f(w, r, subject, tenant)
	})
}

func (h *Host) hostConsoleRoutes(mux *http.ServeMux) {
	h.hostCompositionRoutes(mux)
	h.tenantRoutes(mux)
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/me", Summary: "Whether the caller is a host administrator, and the tenants this console serves", Answer: map[string]any{}},
		func(w http.ResponseWriter, _ *http.Request, subject string, _ *Tenant) {
			tenants := []string{}
			for _, t := range h.currentTenants() {
				tenants = append(tenants, t.ID)
			}
			slices.Sort(tenants)
			WriteJSON(w, http.StatusOK, map[string]any{"subject": subject, "tenants": tenants})
		})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/overview", Summary: "Host and tenant overview: health, quarantine, lifecycle, isolation and resource use (host administrators)", Answer: HostOverview{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, _ *Tenant) {
			out := HostOverview{Now: h.Now().UTC(), Tenants: []HostTenantView{}}
			for _, t := range h.currentTenants() {
				out.Tenants = append(out.Tenants, h.tenantView(t))
			}
			slices.SortFunc(out.Tenants, func(a, b HostTenantView) int { return strings.Compare(a.ID, b.ID) })
			out.Admins = len(h.HostAdmins)
			WriteJSON(w, http.StatusOK, out)
		})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}", Summary: "One tenant's console view (host administrators)", Answer: HostTenantView{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, h.tenantView(t))
		})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/lifecycle", Summary: "Open, suspend, resume or decommission a tenant, with a reason recorded in its audit (host administrators)", Body: LifecycleRequest{}, Answer: HostTenantView{}},
		func(w http.ResponseWriter, r *http.Request, subject string, t *Tenant) {
			var body LifecycleRequest
			if err := decodeBody(r, &body); err != nil {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if err := t.setHostLifecycle(body.Action, body.Reason, subject, h.Now(), hostRequestAuthorization(r, t.ID)...); err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, h.tenantView(t))
		})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/artifacts", Summary: "The tenant's trusted releases and saved candidates with their compatibility (host administrators)", Answer: []HostArtifactView{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.hostArtifacts())
		})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/support", Summary: "Support sessions opened for a tenant (host administrators)", Answer: []platform.SupportGrant{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.supportGrants())
		})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/support", Summary: "Open an authorized support session for a tenant member, with a reason and an end (host administrators)", Answer: platform.SupportGrant{}},
		func(w http.ResponseWriter, r *http.Request, subject string, t *Tenant) {
			var body struct {
				Member  string `json:"member"`
				Reason  string `json:"reason"`
				Minutes int    `json:"minutes"`
			}
			if err := decodeBody(r, &body); err != nil {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			grant, err := t.openSupport(body.Member, body.Reason, subject, body.Minutes, h.Now(), hostRequestAuthorization(r, t.ID)...)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, grant)
		})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/support/{grant}", Summary: "Use an authorized support session: read the tenant's health and audit as its granted member; every use is recorded (host administrators)", Answer: SupportRead{}},
		func(w http.ResponseWriter, r *http.Request, subject string, _ *Tenant) {
			grant := r.PathValue("grant")
			for _, t := range h.currentTenants() {
				read, err := t.useSupport(grant, subject, h.Now(), hostRequestAuthorization(r, t.ID)...)
				if err == controlowner.ErrUnknownGrant {
					continue
				}
				if err != nil {
					WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
					return
				}
				WriteJSON(w, http.StatusOK, read)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		})
}

func (h *Host) tenantView(t *Tenant) HostTenantView {
	now := h.Now()
	state := t
	image := t.readImage.Load()
	if image != nil {
		state = image.state
	} else {
		t.Lane.Lock()
	}
	view := HostTenantView{ID: t.ID, Lifecycle: t.console.State(), ActiveRelease: state.releases.Active(), Candidates: state.releases.Count()}
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
	if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityCounts); ok {
		view.Members = d.MemberCount()
	}
	view.Audit = len(t.Audit())
	if fault := t.Fault.Load(); fault != nil {
		view.Quarantined, view.Fault = true, fault.Reason
	}
	for _, g := range t.supportGrants() {
		if g.Expires.After(now) {
			view.Support++
		}
	}
	return view
}

func (h *Host) environmentRoutes(mux *http.ServeMux) {
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/promotions", Summary: "Promote a sealed release candidate from another tenant into this one, optionally activating it (host administrators)", Answer: PromotionResult{}},
		func(w http.ResponseWriter, r *http.Request, subject string, to *Tenant) {
			var body struct {
				From        string `json:"from"`
				Candidate   string `json:"candidate"`
				Key         string `json:"key"`
				Activate    bool   `json:"activate"`
				TargetGrant string `json:"targetGrant"`
			}
			if err := decodeBody(r, &body); err != nil || body.Key == "" || body.TargetGrant == "" {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "from, candidate, key and targetGrant are required"})
				return
			}
			from := h.tenantByID(body.From)
			if from == nil {
				WriteJSON(w, http.StatusNotFound, map[string]any{"error": "no such source tenant"})
				return
			}
			grant, err := to.grantUsable(body.TargetGrant, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
				return
			}
			member, ok := to.Member(grant.Member)
			if !ok {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": "the support session names no member"})
				return
			}
			result, err := PromoteCandidate(from, to, body.Candidate, body.Key, body.Activate, member, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, result)
		})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/migrations", Summary: "Migrate real records from another tenant into this one through its generated actions (host administrators)", Answer: platform.MigrationResult{}},
		func(w http.ResponseWriter, r *http.Request, _ string, to *Tenant) {
			var body struct {
				From        string   `json:"from"`
				Types       []string `json:"types"`
				SourceGrant string   `json:"sourceGrant"`
				TargetGrant string   `json:"targetGrant"`
			}
			if err := decodeBody(r, &body); err != nil || body.SourceGrant == "" || body.TargetGrant == "" {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "from, types, sourceGrant and targetGrant are required"})
				return
			}
			from := h.tenantByID(body.From)
			if from == nil {
				WriteJSON(w, http.StatusNotFound, map[string]any{"error": "no such source tenant"})
				return
			}
			sourceGrant, err := from.grantUsable(body.SourceGrant, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
				return
			}
			targetGrant, err := to.grantUsable(body.TargetGrant, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
				return
			}
			source, ok := from.Member(sourceGrant.Member)
			if !ok {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": "the source support session names no member"})
				return
			}
			target, ok := to.Member(targetGrant.Member)
			if !ok {
				WriteJSON(w, http.StatusForbidden, map[string]any{"error": "the target support session names no member"})
				return
			}
			result, err := MigrateRecords(from, to, source, target, body.Types, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := to.rememberMigration(result, target.ID, h.Now(), hostRequestAuthorization(r, to.ID)...); err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, result)
		})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/migrations", Summary: "What the host console moved in or out of this tenant (host administrators)", Answer: []platform.MigrationManifest{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.console.Migrations())
		})
}

func (h *Host) tenantByID(id string) *Tenant {
	for _, t := range h.currentTenants() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func decodeBody(r *http.Request, into any) error {
	return decodeBodyLimit(r, into, 1<<20)
}

func decodeBodyLimit(r *http.Request, into any, limit int64) error {
	return transport.DecodeBodyLimit(r, into, limit)
}

type HostArtifactView = controlowner.HostArtifactView

func (t *Tenant) hostArtifacts() []HostArtifactView {
	state := t
	if image := t.readImage.Load(); image != nil {
		state = image.state
	} else {
		t.Lane.Lock()
		defer t.Lane.Unlock()
	}
	return controlowner.HostArtifacts(state.releases.Ids(), state.releases.Active(), func(id string) []byte { return state.releases.Raw(id) })
}

func (t *Tenant) setHostLifecycle(action, reason, subject string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) error {
	if now.IsZero() {
		now = Now()
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	if err := controlowner.ValidLifecycleAction(action); err != nil {
		return err
	}
	request := struct{ Action, Reason string }{action, reason}
	_, err := t.acceptHostControl("lifecycle", "host:"+subject, request, now,
		platform.AuditEntry{Action: "host.lifecycle." + action, Target: t.ID + ":" + reason}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
			return platform.SupportGrant{}, controlowner.ApplyLifecycle(image, action, t.quarantined(), func() bool {
				return missingRuntime(t.desiredRuntime()) || !maps.Equal(t.desiredRuntime(), t.services.Selection()) || !slices.Equal(t.currentDisabledOwners(), t.desiredDisabledOwners()) || slices.Contains(t.desiredDisabledOwners(), controlowner.PlatformApp)
			})
		}, evidence...)
	return err
}

func (t *Tenant) openSupport(member, reason, subject string, minutes int, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) (platform.SupportGrant, error) {
	if err := controlowner.ValidSupportSession(member, reason, subject, minutes, now); err != nil {
		return platform.SupportGrant{}, err
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	request := struct {
		Member, Reason string
		Minutes        int
	}{member, reason, minutes}
	hash, err := platform.CanonicalDigest([]any{t.ID, subject, request, now.UTC()})
	if err != nil {
		return platform.SupportGrant{}, err
	}
	id := "support:" + strconv.FormatInt(now.UTC().UnixNano(), 36) + ":" + hash[:12]
	return t.acceptHostControl("support.open", "host:"+subject, request, now,
		platform.AuditEntry{Action: "host.support.open", Target: member + ":" + reason}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
			if _, ok := t.Member(member); !ok {
				return platform.SupportGrant{}, controlowner.SupportError("no such member in this tenant")
			}
			return controlowner.OpenSupportGrant(image, id, t.ID, member, reason, subject, minutes, now), nil
		}, evidence...)
}

func (t *Tenant) supportGrants() []platform.SupportGrant {
	return t.console.SupportGrants()
}

func (t *Tenant) useSupport(id, subject string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) (SupportRead, error) {
	t.Lane.Lock()
	_, err := t.acceptHostControl("support.use", "host:"+subject, struct{ ID string }{id}, now,
		platform.AuditEntry{Action: "host.support.use", Target: id}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
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
	return runtimekernel.HostControlOwner{Image: t.console.Image, Restore: t.console.Restore, Audit: &t.audit}
}

func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit platform.AuditEntry, decide func(*platform.HostControlImage) (platform.SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (platform.SupportGrant, error) {
	answer, applied, err := t.AcceptHostControl(runtimekernel.HostRequest{Tenant: t.ID, Action: action, Member: member, Request: request, At: now, Audit: audit, Evidence: evidence},
		t.hostControl(), decide, t.acceptResult, t.quarantine)
	if applied {
		t.acceptedResultApplied()
		t.changed()
	}
	return answer, err
}
