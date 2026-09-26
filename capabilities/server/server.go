// Package platformserver is the platform host (layer 2, ADR-0010): it runs a
// tenant's apps behind one HTTP surface — authentication, the directory, routing
// by action, read and input name, the per-caller catalog, one journal — and maps
// contract errors to HTTP once.
package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// Authenticate turns a bearer credential into a subject ("user:<email>",
// "client:<id>"); false rejects the request. See OIDC and Tokens.
type Authenticate func(credential string) (subject string, ok bool)

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
	consoles     map[*Tenant]*Console
	authenticate Authenticate
	Now          func() time.Time
	// Web is the directory of the workspace's build (ADR-0018), served at "/"
	// from the API's origin; empty serves no pages.
	Web string
	// SignIn tells the workspace how to sign in (GET /v1/sign-in): the OpenID
	// issuer and the workspace's client, or, with neither, the development
	// identities of a host whose tokens are the subjects.
	Issuer, Client string
	Development    bool
	routes         []Route // as Handler registered them: the API contract's source (api.go)
}

// NewHost serves tenants; a tenant's members come from its console (the platform app).
func NewHost(authenticate Authenticate, tenants ...*Tenant) *Host {
	h := &Host{tenants: tenants, consoles: map[*Tenant]*Console{}, authenticate: authenticate, Now: Now}
	for _, t := range tenants {
		if d, ok := t.app(PlatformApp).(*Console); ok {
			h.consoles[t] = d
		}
	}
	return h
}

// TenantHeader names the tenant a request is for, when the caller is a member
// of several on this host (ADR-0018 D7); without it, the first that knows them.
const TenantHeader = "Platform-Tenant"

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	subject, ok := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if !ok {
		return platform.Member{}, nil, false
	}
	want := r.Header.Get(TenantHeader)
	for _, t := range h.tenants {
		if d := h.consoles[t]; d == nil || want != "" && t.ID != want {
			continue
		} else if m, ok := d.Member(subject); ok {
			return m, t, true
		}
	}
	return platform.Member{}, nil, false
}

// tenantsOf are the tenants on this host where the request's subject is a member.
func (h *Host) tenantsOf(r *http.Request) []string {
	subject, _ := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	out := []string{}
	for _, t := range h.tenants {
		if d := h.consoles[t]; d != nil {
			if _, ok := d.Member(subject); ok {
				out = append(out, t.ID)
			}
		}
	}
	return out
}

// Handler serves the host's HTTP surface, with CORS for browser clients.
func (h *Host) Handler() http.Handler {
	mux := http.NewServeMux()
	h.routes = nil
	public := func(route Route, f http.HandlerFunc) {
		route.Public = true
		h.routes = append(h.routes, route)
		mux.HandleFunc(route.Pattern, f)
	}
	handle := func(route Route, f func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant)) {
		h.routes = append(h.routes, route)
		mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
			m, t, ok := h.member(r)
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f(w, r, m, t)
		})
	}
	handle(Route{Pattern: "GET /v1/changes", Summary: "Server-sent events: \"changed\" each time the tenant takes inputs, so a client reads again what it shows (F-32)", Answer: ""},
		func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) { followChanges(w, r, t) })
	handle(Route{Pattern: "POST /v1/submissions", Summary: "Submit a decision: an action on a target, received in the kernel's order (K6) and journaled once accepted", Body: pb.Submission{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		record, err := t.Submit(m, sub, h.Now())
		Reply(w, record, err)
	})
	handle(Route{Pattern: "POST /v1/connectors/{input}", Summary: "Deliver a connector's batch or page as the connector's member (K8)", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		out, err := t.Input(m, r.PathValue("input"), body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, err)
	})
	handle(Route{Pattern: "GET /v1/me", Summary: "Who the caller is on this host: tenant, member, the apps they may open, their language", Answer: MeView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		lang := t.Language(m, r)
		WriteJSON(w, http.StatusOK, t.Translate(MeView{TenantID: m.Tenant, PrincipalID: m.ID, Profile: m, Apps: t.AppsOf(m), Tenants: h.tenantsOf(r),
			Language: lang, Languages: t.languages(), Preferred: m.Language, Currency: t.setting(t.automation(PlatformApp, false), SettingCurrency)}, lang))
	})
	handle(Route{Pattern: "GET /v1/declarations", Summary: "The data classes and their authorities the tenant's apps declare (K5)", Answer: []*pb.AuthorityDeclaration{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		out := []json.RawMessage{}
		for _, d := range t.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/actions", Summary: "The caller's catalog: the actions their roles permit, in their language (ADR-0008)", Answer: []platform.Action{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Translate(t.Catalog(m), t.Language(m, r)))
	})
	handle(Route{Pattern: "GET /v1/apps", Summary: "The tenant's apps from their manifests", Answer: []AppInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Translate(t.Apps(), t.Language(m, r)))
	})
	handle(Route{Pattern: "GET /v1/protocols", Summary: "The protocols apps provide and consume, and the provider bound to each (ADR-0011)", Answer: []ProtocolInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Translate(t.Protocols(), t.Language(m, r)))
	})
	handle(Route{Pattern: "POST /v1/protocols/{protocol}/{version}/{action}", Summary: "Call a protocol's action at the provider the tenant binds", Body: ProtocolCall{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var call struct {
			Target, IdempotencyKey string
			Payload                json.RawMessage
		}
		if json.NewDecoder(r.Body).Decode(&call) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		_, record, err := t.Invoke(m, r.PathValue("protocol")+"/"+r.PathValue("version"), r.PathValue("action"), call.Target, call.Payload, call.IdempotencyKey, h.Now())
		Reply(w, record, err)
	})
	handle(Route{Pattern: "POST /v1/ai/chat", Summary: "Call a model the caller may use; the call is metered (ADR-0015)", Body: ChatRequest{}, Answer: ChatAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var req ChatRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		answer, err, failure := t.Chat(m, req, h.Now())
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil:
			WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail}, "usage": answer.Usage})
		default:
			WriteJSON(w, http.StatusOK, answer)
		}
	})
	handle(Route{Pattern: "GET /v1/ai/providers/{id}/models", Summary: "A provider's catalog of models", Answer: []CatalogModel{}, Query: []Param{{"refresh", "true reads the catalog from the provider again"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		models, err, failure := t.ProviderModels(m, r.PathValue("id"), r.URL.Query().Get("refresh") == "true")
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil:
			WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail}})
		default:
			WriteJSON(w, http.StatusOK, models)
		}
	})
	handle(Route{Pattern: "GET /v1/ai/vendors", Summary: "The vendors a provider may be", Answer: []ai.Vendor{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, _ *Tenant) {
		WriteJSON(w, http.StatusOK, ai.Vendors)
	})
	handle(Route{Pattern: "GET /v1/entities", Summary: "The entity types of the apps the caller holds a role in, with their meaning, in their language (ADR-0016, ADR-0023)", Answer: []platform.EntityInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Translate(t.Entities(m), t.Language(m, r)))
	})
	handle(Route{Pattern: "GET /v1/records/{type}", Summary: "A page of an entity type's records within the caller's scope", Answer: RecordPage{}, Query: []Param{{"domain", "Filters in the prefix form, JSON: [[\"stage\",\"=\",\"open\"]]"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated; -field for descending"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search"), Archived: r.URL.Query().Get("archived") == "true"}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		page, err := t.Records(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	handle(Route{Pattern: "GET /v1/context/{type}/{id}", Summary: "A record with its history, references, links, flows and tasks: the context graph (ADR-0021)", Answer: ContextView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.Context(&m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	handle(Route{Pattern: "GET /v1/search", Summary: "Records of every type the caller may read, by words and by the types' names", Answer: []Hit{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Search(&m, r.URL.Query().Get("q"), h.Now()))
	})
	public(Route{Pattern: "GET /a2a/{tenant}/{agent}/.well-known/agent-card.json", Summary: "A published agent's A2A 1.0 card (ADR-0022)"}, func(w http.ResponseWriter, r *http.Request) {
		i := slices.IndexFunc(h.tenants, func(t *Tenant) bool { return t.ID == r.PathValue("tenant") })
		if i < 0 || !h.tenants[i].published(r.PathValue("agent")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		WriteJSON(w, http.StatusOK, h.agentCard(r, h.tenants[i], r.PathValue("agent")))
	})
	public(Route{Pattern: "POST /a2a/{tenant}/{agent}", Summary: "A2A 1.0 JSON-RPC to a published agent; the caller signs in with the host's issuer", Body: json.RawMessage{}}, h.serveA2A)
	handle(Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first (ADR-0022)", Answer: []Passage{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	handle(Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for agent and AI administrators", Answer: []Transcript{}, Query: []Param{{"run", "An agent run"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[AgentApp] != AgentAdmin && m.Roles[ai.ID] != ai.Admin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		WriteJSON(w, http.StatusOK, t.Transcripts(r.URL.Query().Get("run"), 50))
	})
	handle(Route{Pattern: "GET /v1/aggregates/{type}", Summary: "Groups and measures of an entity type's records within the caller's scope (ADR-0019)", Answer: Aggregate{}, Query: []Param{{"group", "Fields or field:month, comma-separated"}, {"measure", "count, sum:field, avg:field, min:field, max:field"}, {"domain", "Filters, JSON"}, {"search", "Words to find"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		p := r.URL.Query()
		q := AggregateQuery{Domain: json.RawMessage(p.Get("domain")), Search: p.Get("search"), Archived: p.Get("archived") == "true"}
		if g := p.Get("group"); g != "" {
			q.Groups = strings.Split(g, ",")
		}
		if m := p.Get("measure"); m != "" {
			q.Measures = strings.Split(m, ",")
		}
		out, err := t.Aggregate(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/records/{type}/{id}", Summary: "A record with its history and related records", Answer: RecordView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.RecordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	handle(Route{Pattern: "POST /mcp", Summary: "MCP: the caller's catalog as tools (JSON-RPC)", Body: json.RawMessage{}}, h.mcp)
	handle(Route{Pattern: "GET /v1/{read}", Summary: "A named read of an app, such as inbox, requests, notifications, views, settings, members or audit; the app decides who may read it"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.Read(m, r.PathValue("read"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if declarationReads[r.PathValue("read")] {
			out = t.Translate(out, t.Language(m, r))
		}
		if messageReads[r.PathValue("read")] {
			out = t.TranslateMessages(out, t.Language(m, r))
		}
		WriteJSON(w, http.StatusOK, out)
	})
	public(Route{Pattern: "GET /v1/sign-in", Summary: "How the workspace signs in: the OpenID issuer and client, or development identities", Answer: SignIn{}}, func(w http.ResponseWriter, _ *http.Request) {
		out := SignIn{}
		if h.Issuer != "" {
			out.Issuer, out.Client = h.Issuer, h.Client
		} else if h.Development {
			out.Identities = []Identity{}
			for _, t := range h.tenants {
				if d := h.consoles[t]; d != nil {
					out.Identities = append(out.Identities, d.Identities()...)
				}
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
	h.routes = append(h.routes, namedReads...) // served by GET /v1/{read}
	handle(Route{Pattern: "GET /v1/openapi.json", Summary: "This contract: the host's routes, and the entity types and action payloads the caller sees (ADR-0023)"}, func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, h.OpenAPI(t, &m))
	})
	// The process is alive and holds its tenants (ADR-0027 D6); a tenant's own
	// health is the administrators' read /v1/health.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "tenants": len(h.tenants)})
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
	pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    http.StatusUnprocessableEntity,
}

// Reply writes {"record": …}, {"ok": true} or {"error": {"code": …}}; clients map
// the code to outbox events (K5 A8), never the HTTP status.
func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error) {
	switch {
	case err != nil:
		WriteJSON(w, statuses[err.Code], map[string]any{"error": map[string]string{"code": err.Error()}})
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
