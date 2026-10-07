// Package platformserver is the platform host (layer 2, ADR-0010): it runs a
// tenant's apps behind one HTTP surface — authentication, the directory, routing
// by action, read and input name, the per-caller catalog, one journal — and maps
// contract errors to HTTP once.
package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"platformserver/idp"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/build"
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
			if t.quarantined() && (r.Method != http.MethodGet || r.URL.Path != "/v1/health") &&
				(r.Method != http.MethodPost || r.URL.Path != "/v1/recovery/retry") {
				WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error": map[string]string{"code": "TENANT_QUARANTINED"}})
				return
			}
			if t.console.suspended() && (r.Method != http.MethodGet || r.URL.Path != "/v1/health") {
				WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error": map[string]string{"code": "TENANT_SUSPENDED"}})
				return
			}
			f(w, r, m, t)
		})
	}
	// Discovery reads the same mutable installed declarations that publication
	// replaces under the tenant lock. Keep the lock at the HTTP boundary:
	// the metadata helpers are also used by already-locked input validation.
	metadata := func(route Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
		handle(route, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			t.mu.Lock()
			defer t.mu.Unlock()
			f(w, r, m, t)
		})
	}
	handle(Route{Pattern: "GET /v1/changes", Summary: "Server-sent events: changed for data changes, operations for queue-only progress (F-32)", Answer: LiveQueryFrame{}},
		func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) {
			if r.URL.Query().Has("watch") {
				followLiveQueries(w, r, t, mux)
				return
			}
			followChanges(w, r, t)
		})
	handle(Route{Pattern: "POST /v1/recovery/retry", Summary: "Retry recovery of this quarantined tenant from the durable journal after an operator repairs its cause (ADR-0038)",
		Answer: TenantHealth{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[PlatformApp] != Admin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if h.Recover == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if err := h.Recover(r.Context(), t.ID); err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "health": t.Health(h.Now())})
			return
		}
		for _, current := range h.currentTenants() {
			if current.ID == t.ID {
				WriteJSON(w, http.StatusOK, current.Health(h.Now()))
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	handle(Route{Pattern: "POST /v1/composites", Summary: "Edit several of an application's assets as one unit: every edit is probed, and either all apply or none (ADR-0047 §11)", Body: struct {
		Key   string          `json:"key"`
		Edits []CompositeEdit `json:"edits"`
	}{}, Answer: CompositeAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var request struct {
			Key   string          `json:"key"`
			Edits []CompositeEdit `json:"edits"`
		}
		if json.Unmarshal(body, &request) != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "a key and its edits are required"})
			return
		}
		answer, err := t.Composite(m, request.Key, request.Edits, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "POST /v1/submissions", Summary: "Submit a decision: an action on a target, received in the kernel's order (K6) and journaled once accepted", Body: pb.Submission{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		record, err := t.Submit(m, sub, h.Now())
		Reply(w, record, t.i18n.said(err, t.Language(m, r)))
	})
	handle(Route{Pattern: "POST /v1/connectors/{input}", Summary: "Deliver a connector's batch or page as the connector's member (K8)", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		out, err := t.Input(m, r.PathValue("input"), body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, err)
	})
	handle(Route{Pattern: "POST /v1/files", Summary: "Upload a file's bytes (the body; query name; Content-Type); answers its SHA-256 to attach with files.file.attach (ADR-0028)",
		Query: []Param{{"name", "The file's name"}}, Body: []byte{}, Answer: Upload{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		up, status, err := t.Upload(m, r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.Body, h.Now())
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		WriteJSON(w, http.StatusOK, up)
	})
	handle(Route{Pattern: "GET /v1/files/{id}", Summary: "Download a file attached to a record the caller may read (ADR-0028)"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.Download(w, m, r.PathValue("id"), h.Now())
	})
	metadata(Route{Pattern: "GET /v1/me", Summary: "Who the caller is on this host: tenant, member, the apps they may open, their language", Answer: MeView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		lang := t.Language(m, r)
		view := MeView{TenantID: m.Tenant, PrincipalID: m.ID, Profile: m, Apps: t.AppsOf(m), Tenants: h.tenantsOf(r),
			Language: lang, Languages: t.i18n.languages(), Preferred: m.Language, Currency: t.setting(t.automation(PlatformApp, false), SettingCurrency)}
		if d, ok := t.app(PlatformApp).(*Console); ok {
			view.Account, view.Tenant = d.Account(m.ID), d.tenantRecord()
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(view, lang))
	})
	metadata(Route{Pattern: "GET /v1/declarations", Summary: "The data classes and their authorities the tenant's apps declare (K5)", Answer: []*pb.AuthorityDeclaration{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		out := []json.RawMessage{}
		for _, d := range t.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/chain/{type}/{id}", Summary: "The chain an agent run or a flow instance belongs to: its record, flows, runs and the effects they caused (ADR-0029)", Answer: Chain{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		chain, err := t.ChainOf(m, r.PathValue("type")+"/"+r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, chain)
	})
	handle(Route{Pattern: "GET /v1/authz/explain", Summary: "Why a member may or may not exercise a permission: their roles, the roles it names, the engine's verdict (administrators, auditors; ADR-0078)", Answer: Explanation{},
		Query: []Param{{"member", "the member to ask about"}, {"permission", "an action's schema, or <app>:read:<name>"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if !m.Holds(PlatformApp, Admin) && !m.Holds(PlatformApp, Auditor) {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED})
			return
		}
		out, err := t.Explain(r.URL.Query().Get("member"), r.URL.Query().Get("permission"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/sessions", Summary: "The caller's sessions: the credentials the host has seen act as them, the current one marked (ADR-0079 §5)", Answer: []Session{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Sessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	})
	handle(Route{Pattern: "POST /v1/sessions/end-others", Summary: "End every session of the caller but this one: the host refuses those credentials from now on (ADR-0079 §5)", Answer: map[string]int{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]int{"ended": consoleOf(t).EndOtherSessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))})
	})
	handle(Route{Pattern: "GET /v1/tokens", Summary: "The caller's personal tokens, never their secrets (ADR-0079 §5)", Answer: []TokenView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Tokens(m.ID, h.Now()))
	})
	handle(Route{Pattern: "GET /v1/tokens/{id}/secret", Summary: "The secret of a token the caller just issued, once (ADR-0079 §5)", Answer: map[string]string{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		secret, ok := consoleOf(t).Minted(r.PathValue("id"), m.ID, time.Now())
		if !ok {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"secret": secret})
	})
	metadata(Route{Pattern: "GET /v1/actions", Summary: "The caller's catalog: the actions their roles permit, in their language (ADR-0008)", Answer: []platform.Action{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Catalog(m), t.Language(m, r)))
	})
	metadata(Route{Pattern: "GET /v1/apps", Summary: "The tenant's apps from their manifests", Answer: []AppInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Apps(), t.Language(m, r)))
	})
	metadata(Route{Pattern: "GET /v1/protocols", Summary: "The protocols apps provide and consume, and the provider bound to each (ADR-0011)", Answer: []ProtocolInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Protocols(), t.Language(m, r)))
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
		// A chat body carries a conversation and its tool declarations; the
		// platform bounds it like every other request body (review AI-03).
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, chatMaxBytes)).Decode(&req) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		if req.Stream { // server-sent events: delta, then done or error (ADR-0029 D2)
			flusher, _ := w.(http.Flusher)
			started := false
			event := func(name string, v any) {
				if !started {
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("Cache-Control", "no-cache")
					w.WriteHeader(http.StatusOK)
					started = true
				}
				raw, _ := json.Marshal(v)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
				if flusher != nil {
					flusher.Flush()
				}
			}
			answer, err, failure := t.Chat(m, req, h.Now(), func(piece string) { event("delta", map[string]string{"content": piece}) })
			switch {
			case err != nil && !started:
				Reply(w, nil, err)
			case failure != nil && failure.Quota && !started:
				WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
			case failure != nil:
				event("error", map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail, "usage": answer.Usage})
			default:
				event("done", map[string]any{"usage": answer.Usage})
			}
			return
		}
		answer, err, failure := t.Chat(m, req, h.Now())
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil && failure.Quota:
			WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
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
	metadata(Route{Pattern: "GET /v1/entities", Summary: "The entity types of the apps the caller holds a role in, with their meaning, in their language (ADR-0016, ADR-0023)", Answer: []platform.EntityInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Entities(m), t.Language(m, r)))
	})
	handle(Route{Pattern: "GET /v1/applications/{app}/{name}/runs", Summary: "Authorized runs related to current or retained application resources; shared use does not imply exclusive application origin", Answer: ApplicationRunPage{}, Query: []Param{{"offset", "Nonnegative run offset"}, {"limit", "1–100, default 50"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 50)
		answer, err := t.ApplicationRuns(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetApp, Name: r.PathValue("name")}, offset, limit, time.Now().UTC())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	metadata(Route{Pattern: "GET /v1/definitions", Summary: "Installed object, action and page definitions the caller may discover, with qualified references and dependencies (ADR-0032)", Answer: []platform.Definition{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Definitions(m), t.Language(m, r)))
	})
	metadata(Route{Pattern: "GET /v1/pages/{app}/{name}/{contentVersion}", Summary: "Read exact published page content through current member discovery permissions (ADR-0046)", Answer: platform.Definition{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.PageContentDefinition(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetPage, Name: r.PathValue("name")}, r.PathValue("contentVersion"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	metadata(Route{Pattern: "GET /v1/capabilities", Summary: "Typed Block projections of the caller's installed owner capabilities (ADR-0044)", Answer: []platform.CapabilityDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Capabilities(m), t.Language(m, r)))
	})
	metadata(Route{Pattern: "GET /v1/capabilities/{app}/{kind}/{name}", Summary: "Read a callable owner's exact retained input/output schema", Query: []Param{{"version", "Retained query/compute/AI ordinal; zero selects code declarations or the installed compute/AI version"}}, Answer: platform.CapabilityDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		version := 0
		if text := r.URL.Query().Get("version"); text != "" {
			var err error
			version, err = strconv.Atoi(text)
			if err != nil || version < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		answer, err := t.DescribeCapability(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetKind(r.PathValue("kind")), Name: r.PathValue("name")}, version)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	handle(Route{Pattern: "POST /v1/capabilities/invoke", Summary: "Route a typed call to its canonical query, action, AI or compute owner", Body: platform.CapabilityInvocation{}, Answer: platform.CapabilityResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q platform.CapabilityInvocation
		if !readCapabilityBody(w, r, &q) {
			return
		}
		answer, err := t.InvokeCapability(m, q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "GET /v1/capabilities/calls/compute/{id}", Summary: "Read a retained compute result after rechecking member and protected sources", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadOperation(m, r.PathValue("id"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "GET /v1/capabilities/calls/ai/{id}", Summary: "Read the original typed AI call result after member and source checks", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadCapabilityAI(m, r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "POST /v1/build/code/sdk", Summary: "Generate the Go/TinyGo input, output and command wrapper from the same bounded schema", Body: ComputeSDKRequest{}, Answer: ComputeSDK{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, _ *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var q ComputeSDKRequest
		if !readCapabilityBody(w, r, &q) {
			return
		}
		source, err := GenerateComputeSDK(q.Input, q.Output)
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error()))
			return
		}
		WriteJSON(w, http.StatusOK, ComputeSDK{Source: string(source)})
	})
	handle(Route{Pattern: "POST /v1/build/process/check", Summary: "Validate a workflow draft with its owner compiler without executing it", Body: build.Process{}, Answer: ProcessDiagnostics{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var p build.Process
		raw, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
		if err != nil || len(raw) > 128<<10 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if _, err = platform.DecodeValue(raw, 128<<10); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		owner, ok := t.app(build.ID).(*build.Build)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
		if refusal := owner.CheckProcess(p); refusal != nil {
			answer.Valid = false
			message := refusal.Message
			node := ""
			parts := strings.SplitN(message, ": ", 2)
			if len(parts) == 2 {
				if strings.HasPrefix(parts[0], "step ") {
					node = strings.TrimPrefix(parts[0], "step ")
				}
				if strings.HasPrefix(parts[0], "Node ") {
					node = strings.TrimPrefix(parts[0], "Node ")
				}
			}
			answer.Issues = append(answer.Issues, ProcessDiagnostic{Node: node, Code: refusal.Code.String(), Message: message})
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "GET /v1/releases/candidates", Summary: "Builder and publisher saved release inventory from committed candidate bytes", Query: []Param{{"offset", "Candidates to skip"}, {"limit", "Candidates in the page, 1 to 100 (default 20)"}}, Answer: ReleasePage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		offset, limit := 0, 20
		for name, target := range map[string]*int{"offset": &offset, "limit": &limit} {
			if raw := r.URL.Query().Get(name); raw != "" {
				value, err := strconv.Atoi(raw)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				*target = value
			}
		}
		if offset < 0 || limit < 1 || limit > 100 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.SavedReleases(m, offset, limit)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "GET /v1/releases/candidates/{id}", Summary: "Builder and publisher review of sealed definitions against actual running definitions", Answer: SavedReleaseReview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		answer, err := t.ReviewSavedRelease(m, r.PathValue("id"))
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	handle(Route{Pattern: "POST /v1/releases/drafts/referenced", Summary: "Builder-only list of the saved record drafts a chosen draft depends on and that are not installed yet (ADR-0048 D1)", Body: ReleaseDraftsRequest{}, Answer: ReleaseDraftClosure{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		if readErr != nil || len(body) > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request ReleaseDraftsRequest
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.ID == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "a draft kind and id are required"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.ReferencedDrafts(m, request.Kind, request.ID)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	// One request carries either a single draft or a joint selection.
	draftsOf := func(request ReleaseSaveRequest) []build.JointDraftRef {
		if len(request.Drafts) > 0 {
			return request.Drafts
		}
		return []build.JointDraftRef{{Kind: request.Kind, ID: request.ID}}
	}
	handle(Route{Pattern: "POST /v1/releases/preview", Summary: "Builder-only read-only comparison of one saved draft with its installed development definition (ADR-0039 20a)", Body: ReleasePreviewRequest{}, Answer: ReleasePreview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		if readErr != nil || len(body) > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request ReleasePreviewRequest
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil ||
			(request.ID == "" && len(request.Drafts) == 0) ||
			(request.ID != "" && len(request.Drafts) > 0) ||
			len(request.Drafts) > 32 {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "one draft kind and id, or up to 32 joint drafts, are required"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.PreviewRelease(m, request.Kind, request.ID)
		if len(request.Drafts) > 0 {
			answer, err = t.PreviewReleaseDrafts(m, request.Drafts)
		}
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	handle(Route{Pattern: "POST /v1/releases/candidates", Summary: "Persist exact, immutable bytes for a builder-reviewed candidate; does not activate it (ADR-0039 20a)", Body: ReleaseSaveRequest{}, Answer: ReleaseSaved{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request ReleaseSaveRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil ||
			(request.ID == "" && len(request.Drafts) == 0) ||
			(request.ID != "" && len(request.Drafts) > 0) || len(request.Drafts) > 32 ||
			request.CandidateID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.SaveReleaseCandidates(m, draftsOf(request), request.CandidateID, request.Key, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, ReleaseSaved{ID: id})
	})
	handle(Route{Pattern: "POST /v1/releases/evaluations", Summary: "Run real measured model calls against synthetic cases for one saved function candidate (ADR-0043 24c)", Body: ReleaseEvaluationRequest{}, Answer: ReleaseEvaluationStarted{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request ReleaseEvaluationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF ||
			request.CandidateID == "" || request.PlanID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.EvaluateRelease(m, request, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, ReleaseEvaluationStarted{ID: id})
	})
	handle(Route{Pattern: "POST /v1/releases/active", Summary: "Builder and publisher atomic installation and activation of a saved object/page/application closure (ADR-0039 20b)", Body: ReleaseActivateRequest{}, Answer: ReleaseActive{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request ReleaseActivateRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.CandidateID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.ActivateReleaseWithUpgrade(m, request.CandidateID, request.Key, request.UpgradeID, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, ReleaseActive{ID: id})
	})
	handle(Route{Pattern: "GET /v1/queries/{app}/{name}", Summary: "Run a declared query as the caller: its conditions, and the record it is run for (ADR-0040 21c)",
		Query: []Param{{"for", "the ID of the record it is run for, when it takes one"}}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		page, err := t.RunQuery(m, r.PathValue("app"), r.PathValue("name"), r.URL.Query().Get("for"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	handle(Route{Pattern: "POST /v1/simulate", Summary: "Builder-only dry run: decide an action as a member in a private staged decision and discard it (ADR-0040 21d)",
		Query: []Param{{"as", "the member it is tried as; empty: the builder"}}, Body: []byte{}, Answer: Simulation{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s := &pb.Submission{}
		if protojson.Unmarshal(body, s) != nil || s.GetSchema() == nil || s.GetTarget() == nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "a submission with a schema and a target is required"})
			return
		}
		out, err := t.Simulate(m, r.URL.Query().Get("as"), s, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "POST /v1/simulate/candidate", Summary: "Test a saved object, process or function draft using fixed actions and answers in an empty isolated tenant (ADR-0040 21d, ADR-0042 23b, ADR-0043 24c)",
		Body: CandidateSimulationRequest{}, Answer: CandidateSimulation{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[build.ID] != build.Builder {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request CandidateSimulationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The test plan must be a valid bounded JSON object"))
			return
		}
		out, err := t.SimulateCandidate(m, request)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/releases/active", Summary: "The tenant's active release ID, readable by any member (ADR-0039 20b)", Answer: ReleaseActive{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, ReleaseActive{ID: t.ActiveRelease()})
	})
	handle(Route{Pattern: "POST /v1/import/{type}", Summary: "Import records from CSV: a header of field names with an id column; each row is the type's generated create or edit as the caller (ADR-0028)",
		Query: []Param{{"preview", "true: check each row and apply none"}}, Body: []byte{}, Answer: []ImportRow{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		rows, err := t.Import(m, r.PathValue("type"), body, r.URL.Query().Get("preview") == "true", h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, rows)
	})
	handle(Route{Pattern: "GET /v1/export/{type}", Summary: "The records a list shows the caller, as CSV (ADR-0028)",
		Query: []Param{{"domain", "Filters in the prefix form"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search")}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		out, err := t.Export(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+r.PathValue("type")+".csv")
		w.Write(out)
	})
	handle(Route{Pattern: "POST /v1/records/{type}/query", Summary: "Read a bounded complete record-set expression in the caller's scope before paging (ADR-0046)", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		var q *platform.Query
		if decoder.Decode(&q) != nil || q == nil || q.Limit < 0 || q.Offset < 0 {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		page, err := t.Records(m, r.PathValue("type"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	handle(Route{Pattern: "POST /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Read a retained relationship using a bounded original query (ADR-0046)", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q *platform.Query
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&q) != nil || q == nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link query must be a bounded JSON object"))
			return
		}
		result, err := t.TraverseLink(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	handle(Route{Pattern: "GET /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Follow one retained link type within the member's original record scope (ADR-0046)", Answer: RecordPage{}, Query: []Param{{"domain", "Additional record filters"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search")}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		result, err := t.TraverseLink(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
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
		tenants := h.currentTenants()
		i := slices.IndexFunc(tenants, func(t *Tenant) bool { return t.ID == r.PathValue("tenant") })
		if i < 0 || !tenants[i].published(r.PathValue("agent")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		WriteJSON(w, http.StatusOK, h.agentCard(r, tenants[i], r.PathValue("agent")))
	})
	public(Route{Pattern: "POST /a2a/{tenant}/{agent}", Summary: "A2A 1.0 JSON-RPC to a published agent; the caller signs in with the host's issuer", Body: json.RawMessage{}}, h.serveA2A)
	handle(Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first (ADR-0022)", Answer: []Passage{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	handle(Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for administrators of the agent or AI app who may also read what the run read (#130)", Answer: []Transcript{}, Query: []Param{{"run", "An agent run"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.TranscriptsFor(m, r.URL.Query().Get("run"), 50, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "POST /v1/aggregates/{type}/query", Summary: "Aggregate authorized membership or explicitly declared recent business-time windows (ADR-0046)", Body: AggregateQuery{}, Answer: Aggregate{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		var q *AggregateQuery
		if decoder.Decode(&q) != nil || q == nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		out, err := t.Aggregate(m, r.PathValue("type"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
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
		if activity, ok := t.i18n.TranslateMessages(view.Activity, t.Language(m, r)).([]any); ok {
			view.Activity = activity
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
			out = t.i18n.Translate(out, t.Language(m, r))
		}
		if messageReads[r.PathValue("read")] {
			out = t.i18n.TranslateMessages(out, t.Language(m, r))
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle(Route{Pattern: "GET /v1/integration-effects", Summary: "Build writeback delivery status for builders and integrators, excluding payloads", Answer: []IntegrationEffect{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.integrationEffects(m, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	public(Route{Pattern: "GET /v1/sign-in", Summary: "How the workspace signs in: the OpenID issuer and client, or development identities", Answer: SignIn{}}, func(w http.ResponseWriter, _ *http.Request) {
		out := SignIn{}
		if h.Issuer != "" {
			out.Issuer, out.Client = h.Issuer, h.Client
		} else if h.Development {
			out.Identities = []Identity{}
			for _, t := range h.currentTenants() {
				if d := consoleOf(t); d != nil {
					out.Identities = append(out.Identities, d.Identities()...)
				}
			}
			if h.Mint != nil {
				for i := range out.Identities { // the lightweight host signs what it serves
					out.Identities[i].Token = h.Mint(out.Identities[i].Token)
				}
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
	h.routes = append(h.routes, namedReads...) // served by GET /v1/{read}
	metadata(Route{Pattern: "GET /v1/openapi.json", Summary: "This contract: the host's routes, and the entity types and action payloads the caller sees (ADR-0023)"}, func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
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
