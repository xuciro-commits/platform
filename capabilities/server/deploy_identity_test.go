package platformserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdentityProxyKeepsSameOriginProtocolAndPlatformRoutes(t *testing.T) {
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.RequestURI() != "/auth/v1/oidc/token?state=kept" || string(body) != "code=original" || r.Host != "localhost:8495" || r.Header.Get("Cookie") != "session=original" || r.Header.Get("X-Forwarded-Host") != "localhost:8495" || strings.Contains(r.Header.Get("X-Forwarded-For"), "untrusted") {
			t.Errorf("identity protocol changed: %s %s %s", r.Method, r.URL.RequestURI(), r.Host)
		}
		w.Header().Set("Location", "http://localhost:8495/")
		w.Header().Set("Set-Cookie", "session=next; Path=/auth; HttpOnly")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("provider result"))
	}))
	defer provider.Close()
	handler, err := identityProxyHandler(provider.URL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("platform:" + r.URL.Path))
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "http://localhost:8495/auth/v1/oidc/token?state=kept", strings.NewReader("code=original"))
	request.Header.Set("Cookie", "session=original")
	request.Header.Set("X-Forwarded-For", "untrusted")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Body.String() != "provider result" || response.Header().Get("Location") != "http://localhost:8495/" || response.Header().Get("Set-Cookie") != "session=next; Path=/auth; HttpOnly" {
		t.Fatal("provider response changed")
	}
	for _, path := range []string{"/", "/v1/me", "/authentication", "/healthz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost:8495"+path, nil))
		if response.Body.String() != "platform:"+path {
			t.Fatal("identity proxy captured a platform route", path)
		}
	}
	if calls != 1 {
		t.Fatal("unexpected identity calls", calls)
	}
}

func TestIdentityProxyRejectsUnboundedUpstreams(t *testing.T) {
	for _, address := range []string{"//provider", "file:///etc/passwd", "http://user:secret@provider", "https://provider/auth", "https://provider?target=other", "https://provider#other"} {
		if _, err := identityUpstream(address); err == nil {
			t.Fatal("invalid identity upstream accepted", address)
		}
	}
	if err := (&Deployment{IdentityProxy: "http://provider"}).validate(); err == nil {
		t.Fatal("identity proxy enabled without an issuer")
	}
}
