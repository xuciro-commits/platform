package platformserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

// The host console (ADR-0047 §6.5) is the host's own management scope: it is
// independent of every tenant's administration and never inherits a customer
// administrator's rights. Its operators are subjects the deployment names as
// host administrators; a route under /v1/host answers their bearer token and
// nothing else. Four areas live here, as the ADR lays them out:
//
//	overview       host and tenants: health, quarantine, isolation, resources
//	lifecycle      open, suspend, resume and decommission a tenant
//	artifacts      trusted releases, candidates and their compatibility
//	support        authorized support records, each use written to the tenant's audit
//
// A tenant's ordinary data stays behind its administrators: the console reads
// the same health the tenant sees and counts, never record contents. Support
// reads run as a member the operator is authorized for, and every use is
// recorded in that tenant's audit.

// HostTenantView is one tenant as the host console shows it.
type HostTenantView struct {
	ID            string    `json:"id"`
	Quarantined   bool      `json:"quarantined,omitempty"`
	Fault         string    `json:"fault,omitempty"`
	Lifecycle     string    `json:"lifecycle,omitempty"` // "", "suspended" or "decommissioned"
	Apps          []string  `json:"apps"`
	Members       int       `json:"members"`
	Audit         int       `json:"audit"`
	ActiveRelease string    `json:"activeRelease,omitempty"`
	Candidates    int       `json:"candidates"`
	Support       int       `json:"support"` // open support grants
	FailedWork    int       `json:"failedWork"`
	Connectors    int       `json:"connectors"`
	Started       time.Time `json:"started,omitempty"`
}

// HostOverview is the console's first read.
type HostOverview struct {
	Now     time.Time        `json:"now"`
	Tenants []HostTenantView `json:"tenants"`
	Admins  int              `json:"admins"`
}

// LifecycleRequest asks the console to move one tenant's lifecycle.
type LifecycleRequest struct {
	Action string `json:"action"` // "suspend", "resume" or "decommission"
	Reason string `json:"reason,omitempty"`
}

// SupportGrant is one authorized support session: who may look at which
// tenant, why, until when, and who opened it.
type SupportGrant struct {
	ID      string    `json:"id"`
	Tenant  string    `json:"tenant"`
	Member  string    `json:"member"`
	Reason  string    `json:"reason"`
	Opened  string    `json:"opened"`
	Expires time.Time `json:"expires"`
	Uses    int       `json:"uses"`
}

// SupportRead is one diagnosis the console performed under a grant.
type SupportRead struct {
	Tenant  string          `json:"tenant"`
	Grant   string          `json:"grant"`
	At      time.Time       `json:"at"`
	Health  TenantHealth    `json:"health"`
	Audit   []AuditEntry    `json:"audit"`
	Answers []SupportAnswer `json:"answers,omitempty"`
}

// SupportAnswer is one record the support read could reach as the granted member.
type SupportAnswer struct {
	Read   string `json:"read"`
	Answer any    `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

// hostAdmin authenticates the bearer token as a subject the deployment names a
// host administrator. A tenant member is not one by being a member.
func (h *Host) hostAdmin(r *http.Request) (string, bool) {
	subject, ok := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if !ok || len(h.HostAdmins) == 0 {
		return "", false
	}
	if h.HostAdmins[subject] {
		return subject, true
	}
	return "", false
}

// hostRoute registers a console route: the API contract lists it, and the
// handler runs only for a host administrator.
func (h *Host) hostRoute(mux *http.ServeMux, route Route, f func(http.ResponseWriter, *http.Request, string, *Tenant)) {
	h.routes = append(h.routes, route)
	mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
		subject, ok := h.hostAdmin(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
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
		f(w, r, subject, tenant)
	})
}

// hostConsoleRoutes registers the console's four areas.
func (h *Host) hostConsoleRoutes(mux *http.ServeMux) {
	h.tenantRoutes(mux)
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/me", Summary: "Whether the caller is a host administrator, and the tenants this console serves", Answer: map[string]any{}},
		func(w http.ResponseWriter, _ *http.Request, subject string, _ *Tenant) {
			tenants := []string{}
			for _, t := range h.currentTenants() {
				tenants = append(tenants, t.ID)
			}
			slices.Sort(tenants)
			WriteJSON(w, http.StatusOK, map[string]any{"subject": subject, "tenants": tenants})
		})
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/overview", Summary: "Host and tenant overview: health, quarantine, lifecycle, isolation and resource use (host administrators)", Answer: HostOverview{}},
		func(w http.ResponseWriter, _ *http.Request, subject string, _ *Tenant) {
			out := HostOverview{Now: h.Now().UTC(), Tenants: []HostTenantView{}}
			for _, t := range h.currentTenants() {
				out.Tenants = append(out.Tenants, h.tenantView(t))
			}
			slices.SortFunc(out.Tenants, func(a, b HostTenantView) int { return strings.Compare(a.ID, b.ID) })
			out.Admins = len(h.HostAdmins)
			_ = subject
			WriteJSON(w, http.StatusOK, out)
		})
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/tenants/{tenant}", Summary: "One tenant's console view (host administrators)", Answer: HostTenantView{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, h.tenantView(t))
		})
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/tenants/{tenant}/lifecycle", Summary: "Open, suspend, resume or decommission a tenant, with a reason recorded in its audit (host administrators)", Body: LifecycleRequest{}, Answer: HostTenantView{}},
		func(w http.ResponseWriter, r *http.Request, subject string, t *Tenant) {
			var body LifecycleRequest
			if err := decodeBody(r, &body); err != nil {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if err := t.setHostLifecycle(body.Action, body.Reason, subject, h.Now()); err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, h.tenantView(t))
		})
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/tenants/{tenant}/artifacts", Summary: "The tenant's trusted releases and saved candidates with their compatibility (host administrators)", Answer: []HostArtifactView{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.hostArtifacts())
		})
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/tenants/{tenant}/support", Summary: "Support sessions opened for a tenant (host administrators)", Answer: []SupportGrant{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.supportGrants(h.Now()))
		})
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/tenants/{tenant}/support", Summary: "Open an authorized support session for a tenant member, with a reason and an end (host administrators)", Answer: SupportGrant{}},
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
			grant, err := t.openSupport(body.Member, body.Reason, subject, body.Minutes, h.Now())
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusOK, grant)
		})
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/support/{grant}", Summary: "Use an authorized support session: read the tenant's health and audit as its granted member; every use is recorded (host administrators)", Answer: SupportRead{}},
		func(w http.ResponseWriter, r *http.Request, subject string, _ *Tenant) {
			grant := r.PathValue("grant")
			for _, t := range h.currentTenants() {
				read, err := t.useSupport(grant, subject, h.Now())
				if err == errUnknownGrant {
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

// tenantView summarizes one tenant without reading its records: the same counts
// its own console shows, plus the host lifecycle.
func (h *Host) tenantView(t *Tenant) HostTenantView {
	now := h.Now()
	t.mu.Lock()
	view := HostTenantView{ID: t.ID, Lifecycle: t.lifecycle(), ActiveRelease: t.activeRelease,
		Candidates: len(t.releaseCandidates)}
	t.mu.Unlock()
	health := t.Health(now)
	view.Started, view.FailedWork, view.Connectors = health.Started, health.Failed, health.Connectors
	view.Apps = []string{}
	for _, a := range t.Apps() {
		view.Apps = append(view.Apps, a.ID)
	}
	slices.Sort(view.Apps)
	if d := consoleOf(t); d != nil {
		d.mu.Lock()
		view.Members = len(d.members)
		d.mu.Unlock()
	}
	view.Audit = len(t.Audit())
	if fault := t.fault.Load(); fault != nil {
		view.Quarantined, view.Fault = true, fault.Reason
	}
	for _, g := range t.supportGrants(now) {
		if g.Expires.After(now) {
			view.Support++
		}
	}
	return view
}

// promoteRoute and migrateRoute are the console's environment operations: a
// sealed candidate moves to another tenant, or real records do. Each names the
// support sessions that authorize reading the source and writing the target, so
// the console acts as a member the operator was authorized for — never as a
// customer administrator it invented.
func (h *Host) environmentRoutes(mux *http.ServeMux) {
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/tenants/{tenant}/promotions", Summary: "Promote a sealed release candidate from another tenant into this one, optionally activating it (host administrators)", Answer: PromotionResult{}},
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
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/tenants/{tenant}/migrations", Summary: "Migrate real records from another tenant into this one through its generated actions (host administrators)", Answer: MigrationResult{}},
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
			to.rememberMigration(result, target.ID, h.Now())
			WriteJSON(w, http.StatusOK, result)
		})
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/tenants/{tenant}/migrations", Summary: "What the host console moved in or out of this tenant (host administrators)", Answer: []MigrationManifest{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.Migrations())
		})
}

// tenantByID finds one tenant of this host.
func (h *Host) tenantByID(id string) *Tenant {
	for _, t := range h.currentTenants() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// decodeBody reads one JSON request body of the console's own size.
func decodeBody(r *http.Request, into any) error {
	body := http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

// HostArtifactView is one tenant's release inventory for the console: the
// active release and every saved candidate with its digest, never its bytes.
type HostArtifactView struct {
	Candidate string `json:"candidate"`
	Active    bool   `json:"active,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Size      int    `json:"size"`
	Assets    int    `json:"assets"`
	Verified  bool   `json:"verified"`
	Note      string `json:"note,omitempty"`
}

// hostArtifacts lists what the tenant could be running, newest first.
func (t *Tenant) hostArtifacts() []HostArtifactView {
	t.mu.Lock()
	ids := make([]string, 0, len(t.releaseCandidates))
	for id := range t.releaseCandidates {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := []HostArtifactView{}
	for _, id := range ids {
		raw := t.releaseCandidates[id]
		view := HostArtifactView{Candidate: id, Active: id == t.activeRelease, Size: len(raw)}
		if candidate, err := platform.ReadCandidate(id, raw); err == nil {
			view.Digest, view.Assets, view.Verified = candidate.ID, len(candidate.Assets), true
		} else {
			view.Note = err.Error()
		}
		out = append(out, view)
	}
	t.mu.Unlock()
	slices.SortStableFunc(out, func(a, b HostArtifactView) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Candidate, b.Candidate)
	})
	return out
}

// errUnknownGrant says the grant belongs to another tenant.
var errUnknownGrant = errSupport("unknown support grant")

type errSupport string

func (e errSupport) Error() string { return string(e) }

// setHostLifecycle moves a tenant's host lifecycle. Suspending keeps every
// record and blocks ordinary requests; decommissioning does the same and is
// terminal until the console opens the tenant again. Both are recorded in the
// tenant's audit next to the reason, so support work leaves a trace.
func (t *Tenant) setHostLifecycle(action, reason, subject string, now time.Time) error {
	if now.IsZero() {
		now = Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch action {
	case "suspend", "decommission", "resume", "open":
	default:
		return errSupport("lifecycle action must be suspend, resume, decommission or open")
	}
	if t.quarantined() && action != "open" && action != "resume" {
		return errSupport("a quarantined tenant is already stopped; repair it through recovery")
	}
	switch action {
	case "suspend", "decommission":
		t.setLifecycle(action)
	case "resume", "open":
		t.setLifecycle("")
	}
	t.remember(AuditEntry{At: now.UTC(), Member: "host:" + subject, App: PlatformApp,
		Action: "host.lifecycle." + action, Target: t.ID + ":" + reason})
	return nil
}

// hostSuspended reports whether the host console stopped this tenant.
func (t *Tenant) hostSuspended() bool {
	l := t.lifecycle()
	return l != "" && l != "open"
}

func (t *Tenant) lifecycle() string {
	if p := t.hostLifecycle.Load(); p != nil {
		return *p
	}
	return ""
}

func (t *Tenant) setLifecycle(v string) { t.hostLifecycle.Store(&v) }

// openSupport authorizes a support session for a member of this tenant. The
// member must exist: the console looks at what that member may look at, so it
// can never exceed a real member's reads, and the grant bounds the time.
func (t *Tenant) openSupport(member, reason, subject string, minutes int, now time.Time) (SupportGrant, error) {
	if member == "" || reason == "" || minutes < 1 || minutes > 24*60 {
		return SupportGrant{}, errSupport("a support session needs a member, a reason and 1 to 1440 minutes")
	}
	if _, ok := t.Member(member); !ok {
		return SupportGrant{}, errSupport("no such member in this tenant")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	id := "support:" + strconv.FormatInt(now.UTC().UnixNano(), 36)
	grant := SupportGrant{ID: id, Tenant: t.ID, Member: member, Reason: reason, Opened: subject,
		Expires: now.UTC().Add(time.Duration(minutes) * time.Minute)}
	t.support = append(t.support, grant)
	t.remember(AuditEntry{At: now.UTC(), Member: "host:" + subject, App: PlatformApp,
		Action: "host.support.open", Target: member + ":" + reason})
	return grant, nil
}

// supportGrants lists the tenant's sessions, newest first.
func (t *Tenant) supportGrants(now time.Time) []SupportGrant {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := slices.Clone(t.support)
	slices.Reverse(out)
	_ = now
	return out
}

// useSupport performs one authorized read of the tenant: health, the audit tail
// and the reads the granted member may make. The use is recorded in the audit
// with the operator and the grant, and the grant's count moves.
func (t *Tenant) useSupport(id, subject string, now time.Time) (SupportRead, error) {
	t.mu.Lock()
	index := slices.IndexFunc(t.support, func(g SupportGrant) bool { return g.ID == id })
	if index < 0 {
		t.mu.Unlock()
		return SupportRead{}, errUnknownGrant
	}
	grant := &t.support[index]
	if !grant.Expires.After(now) {
		t.mu.Unlock()
		return SupportRead{}, errSupport("the support session has ended")
	}
	grant.Uses++
	t.remember(AuditEntry{At: now.UTC(), Member: "host:" + subject, App: PlatformApp,
		Action: "host.support.use", Target: grant.ID})
	audit := tailAudit(t.Audit(), 20)
	t.mu.Unlock()
	// Health takes its own locks and is read outside the tenant's.
	return SupportRead{Tenant: t.ID, Grant: id, At: now.UTC(), Health: t.Health(now), Audit: audit}, nil
}

func tailAudit(entries []AuditEntry, n int) []AuditEntry {
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return slices.Clone(entries)
}
