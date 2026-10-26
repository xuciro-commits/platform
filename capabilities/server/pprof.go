package platformserver

// Optional process profiling for operators: PLATFORM_PPROF=1 exposes the Go
// runtime's own profiles at /debug/pprof/, to loopback callers only. It is off
// unless the environment asks for it, and never reachable from another host.

import (
	"net/http"
	"net/http/pprof"
	"os"
)

// profileRoutes registers the runtime profiles when the environment asks.
func profileRoutes(mux *http.ServeMux) {
	if os.Getenv("PLATFORM_PPROF") != "1" {
		return
	}
	loopback := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if host := remoteOf(r); host != "127.0.0.1" && host != "::1" {
				http.Error(w, "profiles are for the host's operator", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /debug/pprof/", loopback(pprof.Index))
	mux.HandleFunc("GET /debug/pprof/cmdline", loopback(pprof.Cmdline))
	mux.HandleFunc("GET /debug/pprof/profile", loopback(pprof.Profile))
	mux.HandleFunc("GET /debug/pprof/symbol", loopback(pprof.Symbol))
	mux.HandleFunc("GET /debug/pprof/trace", loopback(pprof.Trace))
}
