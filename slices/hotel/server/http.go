package hotel

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

// Server exposes tenants over HTTP. Bearer tokens stand in for authentication.
type Server struct {
	Hotels        map[string]*Hotel    // tenant → hotel
	Tokens        map[string]Principal // bearer token → principal
	ResponseDelay time.Duration        // simulates a slow network after processing
	DelayCount    int                  // delay only this many responses (0: all)
	delayed       int
	Now           func() time.Time
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/submissions", s.withPrincipal(func(w http.ResponseWriter, r *http.Request, p Principal, h *Hotel) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			s.reply(w, nil, fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT))
			return
		}
		record, err := h.Submit(p, sub, s.Now())
		s.reply(w, record, err)
	}))
	mux.HandleFunc("POST /v1/channel/bookings", s.withPrincipal(func(w http.ResponseWriter, r *http.Request, p Principal, h *Hotel) {
		var b ChannelBooking
		if p.Role != Channel || json.NewDecoder(r.Body).Decode(&b) != nil {
			s.reply(w, nil, denied())
			return
		}
		record, err := h.IngestChannelBooking(p, b, s.Now())
		s.reply(w, record, err)
	}))
	mux.HandleFunc("GET /v1/me", s.withPrincipal(func(w http.ResponseWriter, _ *http.Request, p Principal, _ *Hotel) {
		json.NewEncoder(w).Encode(map[string]string{"principalId": p.ID, "tenantId": p.Tenant, "role": string(p.Role)})
	}))
	mux.HandleFunc("GET /v1/reservations", s.withPrincipal(func(w http.ResponseWriter, _ *http.Request, _ Principal, h *Hotel) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(h.Reservations())
	}))
	return mux
}

func (s *Server) withPrincipal(next func(http.ResponseWriter, *http.Request, Principal, *Hotel)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		h := s.Hotels[p.Tenant]
		if !ok || h == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r, p, h)
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

func (s *Server) reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error) {
	if s.DelayCount == 0 || s.delayed < s.DelayCount {
		s.delayed++
		time.Sleep(s.ResponseDelay)
	}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(statuses[err.Code])
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": err.Error()}})
		return
	}
	json.NewEncoder(w).Encode(map[string]json.RawMessage{"record": RecordJSON(record)})
}
