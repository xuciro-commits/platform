package host

import transport "platformserver/kernel/transport"

import (
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
		if transport.Unavailable(w, r, t.quarantined(), t.console.Suspended()) {
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
			buffered := transport.NewLaneBuffer(w)
			func() { t.Lane.Lock(); defer t.Lane.Unlock(); f(buffered, r, m, t) }()
			buffered.Release()
			return
		}
		buffered := transport.NewLaneBuffer(w)
		_, err := coordinatedRecordRead(t, transport.WithLaneHolder(r.Context(), route.Pattern), func(t *Tenant) (struct{}, *kernel.Error) {
			f(buffered, r, m, t)
			return struct{}{}, nil
		})
		if err != nil {
			Reply(w, nil, err)
			return
		}
		buffered.Release()
	})
}

func (rt *routes) metadata(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.recordRead(route, f)
}
