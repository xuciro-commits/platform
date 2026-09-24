// Package platformserver is the platform host (layer 2, ADR-0010): it runs a
// tenant's apps behind one HTTP surface — authentication, the directory, routing
// by action, read and input name, the per-caller catalog, one journal — and maps
// contract errors to HTTP once.
package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
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

func (h *Host) member(r *http.Request) (platform.Member, *Tenant, bool) {
	subject, ok := h.authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if !ok {
		return platform.Member{}, nil, false
	}
	for _, t := range h.tenants {
		if d := h.consoles[t]; d == nil {
			continue
		} else if m, ok := d.Member(subject); ok {
			return m, t, true
		}
	}
	return platform.Member{}, nil, false
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
	handle("GET /v1/me", func(w http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]any{"tenantId": m.Tenant, "principalId": m.ID, "profile": m})
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
	handle("POST /mcp", h.mcp)
	handle("GET /v1/{read}", func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.Read(m, r.PathValue("read"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
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
