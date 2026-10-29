package platformserver

import transport "platformserver/kernel/transport"

import (
	"net/http"
	"platformkernel/kernel"

	"platformserver/platform"
)

// routes registers the tenant API on a mux and records each route for the
// OpenAPI document. handle authenticates the caller and refuses quarantined
// or suspended tenants; metadata additionally holds the tenant lock, because
// discovery reads the installed declarations publication replaces under it;
// public routes need no member.
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
		m, t, ok := h.member(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
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

// Restricted HTTP surfaces resolve declared owner grants through the same
// engine as an operation, instead of maintaining another role predicate.
func (rt *routes) authorized(route transport.Route, app string, roles []string, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.handle(route, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		kind := "action"
		if r.Method == http.MethodGet {
			kind = "read"
		}
		authorize := func() (platform.AuthzDecision, *kernel.Error) {
			c := platform.NewCaller(runtime{t}, m, app, false, false)
			return t.authorizeNative(c, route.Pattern, kind, roles, true), nil
		}
		var decision platform.AuthzDecision
		var err *kernel.Error
		if t.quarantined() {
			decision, err = authorize()
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
		// Health remains available while recovery has stopped ordinary data
		// reads. Its original owner reports isolation without loading rows.
		if t.quarantined() && r.Method == http.MethodGet && r.URL.Path == "/v1/health" {
			t.mu.Lock()
			defer t.mu.Unlock()
			f(w, r, m, t)
			return
		}
		_, err := coordinatedRecordRead(t, r.Context(), func() (struct{}, *kernel.Error) { f(w, r, m, t); return struct{}{}, nil })
		if err != nil {
			Reply(w, nil, err)
		}
	})
}
func (rt *routes) metadata(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.recordRead(route, f)
}
