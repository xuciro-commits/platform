package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestPostgresApplicationRuntimeAndDiagnosticReadScopes(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("application-runtime-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	tn := composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "ai:admin"), seatOf("user", "build:user")}, build.New(id), ai.New(id))
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	tn.attachJournal(ctx, j)
	at := time.Now().UTC().Truncate(time.Microsecond)
	typ := publishObject(t, tn, "builder", "OBJECT", map[string]any{
		"name": "item", "title": "Item",
		"fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}},
		"access": []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true}},
	}, at)
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "APP", map[string]any{
		"name": "wms", "title": "WMS", "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: typ}},
	}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaHandOver, construction.AppType, "APP", map[string]any{}, at)
	decide(t, tn, "user", construction.ID, typ+".create", typ, "I1", map[string]any{"name": "one"}, at)
	if err = tn.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	handler := NewHost(Tokens(map[string]string{"builder-token": "builder"}), tn).Handler()
	get := func(path string, answer any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer builder-token")
		r.Header.Set("Platform-Tenant", id)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), answer); err != nil {
			t.Fatal(err)
		}
		if tn.records.readView != nil {
			t.Fatal("application read retained its SQL snapshot")
		}
	}
	var runtime ApplicationRuntime
	get("/v1/applications/build/wms/runtime", &runtime)
	if runtime.Artifact.Ref.Name != "wms" || len(runtime.Data) != 1 || runtime.Data[0].Records != 1 || runtime.Recovery.State != "ready" {
		t.Fatalf("application runtime lost its data or recovery state: %+v", runtime)
	}
	var diagnostic ApplicationDiagnostic
	get("/v1/applications/build/wms/records/"+typ+"/I1/diagnostic", &diagnostic)
	if diagnostic.Record != "I1" || len(diagnostic.Decisions) != 1 {
		t.Fatalf("application diagnostic lost record history: %+v", diagnostic)
	}
}
