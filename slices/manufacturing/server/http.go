package mes

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

// Server exposes plants over HTTP. Bearer tokens stand in for authentication.
// F-18: this adapter repeats the Hotel slice's (bearer principals, submissions,
// error-to-status mapping).
type Server struct {
	Plants map[string]*Plant    // tenant → plant
	Tokens map[string]Principal // bearer token → principal
	Now    func() time.Time
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, h func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant)) {
		mux.HandleFunc(pattern, s.withPrincipal(h))
	}
	route("POST /v1/submissions", func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			reply(w, nil, invalid)
			return
		}
		record, err := p.Submit(who, sub, s.Now())
		reply(w, record, err)
	})
	route("POST /v1/connectors/states", func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant) {
		var b StateBatch
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			reply(w, nil, invalid)
			return
		}
		_, err := p.DeliverStates(who, b, s.Now())
		reply(w, nil, err)
	})
	route("POST /v1/connectors/planned-orders", func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant) {
		var page PlannedPage
		if json.NewDecoder(r.Body).Decode(&page) != nil {
			reply(w, nil, invalid)
			return
		}
		reply(w, nil, p.DeliverPlanned(who, page, s.Now()))
	})
	route("POST /v1/connectors/heartbeat", func(w http.ResponseWriter, _ *http.Request, who Principal, p *Plant) {
		reply(w, nil, p.Heartbeat(who, s.Now()))
	})
	read := func(path string, get func(p *Plant) any) {
		route("GET "+path, func(w http.ResponseWriter, _ *http.Request, _ Principal, p *Plant) {
			writeJSON(w, http.StatusOK, get(p))
		})
	}
	read("/v1/master", func(p *Plant) any { return p.Master() })
	read("/v1/orders", func(p *Plant) any { return p.Orders() })
	read("/v1/sfcs", func(p *Plant) any { return p.SFCs() })
	read("/v1/planned-orders", func(p *Plant) any { return p.Planned() })
	read("/v1/downtime", func(p *Plant) any { return p.Downtime() })
	read("/v1/connectors", func(p *Plant) any { return p.Connectors(s.Now()) })
	route("GET /v1/declarations", func(w http.ResponseWriter, _ *http.Request, _ Principal, p *Plant) {
		var out []json.RawMessage
		for _, d := range p.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		writeJSON(w, http.StatusOK, out)
	})
	route("GET /v1/me", func(w http.ResponseWriter, _ *http.Request, who Principal, _ *Plant) {
		writeJSON(w, http.StatusOK, who)
	})
	return cors(mux)
}

// cors lets the browser client on another port call the server (development setup).
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withPrincipal(next func(http.ResponseWriter, *http.Request, Principal, *Plant)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who, ok := s.Tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		p := s.Plants[who.Tenant]
		if !ok || p == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r, who, p)
	}
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

func reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error) {
	if err != nil {
		writeJSON(w, statuses[err.Code], map[string]any{"error": map[string]string{"code": err.Error()}})
		return
	}
	if record == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	raw, _ := protojson.Marshal(record)
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{"record": raw})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
