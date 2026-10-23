package platformserver

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"platformserver/platform"
)

func TestDeclaredTenantConfigsPreserveIdentitiesAndRejectOldRequests(t *testing.T) {
	for _, industry := range []string{"hospitality", "manufacturing"} {
		load := func(name string) []TenantSpec {
			t.Helper()
			raw, err := os.ReadFile(filepath.Join("../../deploy/local", industry, name))
			var specs []TenantSpec
			if err != nil || json.Unmarshal(raw, &specs) != nil {
				t.Fatalf("load %s: %v", name, err)
			}
			return specs
		}
		original, next := load("tenants.json"), load("tenants-next.json")
		for _, spec := range next {
			t.Run(spec.ID, func(t *testing.T) {
				oldID := strings.TrimSuffix(spec.ID, "-build")
				i := slices.IndexFunc(original, func(prior TenantSpec) bool { return prior.ID == oldID })
				if i < 0 || oldID == spec.ID || !maps.Equal(spec.Settings, original[i].Settings) || len(spec.Seats) != len(original[i].Seats) {
					t.Fatal("the new scope changed its original configuration or reused its identity")
				}
				admin := ""
				for _, seat := range spec.Seats {
					j := slices.IndexFunc(original[i].Seats, func(prior Seat) bool { return prior.ID == seat.ID })
					if j < 0 || !slices.Equal(seat.Subjects, original[i].Seats[j].Subjects) {
						t.Fatal("the new scope replaced a member or authentication subject")
					}
					for _, app := range []string{"crm", "pms", "mes", "erp", "hcm", "csm", "memstay"} {
						if seat.Roles[app] != "" || slices.ContainsFunc(seat.Grants, func(grant platform.Grant) bool { return grant.App == app }) {
							t.Fatal("the new scope grants a retired native application")
						}
					}
					if seat.Holds("platform", "admin") && seat.Holds("build", "builder") {
						admin = seat.ID
					}
				}
				if admin == "" {
					t.Fatal("the new scope has no explicitly configured builder administrator")
				}
				tn := composeDeclaredTenant(t, spec.ID, spec.Seats)
				member := memberOf(t, tn, admin)
				host := NewHost(Tokens(map[string]string{"configuration-test": spec.Seats[slices.IndexFunc(spec.Seats, func(seat Seat) bool { return seat.ID == admin })].Subjects[0]}), tn)
				host.Now = func() time.Time { return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC) }
				request := submission(tn, member, "build", "build.object.create", "build.object", "BOUNDARY", map[string]any{"name": "boundaryproof", "title": "Boundary proof", "fields": []any{}})
				request.TenantId = oldID
				raw, err := protojson.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				before := txnState(tn)
				for _, header := range []string{oldID, spec.ID} {
					r := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
					r.Header.Set("Authorization", "Bearer configuration-test")
					r.Header.Set("Platform-Tenant", header)
					w := httptest.NewRecorder()
					host.Handler().ServeHTTP(w, r)
					if w.Code < 400 || txnState(tn) != before {
						t.Fatalf("an old tenant request entered the new scope: %d %s", w.Code, w.Body)
					}
				}
				decide(t, tn, admin, "build", "build.object.create", "build.object", "FRESH", map[string]any{"name": "freshscope", "title": "Fresh scope", "fields": []any{}}, host.Now())
			})
		}
	}
}
