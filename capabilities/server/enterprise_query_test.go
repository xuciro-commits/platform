package platformserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// TestEnterpriseQueryHTTP walks the typed reads over the wire (ADR-0094): the
// routes answer, the filters reach the model, and a refused argument is a 400 —
// the contract the generated SDK calls.
func TestEnterpriseQueryHTTP(t *testing.T) {
	seat := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin, enterprise.ID: enterprise.Admin}}}
	tn, err := NewTenant("enterprise-query", NewConsole("enterprise-query", seat), enterprise.New("enterprise-query", platform.OrgSeed{Units: []platform.Unit{
		{ID: "plant-1", Name: "Main plant", Kind: "plant", From: "2026-01-01"},
		{ID: "line-1", Name: "Line 1", Kind: "line", From: "2026-01-01"},
		{ID: "line-old", Name: "Retired line", Kind: "line", From: "2026-01-01", Until: "2026-06-01", Closed: "merged"},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHost(Tokens(map[string]string{"token": "user:admin@example.test"}), tn).Handler())
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	get := func(path string, want int) enterprise.EnterpriseQueryResult {
		t.Helper()
		request, _ := http.NewRequest("GET", server.URL+path, nil)
		request.Header.Set("Authorization", "Bearer token")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s: HTTP %d, want %d", path, response.StatusCode, want)
		}
		var out enterprise.EnterpriseQueryResult
		if want == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	ids := func(r enterprise.EnterpriseQueryResult) (first, second string) {
		if len(r.Elements) > 0 {
			first = r.Elements[0].ID
		}
		if len(r.Elements) > 1 {
			second = r.Elements[1].ID
		}
		return
	}
	// Today's live organisations (units project to ActualOrganization), by id.
	if a, b := ids(get("/v1/enterprise-query", http.StatusOK)); a != "line-1" || b != "plant-1" {
		t.Errorf("query = %q, %q", a, b)
	}
	// Filters reach the model: exact kind, case-insensitive words, stereotype.
	if a, _ := ids(get("/v1/enterprise-query?kind=line", http.StatusOK)); a != "line-1" {
		t.Errorf("kind=line → %q", a)
	}
	if a, _ := ids(get("/v1/enterprise-query?q=MAIN", http.StatusOK)); a != "plant-1" {
		t.Errorf("q=MAIN → %q", a)
	}
	if r := get("/v1/enterprise-query?stereotype=ActualLocation", http.StatusOK); len(r.Elements) != 0 {
		t.Errorf("locations → %+v", r.Elements)
	}
	// alive=all reads closed elements too; the default day does not.
	if r := get("/v1/enterprise-query?stereotype=ActualOrganization&alive=all", http.StatusOK); len(r.Elements) != 3 {
		t.Errorf("all → %+v", r.Elements)
	}
	// A refused argument is a 400, and resolve skips what it does not know.
	get("/v1/enterprise-query?limit=0", http.StatusBadRequest)
	if r := get("/v1/enterprise-resolve?ids=line-old,ghost", http.StatusOK); len(r.Elements) != 1 || r.Elements[0].ID != "line-old" || !r.Elements[0].Closed {
		t.Errorf("resolve → %+v", r.Elements)
	}
}
