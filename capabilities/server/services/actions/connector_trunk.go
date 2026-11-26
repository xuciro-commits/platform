package actions

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

var connectorTrunkOrigin string
var connectorTrunkClient *http.Client

func ConfigureConnectorTrunk(origin, certFile string) error {
	if origin == "" && certFile == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" || !net.ParseIP(u.Hostname()).IsLoopback() || origin != "https://"+u.Host || certFile == "" {
		return fmt.Errorf("connector trunk requires one exact HTTPS loopback origin and certificate")
	}
	pem, err := os.ReadFile(certFile)
	if err != nil {
		return fmt.Errorf("connector trunk certificate is unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return fmt.Errorf("connector trunk certificate is invalid")
	}
	connectorTrunkOrigin = origin
	connectorTrunkClient = &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return nil
}

func GuardedHTTPClient(req *http.Request, allowPrivate bool) *http.Client {
	if connectorTrunkClient != nil && req.URL.User == nil && req.URL.Scheme+"://"+req.URL.Host == connectorTrunkOrigin {
		return connectorTrunkClient
	}
	dialer := GuardedDialer(allowPrivate)
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
