package idp

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func IdentityUpstream(address string) (*url.URL, error) {
	target, err := url.Parse(address)
	if err != nil || target.Host == "" || target.Scheme != "http" && target.Scheme != "https" || target.User != nil || target.Path != "" && target.Path != "/" || target.RawQuery != "" || target.Fragment != "" {
		return nil, fmt.Errorf("-oidc-proxy requires a fixed HTTP(S) service origin without credentials, path, query or fragment")
	}
	return target, nil
}

func IdentityProxyHandler(address string, next http.Handler) (http.Handler, error) {
	if address == "" {
		return next, nil
	}
	target, err := IdentityUpstream(address)
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

func LoadLocal(path string) (*Local, error) {
	key, err := LoadKey(path)
	if err != nil {
		return nil, err
	}
	return NewLocal(key)
}

func MakeKeyFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("idp: %s already exists; it is not replaced", path)
	}
	key, err := NewKey()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return SaveKey(path, key)
}

type Profile struct {
	Name, Data, KeyFile                  string
	Database, Files, Issuer, Keys, Proxy string
	Project                              bool
}

func (p Profile) Check() (string, error) {
	if p.Proxy != "" {
		if p.Issuer == "" {
			return p.Name, fmt.Errorf("-oidc-proxy requires -oidc-issuer")
		}
		if _, err := IdentityUpstream(p.Proxy); err != nil {
			return p.Name, err
		}
	}
	switch p.Name {
	case "", "delivery":
		if p.Data != "" || p.KeyFile != "" {
			return "delivery", fmt.Errorf("-data and -idp-key belong to -profile lightweight; the delivery profile keeps -database, -files and -oidc-issuer")
		}
		return "delivery", nil
	case "lightweight":
		if p.Data == "" {
			return p.Name, fmt.Errorf("-profile lightweight needs -data <directory>: a lightweight host keeps its journal, file bytes and signing key there")
		}
		for name, set := range map[string]bool{"-database": p.Database != "", "-files": p.Files != "", "-oidc-issuer": p.Issuer != "",
			"-oidc-keys": p.Keys != "", "-oidc-proxy": p.Proxy != "", "-project": p.Project} {
			if set {
				return p.Name, fmt.Errorf("%s is not part of the lightweight profile: its state is the -data directory", name)
			}
		}
		return p.Name, nil
	}
	return p.Name, fmt.Errorf("unknown -profile %q: delivery, lightweight", p.Name)
}

func Select(issuer, keys string) (Authenticate, Attest, error) {
	if issuer == "" {
		return func(token string) (string, bool) { return token, token != "" }, nil, nil
	}
	if keys == "" {
		return nil, nil, fmt.Errorf("-oidc-keys is required with -oidc-issuer")
	}
	authenticate, attest := OIDCProvider(issuer, keys)
	return authenticate, attest, nil
}
