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

type (
	Authenticate = idp.Authenticate
	Attest       = idp.Attest
)

func Tokens(table map[string]string) Authenticate {
	return func(token string) (string, bool) { s, ok := table[token]; return s, ok }
}

func Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type Host struct {
	Release             int
	ReleaseDigest       string
	tenants             []*Tenant
	tenantsFrom         func() []*Tenant
	authenticate        Authenticate
	Now                 func() time.Time
	Recover             func(context.Context, string) error
	Web                 string
	Issuer, Client      string
	Development         bool
	Mint                func(subject string) string
	HostAdmins          map[string]bool
	RuntimeProviders    platform.RuntimeProviders
	AuthorizationEngine authz.Engine
	stats               *transport.Stats
	limiter             *transport.Limiter
	web                 *transport.WebFiles
	Templates           func() []platform.TenantTemplate
	CreateTenant        func(platform.CreateTenantRequest, string) (*Tenant, error)
	routes              []transport.Route
	SecondFactor        Attest
}

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

func NewHost(authenticate Authenticate, tenants ...*Tenant) *Host {
	h := &Host{tenants: tenants, authenticate: authenticate, Now: Now, RuntimeProviders: platform.DefaultRuntimeProviders(), AuthorizationEngine: authz.Standard{}}
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

const TenantHeader = "Platform-Tenant"

const chatMaxBytes = 1 << 20

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := r.Header.Get(TenantHeader)
	now := h.Now()
	if strings.HasPrefix(credential, controlowner.TokenPrefix) {
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
				return platform.Member{}, nil, false
			}
			if !d.NoticeSession(m.ID, credential, r.UserAgent(), "sign-in", now) {
				return platform.Member{}, nil, false
			}
			d.SeenMember(m.ID, now)
			return m, t, true
		}
	}
	for _, t := range h.currentTenants() {
		if d := consoleOf(t); d != nil && (want == "" || t.ID == want) && (!d.SecondFactorRequired() || h.SecondFactor != nil && h.SecondFactor(credential)) && d.JoinsSubject(subject) && h.join(t, d, subject, now) {
			return h.member(r)
		}
	}
	return platform.Member{}, nil, false
}

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
	h.routes = append(h.routes, namedReads...)
	rt.metadata(transport.Route{Pattern: "GET /v1/openapi.json", Summary: "This contract: the host's routes, and the entity types and action payloads the caller sees"}, func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, h.OpenAPI(t, &m))
	})
	transport.ProfileRoutes(mux)
	h.hostConsoleRoutes(mux)
	h.environmentRoutes(mux)
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
		if h.web == nil {
			h.web = transport.LoadWeb(h.Web)
		}
		web := h.web
		mux.Handle("GET /", web)
	}
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

func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error, issues ...platform.FieldIssue) {
	switch {
	case err != nil:
		body := platform.ErrorBody{Code: err.Error(), Message: err.Message, Issues: issues}
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
