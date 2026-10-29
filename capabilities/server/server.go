// The HTTP surface of the host: authentication, the directory, routing by
// action, read and input name, the per-caller catalog, one journal, and the one
// place contract errors become HTTP. The package's component map, lock
// obligations and pipeline entry points are in doc.go.
package platformserver

import transport "platformserver/kernel/transport"

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"platformserver/idp"
	controlowner "platformserver/kernel/control"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/platform/authz"
)

// Authenticate and Attest are the identity provider's two answers (package idp).
type (
	Authenticate = idp.Authenticate
	Attest       = idp.Attest
)

// Tokens authenticates with a fixed token → subject table (development and tests).
func Tokens(table map[string]string) Authenticate {
	return func(token string) (string, bool) { s, ok := table[token]; return s, ok }
}

// Now is the server clock at the journal's precision (PostgreSQL keeps
// microseconds), so replayed records carry the times first recorded.
func Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Host serves tenants; each tenant's directory resolves its members.
type Host struct {
	Release       int
	ReleaseDigest string
	tenants       []*Tenant
	tenantsFrom   func() []*Tenant // deployment generations; nil for an in-memory host
	authenticate  Authenticate
	Now           func() time.Time
	Recover       func(context.Context, string) error // only configured with a durable journal and a tenant factory
	// Web is the directory of the workspace's build, served at "/"
	// from the API's origin; empty serves no pages.
	Web string
	// SignIn tells the workspace how to sign in (GET /v1/sign-in): the OpenID
	// issuer and the workspace's client, or, with neither, the identities of
	// the host's own seats — on a development host the token is the subject,
	// on a lightweight one each served token is signed with the local key.
	Issuer, Client string
	Development    bool
	// Mint, when set, signs the identity tokens /v1/sign-in serves: the
	// lightweight host's key. Without it the tokens are the
	// development ones, the subject itself.
	Mint func(subject string) string
	// HostAdmins are the subjects that may open the host console: an independent scope, not a tenant's administrators.
	HostAdmins          map[string]bool
	RuntimeProviders    RuntimeProviders
	AuthorizationEngine authz.Engine
	stats               *transport.Stats
	limiter             *transport.Limiter
	web                 *transport.WebFiles
	// Templates and CreateTenant are how the host console adds a tenant;
	// a host without a Rebuild leaves CreateTenant nil.
	Templates    func() []TenantTemplate
	CreateTenant func(CreateTenantRequest, string) (*Tenant, error)
	routes       []transport.Route // as Handler registered them: the API contract's source (api.go)
	SecondFactor Attest            // nil: the provider does not say
}

// SignWith makes the host take the lightweight provider's tokens and sign the
// seats it serves with them: the host's authenticate function is
// replaced, so a development token — the subject itself — is not accepted
// beside them. Both are set through one call so a host cannot end up signing
// with a key it does not verify.
func (h *Host) SignWith(signer *idp.Local, ttl time.Duration) {
	h.authenticate = signer.Authenticate()
	controlowner.UseTokenKey(signer.Key())
	h.Mint = func(subject string) string {
		token, err := signer.Mint(subject, ttl, h.Now())
		if err != nil {
			log.Printf("mint token: %v", err)
		}
		return token
	}
}

// NewHost serves tenants; a tenant's members come from its console (the platform app).
func NewHost(authenticate Authenticate, tenants ...*Tenant) *Host {
	h := &Host{tenants: tenants, authenticate: authenticate, Now: Now, RuntimeProviders: DefaultRuntimeProviders(), AuthorizationEngine: authz.Standard{}}
	h.bindSystemDefinitions(tenants)
	return h
}

func (h *Host) currentTenants() []*Tenant {
	tenants := h.tenants
	if h.tenantsFrom != nil {
		tenants = h.tenantsFrom()
	}
	h.bindSystemDefinitions(tenants)
	return tenants
}

func consoleOf(t *Tenant) platform.IdentitySessions {
	d, _ := t.app(controlowner.PlatformApp).(platform.IdentitySessions)
	return d
}

// TenantHeader names the tenant a request is for, when the caller is a member
// of several on this host; without it, the first that knows them.
const TenantHeader = "Platform-Tenant"

// chatMaxBytes bounds a chat request body: a conversation with its tool
// declarations, well within what a person or a page sends (review AI-03).
const chatMaxBytes = 1 << 20

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := r.Header.Get(TenantHeader)
	now := h.Now()
	if strings.HasPrefix(credential, controlowner.TokenPrefix) { // a personal token
		for _, t := range h.currentTenants() {
			if d := consoleOf(t); d != nil && (want == "" || t.ID == want) {
				if m, ok := d.MemberByToken(credential, now); ok && d.NoticeSession(m.ID, credential, r.UserAgent(), "token", now) {
					return m, t, true
				}
			}
		}
		return platform.Member{}, nil, false
	}
	subject, ok := h.authenticate(credential)
	if !ok {
		return platform.Member{}, nil, false
	}
	for _, t := range h.currentTenants() {
		if d := consoleOf(t); d == nil || want != "" && t.ID != want {
			continue
		} else if m, ok := d.MemberBySubject(subject); ok {
			if d.SecondFactorRequired() && (h.SecondFactor == nil || !h.SecondFactor(credential)) {
				return platform.Member{}, nil, false // the tenant asks a second factor this credential does not show
			}
			if !d.NoticeSession(m.ID, credential, r.UserAgent(), "sign-in", now) {
				return platform.Member{}, nil, false // a session the member ended, or whose hours are up
			}
			d.SeenMember(m.ID, now)
			return m, t, true
		}
	}
	for _, t := range h.currentTenants() { // nobody's member: a tenant's sign-in domain may seat them
		if d := consoleOf(t); d != nil && (want == "" || t.ID == want) && (!d.SecondFactorRequired() || h.SecondFactor != nil && h.SecondFactor(credential)) && d.JoinsSubject(subject) && h.join(t, d, subject, now) {
			return h.member(r)
		}
	}
	return platform.Member{}, nil, false
}

// join seats subject in t by its sign-in domain: the host records the
// decision as the platform's automation; the newcomer holds nothing until granted.
func (h *Host) join(t *Tenant, d platform.IdentitySessions, subject string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return false
	}
	if _, ok := d.MemberBySubject(subject); ok {
		return true
	}
	if !d.JoinableSubject(subject) || !d.JoinsSubject(subject) {
		return false
	}
	id := d.JoinID(subject)
	payload, _ := json.Marshal(map[string]string{"subject": subject})
	err := t.submitAutomated(d, t.automation(controlowner.PlatformApp, false), &pb.Submission{TenantId: t.ID, Authority: controlowner.PlatformApp, IdempotencyKey: "join:" + subject,
		Target: &pb.EntityRef{Type: controlowner.MemberType, Id: id}, Schema: &pb.SchemaRef{Name: controlowner.SchemaJoin, Version: 1}, Payload: payload}, now)
	if err != nil {
		log.Printf("join %s to %s: %s", subject, t.ID, err.Error())
	}
	return err == nil
}

// tenantsOf are the tenants on this host where the request's subject is a member.
func (h *Host) tenantsOf(r *http.Request) []string {
	subject, _ := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	out := []string{}
	for _, t := range h.currentTenants() {
		if d := consoleOf(t); d != nil {
			if _, ok := d.MemberBySubject(subject); ok {
				out = append(out, t.ID)
			}
		}
	}
	return out
}

// Handler serves the host's HTTP surface, with CORS for browser clients.
func (h *Host) Handler() http.Handler {
	rt := &routes{h: h, mux: http.NewServeMux()}
	mux := rt.mux
	h.routes = nil
	h.routesCore(rt)
	h.routesDiscovery(rt)
	h.routesApplications(rt)
	h.routesRecords(rt)
	h.routesBuild(rt)
	h.routesAI(rt)
	h.routesCredentials(rt)
	h.routesIntegration(rt)
	h.routes = append(h.routes, namedReads...) // served by GET /v1/{read}
	rt.metadata(transport.Route{Pattern: "GET /v1/openapi.json", Summary: "This contract: the host's routes, and the entity types and action payloads the caller sees"}, func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, h.OpenAPI(t, &m))
	})
	// The runtime's own profiles, when the operator asks for them.
	transport.ProfileRoutes(mux)
	// The host console: its own scope, its own administrators.
	h.hostConsoleRoutes(mux)
	h.environmentRoutes(mux)
	// The process is alive and holds its tenants; a tenant's own
	// health is the administrators' read /v1/health.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		quarantined := 0
		tenants := h.currentTenants()
		for _, t := range tenants {
			if t.quarantined() {
				quarantined++
			}
		}
		status := "ok"
		if quarantined > 0 {
			status = "degraded"
		}
		WriteJSON(w, http.StatusOK, map[string]any{"status": status, "tenants": len(tenants), "quarantined": quarantined, "release": h.Release, "releaseDigest": h.ReleaseDigest})
	})
	if h.Web != "" {
		// The workspace is one page held in memory: a path that is not a file
		// is its route, and a hashed asset is cached for a year.
		if h.web == nil {
			h.web = transport.LoadWeb(h.Web)
		}
		web := h.web
		mux.Handle("GET /", web)
	}
	// What the process has served, in the exposition format Prometheus reads.
	// The counts name route patterns, never tenants or members.
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		if !h.scrapeAllowed(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"ERROR_CODE_POLICY_DENIED","message":"Metrics need a credential or a loopback caller."}}`))
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		h.stats.WritePrometheus(w)
	})
	if h.limiter == nil {
		// A person never reaches this; a loop or a leaked token does.
		// PLATFORM_RATE_LIMIT=0 turns it off.
		rate, _ := strconv.ParseFloat(os.Getenv("PLATFORM_RATE_LIMIT"), 64)
		if rate == 0 && os.Getenv("PLATFORM_RATE_LIMIT") == "" {
			rate = 600
		}
		burst, _ := strconv.ParseFloat(os.Getenv("PLATFORM_RATE_BURST"), 64)
		if burst == 0 {
			burst = rate * 2
		}
		h.limiter = transport.NewLimiter(rate, burst)
	}
	if h.stats == nil {
		h.stats = transport.NewStats()
	}
	return transport.Edge(mux, h.stats, h.limiter, TenantHeader, SchemaVersionHeader)
}

// scrapeAllowed keeps the process counters off the open internet: a scrape
// needs a credential this host accepts, or a loopback caller.
func (h *Host) scrapeAllowed(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		if _, _, ok := h.member(r); ok {
			return true
		}
	}
	host := transport.RemoteOf(r)
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

var statuses = map[pb.ErrorCode]int{
	pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:     http.StatusBadRequest,
	pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA:       http.StatusBadRequest,
	pb.ErrorCode_ERROR_CODE_NOT_FOUND:            http.StatusNotFound,
	pb.ErrorCode_ERROR_CODE_POLICY_DENIED:        http.StatusForbidden,
	pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY:        http.StatusMisdirectedRequest,
	pb.ErrorCode_ERROR_CODE_CONFLICT:             http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT: http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE:       http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    http.StatusUnprocessableEntity,
}

// Reply writes {"record": …}, {"ok": true} or {"error": {"code": …}}; clients map
// the code to outbox events (K5 A8), never the HTTP status.
func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error, issues ...platform.FieldIssue) {
	switch {
	case err != nil:
		body := ErrorBody{Code: err.Error(), Message: err.Message, Issues: issues}
		if len(issues) > 0 && issues[0].Constraint != "" {
			body.Code = "CONSTRAINT_VIOLATION"
			body.Message = issues[0].Constraint + ": " + issues[0].Message
		}
		status := statuses[err.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		WriteJSON(w, status, map[string]any{"error": body})
	case record == nil:
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		raw, _ := protojson.Marshal(record)
		WriteJSON(w, http.StatusOK, map[string]json.RawMessage{"record": raw})
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func pageBounds(r *http.Request, offsetDefault, limitDefault int) (int, int) {
	offset, limit := offsetDefault, limitDefault
	if value := r.URL.Query().Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return -1, -1
		}
		offset = parsed
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return -1, -1
		}
		limit = parsed
	}
	return offset, limit
}
