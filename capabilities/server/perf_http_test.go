package platformserver_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"platformserver"
	"platformserver/composition"
	"platformserver/platform"
)

func benchServer(b *testing.B) (*httptest.Server, string) {
	b.Helper()
	seats := []platform.Seat{{Subjects: []string{"dev:bench"}, Member: platform.Member{ID: "builder", Roles: map[string]string{
		"platform": "admin", "build": "builder", "core": "steward", "ai": "admin", "work": "admin", "agent": "admin",
	}}}}
	tenant, err := composition.NewTenant("bench", seats...)
	if err != nil {
		b.Fatal(err)
	}
	host := platformserver.NewHost(platformserver.Tokens(map[string]string{"dev-token": "dev:bench"}), tenant)
	host.Now = platformserver.Now
	return httptest.NewServer(host.Handler()), "dev-token"
}

func get(b *testing.B, server *httptest.Server, token, path string) {
	req, _ := http.NewRequest("GET", server.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Platform-Tenant", "bench")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.Error(err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func BenchmarkHTTPMe(b *testing.B) {
	server, token := benchServer(b)
	defer server.Close()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			get(b, server, token, "/v1/me")
		}
	})
}

func BenchmarkHTTPEntities(b *testing.B) {
	server, token := benchServer(b)
	defer server.Close()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			get(b, server, token, "/v1/entities")
		}
	})
}
