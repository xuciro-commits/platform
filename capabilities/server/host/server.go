package host

import transport "platformserver/kernel/transport"

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	AuthorizationEngine platform.AuthorizationEngine
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
	h := &Host{tenants: tenants, authenticate: authenticate, Now: Now, RuntimeProviders: platform.DefaultRuntimeProviders(security.Standard{}), AuthorizationEngine: security.Standard{}}
	return h
}

func (h *Host) currentTenants() []*Tenant {
	tenants := h.tenants
	if h.tenantsFrom != nil {
		tenants = h.tenantsFrom()
	}
	return tenants
}

func consoleOf(t *Tenant) platform.IdentitySessions {
	d, _ := t.App(controlowner.PlatformApp).(platform.IdentitySessions)
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

func (h *Host) join(t *Tenant, _ platform.IdentitySessions, subject string, now time.Time) bool {
	_, refusal := decideOnImage(t, context.Background(), "identity:join", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		d, ok := t.App(controlowner.PlatformApp).(platform.IdentitySessions)
		if !ok {
			return nil, unknown()
		}
		if _, known := d.MemberBySubject(subject); known {
			return nil, nil
		}
		if !d.JoinableSubject(subject) || !d.JoinsSubject(subject) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		payload, _ := json.Marshal(map[string]string{"subject": subject})
		caller := t.automation(controlowner.PlatformApp, false)
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: controlowner.PlatformApp, IdempotencyKey: "join:" + subject, Target: &pb.EntityRef{Type: controlowner.MemberType, Id: d.JoinID(subject)}, Schema: &pb.SchemaRef{Name: controlowner.SchemaJoin, Version: 1}, Payload: payload}
		return t.accept(d, caller.Member, sub, now, acceptOptions{automation: true})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		return t.finishPreparedSubmission(platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, candidate, record, refusal)
	})
	if refusal != nil {
		log.Printf("join %s to %s: %s", subject, t.ID, refusal.Error())
	}
	return refusal == nil
}

func (h *Host) tenantsOf(r *http.Request) []transport.MeTenant {
	subject, _ := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	out := []transport.MeTenant{}
	for _, t := range h.currentTenants() {
		if d := consoleOf(t); d != nil {
			if _, ok := d.MemberBySubject(subject); !ok {
				continue
			}
			name := ""
			if p, ok := t.App(controlowner.PlatformApp).(platform.IdentityProfile); ok {
				name = p.TenantRecord().Name
			}
			out = append(out, transport.MeTenant{ID: t.ID, Name: name})
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
	h.routesMonitoring(rt)
	h.routesObservability(rt)
	h.routesRecords(rt)
	h.routesResources(rt)
	h.routesEditProposals(rt)
	h.routesBuild(rt)
	h.routesBranches(rt)
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
		tenants := h.currentTenants()
		quarantined := 0
		for _, t := range tenants {
			if t.quarantined() {
				quarantined++
			}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "tenants": len(tenants), "quarantined": quarantined, "release": h.Release, "releaseDigest": h.ReleaseDigest})
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
	host := transport.RemoteOf(r)
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	if h.authenticate == nil {
		return false
	}
	credential := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if credential == "" {
		return false
	}
	subject, ok := h.authenticate(credential)
	return ok && h.HostAdmins[subject]
}

func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error, issues ...platform.FieldIssue) {
	transport.Reply(w, record, err, issues...)
}

func WriteJSON(w http.ResponseWriter, status int, v any) { transport.WriteJSON(w, status, v) }

func pageBounds(r *http.Request, offsetDefault, limitDefault int) (int, int) {
	return transport.PageBounds(r, offsetDefault, limitDefault)
}
