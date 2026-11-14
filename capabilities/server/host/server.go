package host

import transport "platformserver/kernel/transport"

import (
	"context"
	"log"
	"net/http"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
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
	return t.identityOwner
}

const TenantHeader = "Platform-Tenant"

const chatMaxBytes = 1 << 20

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	return idp.Members[*Tenant]{Authenticate: h.authenticate, SecondFactor: h.SecondFactor, Now: h.Now, Consoles: func() []idp.Console[*Tenant] {
		var consoles []idp.Console[*Tenant]
		for _, t := range h.currentTenants() {
			consoles = append(consoles, idp.Console[*Tenant]{Tenant: t, ID: t.ID, Sessions: consoleOf(t), Join: func(subject string, now time.Time) bool { return h.join(t, subject, now) }})
		}
		return consoles
	}}.Resolve(r.Header.Get("Authorization"), r.Header.Get(TenantHeader), r.UserAgent())
}

func (h *Host) join(t *Tenant, subject string, now time.Time) bool {
	_, refusal := decideOnImage(t, context.Background(), "identity:join", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		if t.identityOwner == nil {
			return nil, unknown()
		}
		var caller platform.Caller
		sub, refusal := controlowner.JoinSubmission(t.identityOwner, t.ID, subject, func() string {
			caller = t.automation(controlowner.PlatformApp, false)
			return caller.ID
		})
		if sub == nil {
			return nil, refusal
		}
		return t.accept(t.identityOwner, caller.Member, sub, now, acceptOptions{automation: true})
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
			if p := t.identityOwner; p != nil {
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
	if h.limiter == nil {
		h.limiter = transport.LimiterFromEnv()
	}
	if h.stats == nil {
		h.stats = transport.NewStats()
	}
	mux.HandleFunc("GET /metrics", transport.MetricsHandler(h.stats, func(r *http.Request) bool {
		return transport.ScrapeAllowed(r, h.authenticate, h.HostAdmins)
	}))
	return transport.Edge(mux, h.stats, h.limiter, TenantHeader, SchemaVersionHeader)
}

func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error, issues ...platform.FieldIssue) {
	transport.Reply(w, record, err, issues...)
}

func WriteJSON(w http.ResponseWriter, status int, v any) { transport.WriteJSON(w, status, v) }
