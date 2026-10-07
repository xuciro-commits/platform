package platformserver

import (
	"net/http"

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

func (rt *routes) public(route Route, f http.HandlerFunc) {
	route.Public = true
	rt.h.routes = append(rt.h.routes, route)
	rt.mux.HandleFunc(route.Pattern, f)
}

func (rt *routes) handle(route Route, f func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant)) {
	h := rt.h
	h.routes = append(h.routes, route)
	rt.mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) {
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

func (rt *routes) metadata(route Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
	rt.handle(route, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.mu.Lock()
		defer t.mu.Unlock()
		f(w, r, m, t)
	})
}
