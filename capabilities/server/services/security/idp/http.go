package idp

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func remoteIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request) (*T, bool) {
	var value *T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if dec.Decode(&value) != nil || value == nil || dec.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return nil, false
	}
	return value, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeResult(w http.ResponseWriter, ans any, retry time.Duration, refusal *platform.Error, translate func(string) string) {
	say := func(msg string) string {
		if translate != nil && msg != "" {
			return translate(msg)
		}
		return msg
	}
	if retry > 0 && refusal != nil {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()+0.9))))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": protocol.ErrorBody{Code: "ERROR_CODE_RATE_LIMITED", Message: say(refusal.Message)}})
		return
	}
	if refusal != nil {
		status := http.StatusBadRequest
		if refusal.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
			status = http.StatusConflict
		} else if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": protocol.ErrorBody{Code: refusal.Error(), Message: say(refusal.Message)}})
		return
	}
	writeJSON(w, http.StatusOK, ans)
}

func (a *Accounts) ServeCodeRequest(w http.ResponseWriter, r *http.Request, enabled bool, now time.Time, deliver func(to, subject, text, id string) error, translate func(string) string) {
	if !enabled || a == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, ok := decodeBody[protocol.EmailCodeRequest](w, r)
	if !ok {
		return
	}
	ans, retry, refusal := a.RequestCode(body.Email, remoteIP(r), now, deliver)
	writeResult(w, ans, retry, refusal, translate)
}

func (a *Accounts) ServeCodeVerify(w http.ResponseWriter, r *http.Request, enabled bool, now time.Time, workspaces func(string) []protocol.AuthWorkspace, translate func(string) string) {
	if !enabled || a == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, ok := decodeBody[protocol.EmailVerifyRequest](w, r)
	if !ok {
		return
	}
	ans, retry, refusal := a.VerifyCode(body.Email, body.Code, remoteIP(r), now, workspaces)
	writeResult(w, ans, retry, refusal, translate)
}

func ServeAuthSession(w http.ResponseWriter, r *http.Request, accounts *Accounts, authenticate Authenticate, workspaces func(string) []protocol.AuthWorkspace) {
	subject, ok := authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if !ok || subject == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if accounts != nil {
		if session, found := accounts.SessionOf(subject, workspaces); found {
			writeJSON(w, http.StatusOK, session)
			return
		}
	}
	email := strings.TrimPrefix(subject, "user:")
	writeJSON(w, http.StatusOK, protocol.AuthSession{AccountID: subject, Email: email, Subject: subject, Tenants: workspaces(subject)})
}

func (a *Accounts) ServeDevMailbox(w http.ResponseWriter, r *http.Request) {
	if a == nil || a.opts.Captured == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	ip := remoteIP(r)
	if (ip != "127.0.0.1" && ip != "::1" && ip != "localhost") || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Real-IP") != "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	raw, _ := json.Marshal(a.opts.Captured.Messages(r.URL.Query().Get("to")))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = bytes.NewReader(raw).WriteTo(w)
}
