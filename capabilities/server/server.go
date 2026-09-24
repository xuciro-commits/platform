// Package platformserver is the capability every domain server shares (layer 2):
// callers resolved from bearer credentials, the kernel's submission and
// declaration endpoints, and one mapping from contract errors to HTTP.
// Domains add their own reads and connector endpoints on the same mux.
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
)

// Principal is a domain's authenticated caller; the platform needs its tenant and ID (K6).
type Principal interface {
	TenantID() string
	PrincipalID() string
}

// Authenticate turns a bearer credential into a principal; false rejects the request.
type Authenticate[P Principal] func(credential string) (P, bool)

// Server routes requests to the caller's tenant, of type T.
type Server[P Principal, T any] struct {
	Mux          *http.ServeMux
	Tenants      map[string]T
	Authenticate Authenticate[P]
	Now          func() time.Time
}

func New[P Principal, T any](tenants map[string]T, authenticate Authenticate[P]) *Server[P, T] {
	return &Server[P, T]{Mux: http.NewServeMux(), Tenants: tenants, Authenticate: authenticate, Now: Now}
}

// Now is the server clock at the journal's precision (PostgreSQL keeps
// microseconds), so replayed records carry the times first recorded.
func Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// Tokens authenticates with a fixed token table (development and tests).
func Tokens[P Principal](table map[string]P) Authenticate[P] {
	return func(token string) (P, bool) { p, ok := table[token]; return p, ok }
}

// Handle serves pattern for authenticated callers of a known tenant.
func (s *Server[P, T]) Handle(pattern string, h func(w http.ResponseWriter, r *http.Request, who P, tenant T)) {
	s.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		who, ok := s.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		tenant, known := s.Tenants[who.TenantID()]
		if !known {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h(w, r, who, tenant)
	})
}

// Read serves a JSON read model at GET path.
func (s *Server[P, T]) Read(path string, get func(who P, tenant T) any) {
	s.Handle("GET "+path, func(w http.ResponseWriter, _ *http.Request, who P, tenant T) {
		WriteJSON(w, http.StatusOK, get(who, tenant))
	})
}

// Kernel serves POST /v1/submissions, GET /v1/declarations (K5 A9) and GET /v1/me.
func (s *Server[P, T]) Kernel(submit func(who P, tenant T, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error),
	declarations func(tenant T) []*pb.AuthorityDeclaration) {
	s.Handle("POST /v1/submissions", func(w http.ResponseWriter, r *http.Request, who P, tenant T) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		record, err := submit(who, tenant, sub, s.Now())
		Reply(w, record, err)
	})
	s.Handle("GET /v1/declarations", func(w http.ResponseWriter, _ *http.Request, _ P, tenant T) {
		out := []json.RawMessage{}
		for _, d := range declarations(tenant) {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.Read("/v1/me", func(who P, _ T) any {
		return map[string]any{"tenantId": who.TenantID(), "principalId": who.PrincipalID(), "profile": who}
	})
}

// Handler adds CORS for browser clients served from another origin.
func (s *Server[P, T]) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.Mux.ServeHTTP(w, r)
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
