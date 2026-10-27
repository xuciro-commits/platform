package platformserver

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func identityUpstream(address string) (*url.URL, error) {
	target, err := url.Parse(address)
	if err != nil || target.Host == "" || target.Scheme != "http" && target.Scheme != "https" || target.User != nil || target.Path != "" && target.Path != "/" || target.RawQuery != "" || target.Fragment != "" {
		return nil, fmt.Errorf("-oidc-proxy requires a fixed HTTP(S) service origin without credentials, path, query or fragment")
	}
	return target, nil
}

// The identity provider keeps its own protocol and authentication. Deployment
// only gives its existing /auth endpoints the platform's public origin.
func identityProxyHandler(address string, next http.Handler) (http.Handler, error) {
	if address == "" {
		return next, nil
	}
	target, err := identityUpstream(address)
	if err != nil {
		return nil, err
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = request.In.Host
			request.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Identity service unavailable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth" || strings.HasPrefix(r.URL.Path, "/auth/") {
			proxy.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
