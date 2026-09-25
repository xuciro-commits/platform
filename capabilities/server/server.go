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
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	handle := func(pattern string, f func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			m, t, ok := h.member(r)
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f(w, r, m, t)
		})
	}
	handle("POST /v1/submissions", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		record, err := t.Submit(m, sub, h.Now())
		Reply(w, record, err)
	})
	handle("POST /v1/connectors/{input}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		out, err := t.Input(m, r.PathValue("input"), body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, err)
	})
	handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]any{"tenantId": m.Tenant, "principalId": m.ID, "profile": m, "apps": t.AppsOf(m), "tenants": h.tenantsOf(r)})
	})
	handle("GET /v1/declarations", func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		out := []json.RawMessage{}
		for _, d := range t.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		WriteJSON(w, http.StatusOK, out)
	})
	handle("GET /v1/actions", func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Catalog(m))
	})
	handle("GET /v1/apps", func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Apps())
	})
	handle("GET /v1/protocols", func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Protocols())
	})
	handle("POST /v1/protocols/{protocol}/{version}/{action}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	handle("POST /v1/ai/chat", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	handle("GET /v1/ai/providers/{id}/models", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	handle("GET /v1/ai/vendors", func(w http.ResponseWriter, _ *http.Request, _ platform.Member, _ *Tenant) {
		WriteJSON(w, http.StatusOK, Vendors)
	})
	handle("GET /v1/entities", func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Entities(m))
	})
	handle("GET /v1/records/{type}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	handle("GET /v1/records/{type}/{id}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.RecordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	handle("POST /mcp", h.mcp)
	handle("GET /v1/{read}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.Read(m, r.PathValue("read"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /v1/sign-in", func(w http.ResponseWriter, _ *http.Request) {
		out := map[string]any{}
		if h.Issuer != "" {
			out["issuer"], out["client"] = h.Issuer, h.Client
		} else if h.Development {
			identities := []Identity{}
			for _, t := range h.tenants {
				if d := h.consoles[t]; d != nil {
					identities = append(identities, d.Identities()...)
				}
			}
			out["identities"] = identities
		}
		WriteJSON(w, http.StatusOK, out)
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
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+TenantHeader)
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
