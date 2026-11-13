package host

import transport "platformserver/kernel/transport"

import (
	"bytes"
	"net/http"
	"platformkernel/kernel"
	"time"

	"platformserver/platform"
)

type routes struct {
	h   *Host
	mux *http.ServeMux
}

func (rt *routes) public(route transport.Route, f http.HandlerFunc) {
	route.Public = true
	rt.h.routes = append(rt.h.routes, route)
	rt.mux.HandleFunc(route.Pattern, f)
}

func (rt *routes) handle(route transport.Route, f func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant)) {
	h := rt.h
	h.routes = append(h.routes, route)
	rt.mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		m, t, ok := h.member(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		correlation := transport.NewRequestCorrelation(r)
		r = transport.WithRequestCorrelation(r, correlation)
		tracked := &transport.StatusWriter{ResponseWriter: w}
		w = tracked
		w.Header().Set("X-Request-Id", correlation.RequestID)
		defer func() {
			h.observeRequest(t, m, correlation, route.Pattern, tracked.StatusCode(), time.Since(started))
		}()
		if t.quarantined() && (r.Method != http.MethodGet || r.URL.Path != "/v1/health" && r.Pattern != "GET /v1/applications/{app}/{name}/runtime") &&
			(r.Method != http.MethodPost || r.URL.Path != "/v1/recovery/retry") {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": map[string]string{"code": "TENANT_QUARANTINED"}})
			return
		}
		if t.console.Suspended() && (r.Method != http.MethodGet || r.URL.Path != "/v1/health") {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": map[string]string{"code": "TENANT_SUSPENDED"}})
			return
		}
		f(w, r, m, t)
	})
}

func (rt *routes) authorized(route transport.Route, app string, roles []string, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.handle(route, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		kind := "action"
		if r.Method == http.MethodGet {
			kind = "read"
		}
		authorize := func(t *Tenant) (platform.AuthzDecision, *kernel.Error) {
			c := platform.NewCaller(runtime{t: t}, m, app, false, false)
			return t.authorizeNative(c, route.Pattern, kind, roles, true), nil
		}
		var decision platform.AuthzDecision
		var err *kernel.Error
		if t.quarantined() {
			decision, err = authorize(t)
		} else {
			decision, err = recordRead(t, r.Context(), authorize)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if refusal := authorizationRefusal(decision); refusal != nil {
			Reply(w, nil, t.i18n.Said(refusal, t.Language(m, r)))
			return
		}
		f(w, r, m, t)
	})
}

func (rt *routes) recordRead(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.handle(route, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if t.quarantined() && r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
			buffered := &laneBuffer{target: w}
			func() { t.Lane.Lock(); defer t.Lane.Unlock(); f(buffered, r, m, t) }()
			buffered.release()
			return
		}
		buffered := &laneBuffer{target: w}
		_, err := coordinatedRecordRead(t, transport.WithLaneHolder(r.Context(), route.Pattern), func(t *Tenant) (struct{}, *kernel.Error) {
			f(buffered, r, m, t)
			return struct{}{}, nil
		})
		if err != nil {
			Reply(w, nil, err)
			return
		}
		buffered.release()
	})
}

type laneBuffer struct {
	target http.ResponseWriter
	status int
	body   bytes.Buffer
	after  func(http.ResponseWriter)
}

func (b *laneBuffer) Header() http.Header { return b.target.Header() }

func (b *laneBuffer) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *laneBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *laneBuffer) release() {
	if b.after != nil && b.status == 0 && b.body.Len() == 0 {
		b.after(b.target)
		return
	}
	if b.status != 0 {
		b.target.WriteHeader(b.status)
	}
	_, _ = b.target.Write(b.body.Bytes())
}

func outsideLane(w http.ResponseWriter, f func(http.ResponseWriter)) {
	if b, ok := w.(*laneBuffer); ok {
		b.after = f
		return
	}
	f(w)
}
func (rt *routes) metadata(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.recordRead(route, f)
}
