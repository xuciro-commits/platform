// Package platformserver is the platform host (layer 2, ADR-0010): it runs a
// tenant's apps behind one HTTP surface — authentication, the directory, routing
// by action, read and input name, the per-caller catalog, one journal — and maps
// contract errors to HTTP once.
package platformserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"platformserver/idp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	tenants      []*Tenant
	tenantsFrom  func() []*Tenant // deployment generations; nil for an in-memory host
	authenticate Authenticate
	Now          func() time.Time
	Recover      func(context.Context, string) error // only configured with a durable journal and a tenant factory
	// Web is the directory of the workspace's build (ADR-0018), served at "/"
	// from the API's origin; empty serves no pages.
	Web string
	// SignIn tells the workspace how to sign in (GET /v1/sign-in): the OpenID
	// issuer and the workspace's client, or, with neither, the identities of
	// the host's own seats — on a development host the token is the subject,
	// on a lightweight one each served token is signed with the local key
	// (ADR-0049 D3).
	Issuer, Client string
	Development    bool
	// Mint, when set, signs the identity tokens /v1/sign-in serves: the
	// lightweight host's key (ADR-0049 D3). Without it the tokens are the
	// development ones, the subject itself.
	Mint func(subject string) string
	// HostAdmins are the subjects that may open the host console (ADR-0047
	// §6.5): an independent scope, not a tenant's administrators.
	HostAdmins map[string]bool
	// Templates and CreateTenant are how the host console adds a tenant
	// (ADR-0078 §2.2); a host without a Rebuild leaves CreateTenant nil.
	Templates    func() []TenantTemplate
	CreateTenant func(CreateTenantRequest, string) (*Tenant, error)
	routes       []Route // as Handler registered them: the API contract's source (api.go)
	SecondFactor Attest  // nil: the provider does not say
}

// SignWith makes the host take the lightweight provider's tokens and sign the
// seats it serves with them (ADR-0049 D3): the host's authenticate function is
// replaced, so a development token — the subject itself — is not accepted
// beside them. Both are set through one call so a host cannot end up signing
// with a key it does not verify.
func (h *Host) SignWith(signer *idp.Local, ttl time.Duration) {
	h.authenticate = signer.Authenticate()
	UseTokenKey(signer.Key())
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
	return &Host{tenants: tenants, authenticate: authenticate, Now: Now}
}

func (h *Host) currentTenants() []*Tenant {
	if h.tenantsFrom != nil {
		return h.tenantsFrom()
	}
	return h.tenants
}

func consoleOf(t *Tenant) *Console {
	d, _ := t.app(PlatformApp).(*Console)
	return d
}

// TenantHeader names the tenant a request is for, when the caller is a member
// of several on this host (ADR-0018 D7); without it, the first that knows them.
const TenantHeader = "Platform-Tenant"

// chatMaxBytes bounds a chat request body: a conversation with its tool
// declarations, well within what a person or a page sends (review AI-03).
const chatMaxBytes = 1 << 20

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := r.Header.Get(TenantHeader)
	now := h.Now()
	if strings.HasPrefix(credential, tokenPrefix) { // a personal token (ADR-0079 §5)
		for _, t := range h.currentTenants() {
			if d := consoleOf(t); d != nil && (want == "" || t.ID == want) {
				if m, ok := d.MemberByToken(credential, now); ok && d.noticed(m.ID, credential, r.UserAgent(), "token", now) {
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
		} else if m, ok := d.Member(subject); ok {
			if d.SecondFactorRequired() && (h.SecondFactor == nil || !h.SecondFactor(credential)) {
				return platform.Member{}, nil, false // the tenant asks a second factor this credential does not show
			}
			if !d.noticed(m.ID, credential, r.UserAgent(), "sign-in", now) {
				return platform.Member{}, nil, false // a session the member ended, or whose hours are up
			}
			d.seen(m.ID, now)
			return m, t, true
		}
	}
	for _, t := range h.currentTenants() { // nobody's member: a tenant's sign-in domain may seat them (ADR-0079 §4)
		if d := consoleOf(t); d != nil && (want == "" || t.ID == want) && (!d.SecondFactorRequired() || h.SecondFactor != nil && h.SecondFactor(credential)) && d.joins(subject) && h.join(t, d, subject, now) {
			return h.member(r)
		}
	}
	return platform.Member{}, nil, false
}

// join seats subject in t by its sign-in domain: the host records the
// decision as the platform's automation; the newcomer holds nothing until granted.
func (h *Host) join(t *Tenant, d *Console, subject string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return false
	}
	if _, ok := d.Member(subject); ok {
		return true
	}
	if !d.joinable(subject) || !d.joins(subject) {
		return false
	}
	id := d.JoinID(subject)
	payload, _ := json.Marshal(map[string]string{"subject": subject})
	err := t.submitAutomated(d, t.automation(PlatformApp, false), &pb.Submission{TenantId: t.ID, Authority: PlatformApp, IdempotencyKey: "join:" + subject,
		Target: &pb.EntityRef{Type: MemberType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaJoin, Version: 1}, Payload: payload}, now)
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
			if _, ok := d.Member(subject); ok {
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
	h.routesRecords(rt)
	h.routesBuild(rt)
	h.routesAI(rt)
	h.routesIntegration(rt)
	h.routes = append(h.routes, namedReads...) // served by GET /v1/{read}
	rt.metadata(Route{Pattern: "GET /v1/openapi.json", Summary: "This contract: the host's routes, and the entity types and action payloads the caller sees (ADR-0023)"}, func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, h.OpenAPI(t, &m))
	})
	// The host console (ADR-0047 §6.5): its own scope, its own administrators.
	h.hostConsoleRoutes(mux)
	h.environmentRoutes(mux)
	// The process is alive and holds its tenants (ADR-0027 D6); a tenant's own
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
		WriteJSON(w, http.StatusOK, map[string]any{"status": status, "tenants": len(tenants), "quarantined": quarantined})
	})
	if h.Web != "" {
		// The workspace is one page: a path that is not a file is its route.
		pages := http.FileServer(http.Dir(h.Web))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if _, err := os.Stat(filepath.Join(h.Web, filepath.FromSlash(path.Clean("/"+r.URL.Path)))); err != nil {
				r.URL.Path = "/"
			}
			pages.ServeHTTP(w, r)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, "+TenantHeader)
		w.Header().Set("Vary", "Accept-Language") // declarations are served in the request's language (ADR-0023)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
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
func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error) {
	switch {
	case err != nil:
		body := map[string]string{"code": err.Error()}
		if err.Message != "" {
			body["message"] = err.Message
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
