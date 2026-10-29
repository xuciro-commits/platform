package composition

import (
	"net/http/httptest"
	"testing"

	"platformserver"
	"platformserver/platform"
)

func TestBuiltinsExposeOneAcyclicComposition(t *testing.T) {
	tenant, err := NewTenant("builtins", platform.Seat{Subjects: []string{"ops"}, Member: platform.Member{ID: "ops", Roles: map[string]string{"platform": "admin"}}})
	if err != nil {
		t.Fatal(err)
	}
	host := platformserver.NewHost(func(token string) (string, bool) { return token, token != "" }, tenant)
	r := httptest.NewRequest("GET", "/v1/composition", nil)
	r.Header.Set("Authorization", "Bearer ops")
	w := httptest.NewRecorder()
	host.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
