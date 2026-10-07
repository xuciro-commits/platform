package platformserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The same lifecycle as TestApplicationLifecycleAcrossEnvironments, driven
// through the HTTP surface a browser and the host console use (ADR-0080 §3.1):
// builder routes for drafts, preview, save and activation; host-console routes
// for support sessions, promotion and migration. It is the contract the web
// release workbench and the host console are written against.
func TestApplicationLifecycleOverHTTP(t *testing.T) {
	seats := append(upgradeSeats(),
		Seat{Subjects: []string{"user:ops@example.test"}, Member: platform.Member{ID: "ops", Roles: map[string]string{build.ID: build.User}}},
		Seat{Subjects: []string{"user:pub-only@example.test"}, Member: platform.Member{ID: "pub-only", Roles: map[string]string{build.ID: build.Publisher}}})
	compose := func(id string) *Tenant {
		tn, err := NewTenant(id, NewConsole(id, seats...), work.New(id), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	dev, prod := compose("dev"), compose("prod")
	host := NewHost(Tokens(map[string]string{
		"dana": "user:dana@example.test", "pat": "user:pat@example.test", "ops": "user:ops@example.test", "mo": "user:mo@example.test",
		"host": "user:host@example.test",
	}), dev, prod)
	host.HostAdmins = map[string]bool{"user:host@example.test": true}
	clock := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	host.Now = func() time.Time { clock = clock.Add(time.Second); return clock }
	srv := httptest.NewServer(host.Handler())
	defer srv.Close()

	call := func(method, tenant, token, path string, body any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		request, _ := http.NewRequest(method, srv.URL+path, reader)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		if tenant != "" {
			request.Header.Set(TenantHeader, tenant)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		var out map[string]any
		if len(raw) > 0 && raw[0] == '{' {
			_ = json.Unmarshal(raw, &out)
		} else if len(raw) > 0 {
			out = map[string]any{"body": string(raw)}
		}
		return response.StatusCode, out
	}
	keys := 0
	submit := func(tenant, token, typ, target, schema string, payload any) string {
		t.Helper()
		keys++
		raw, _ := json.Marshal(payload)
		authority := build.ID
		code, out := call(http.MethodPost, tenant, token, "/v1/submissions", map[string]any{
			"tenantId": tenant, "principalId": token, "authority": authority, "idempotencyKey": fmt.Sprintf("http-%s-%d", tenant, keys),
			"target": map[string]string{"type": typ, "id": target}, "schema": map[string]any{"name": schema, "version": 1},
			"payload": base64.StdEncoding.EncodeToString(raw)})
		if code != http.StatusOK {
			return fmt.Sprintf("HTTP %d %v", code, out)
		}
		if e, ok := out["error"].(map[string]any); ok {
			return fmt.Sprint(e["code"])
		}
		return "ok"
	}
	visitV1 := map[string]any{"name": "visit", "title": "Visit",
		"fields":  []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
		"states":  []map[string]any{{"name": "open", "title": "Open"}, {"name": "done", "title": "Done"}},
		"actions": []map[string]any{{"name": "close", "title": "Close", "from": []string{"open"}, "to": "done"}}}
	deskV1 := map[string]any{"name": "desk", "title": "Visit desk", "object": "build.visit", "list": []string{"note"}, "detail": []string{"note"}}
	appV1 := map[string]any{"name": "frontdesk", "title": "Front desk", "pages": []string{"desk"}}
	drafts := []map[string]string{{"kind": "object", "id": "O1"}, {"kind": "page", "id": "P1"}, {"kind": "app", "id": "A1"}}
	define := func(tenant string) {
		t.Helper()
		for _, step := range []struct {
			typ, target string
			payload     any
		}{{build.ObjectType, "O1", visitV1}, {build.PageType, "P1", deskV1}, {build.AppType, "A1", appV1}} {
			if got := submit(tenant, "dana", step.typ, step.target, step.typ+".create", step.payload); got != "ok" {
				t.Fatalf("%s %s: %s", tenant, step.typ, got)
			}
		}
	}
	freeze := func(tenant string) string {
		t.Helper()
		code, preview := call(http.MethodPost, tenant, "dana", "/v1/releases/preview", map[string]any{"drafts": drafts})
		if code != http.StatusOK || preview["candidateId"] == "" || preview["diagnostic"] != nil && preview["diagnostic"] != "" {
			t.Fatalf("%s preview: %d %v", tenant, code, preview)
		}
		id := preview["candidateId"].(string)
		if code, out := call(http.MethodPost, tenant, "dana", "/v1/releases/candidates", map[string]any{"drafts": drafts, "candidateId": id, "key": "save-" + id}); code != http.StatusOK {
			t.Fatalf("%s save: %d %v", tenant, code, out)
		}
		if _, ok := host.tenantByID(tenant).SealedArtifact(id); !ok {
			t.Fatalf("%s: saving over HTTP did not seal the candidate", tenant)
		}
		return id
	}
	activate := func(tenant, token, id, key, upgrade string) (int, map[string]any) {
		body := map[string]any{"candidateId": id, "key": key}
		if upgrade != "" {
			body["upgradeId"] = upgrade
		}
		return call(http.MethodPost, tenant, token, "/v1/releases/active", body)
	}
	activeRelease := func(tenant string) string {
		_, out := call(http.MethodGet, tenant, "ops", "/v1/releases/active", nil)
		id, _ := out["id"].(string)
		return id
	}

	// 1–2. dev: define, freeze, activate, operate.
	define("dev")
	v1 := freeze("dev")
	if code, out := activate("dev", "mo", v1, "activate-v1", ""); code == http.StatusOK {
		t.Fatalf("a member without a release role activated: %v", out)
	}
	if code, out := activate("dev", "dana", v1, "activate-v1", ""); code != http.StatusOK {
		t.Fatalf("dev activate: %d %v", code, out)
	}
	if activeRelease("dev") != v1 {
		t.Fatal("dev did not activate v1")
	}
	for i := 1; i <= 3; i++ {
		if got := submit("dev", "ops", "build.visit", fmt.Sprintf("V%d", i), "build.visit.create", map[string]any{"note": fmt.Sprintf("visit %d", i)}); got != "ok" {
			t.Fatalf("dev visit %d: %s", i, got)
		}
	}

	// 3. Host console: a support session names the prod member the console
	// acts as; promotion moves the sealed bytes and activates them. prod never
	// authored the application: the drafts activation publishes onto are
	// created from the sealed image, under the source's IDs (UX-06).
	if code, _ := call(http.MethodPost, "", "dana", "/v1/host/tenants/prod/support", map[string]any{"member": "pat", "reason": "release", "minutes": 30}); code != http.StatusUnauthorized {
		t.Fatalf("a tenant member opened the host console: %d", code)
	}
	code, pub := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/support", map[string]any{"member": "pub-only", "reason": "promote v1 as publisher", "minutes": 30})
	if code != http.StatusOK {
		t.Fatalf("publisher support session: %d %v", code, pub)
	}
	if code, out := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v1, "key": "promote-v1-pub", "activate": true, "targetGrant": pub["id"]}); code != http.StatusConflict || !strings.Contains(fmt.Sprint(out["error"]), "builder role") {
		t.Fatalf("a publisher materialised drafts in an empty environment: %d %v", code, out)
	}
	code, grant := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/support", map[string]any{"member": "dana", "reason": "promote v1", "minutes": 30})
	if code != http.StatusOK || grant["id"] == nil {
		t.Fatalf("support session: %d %v", code, grant)
	}
	prodGrant := grant["id"].(string)
	code, promoted := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v1, "key": "promote-v1", "activate": true, "targetGrant": prodGrant})
	if code != http.StatusOK || promoted["to"] != "prod" || promoted["candidate"] != v1 {
		t.Fatalf("promotion: %d %v", code, promoted)
	}
	if activeRelease("prod") != v1 {
		t.Fatal("prod did not activate the promoted candidate")
	}
	if code, out := call(http.MethodGet, "prod", "pat", "/v1/records/build.object/visit", nil); code != http.StatusOK || !strings.Contains(fmt.Sprint(out["record"]), "published") {
		t.Fatalf("prod has no published draft for the promoted object: %d %v", code, out)
	}
	if code, again := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v1, "key": "promote-v1", "activate": true, "targetGrant": prodGrant}); code != http.StatusOK || again["digest"] != promoted["digest"] {
		t.Fatalf("a repeated promotion is not idempotent: %d %v", code, again)
	}
	if code, out := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v1, "key": "promote-v1", "activate": true, "targetGrant": "support:nothing"}); code != http.StatusForbidden {
		t.Fatalf("an unknown support session promoted: %d %v", code, out)
	}

	// 4. Migration: a source session reads dev, the target session writes prod.
	code, devGrant := call(http.MethodPost, "", "host", "/v1/host/tenants/dev/support", map[string]any{"member": "ops", "reason": "migrate visits", "minutes": 30})
	if code != http.StatusOK {
		t.Fatalf("dev support session: %d %v", code, devGrant)
	}
	code, prodOps := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/support", map[string]any{"member": "ops", "reason": "migrate visits", "minutes": 30})
	if code != http.StatusOK {
		t.Fatalf("prod support session: %d %v", code, prodOps)
	}
	migrate := map[string]any{"from": "dev", "types": []string{"build.visit"}, "sourceGrant": devGrant["id"], "targetGrant": prodOps["id"]}
	code, migration := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/migrations", migrate)
	if code != http.StatusOK {
		t.Fatalf("migration: %d %v", code, migration)
	}
	if parts, _ := migration["types"].([]any); len(parts) != 1 || parts[0].(map[string]any)["written"] != float64(3) {
		t.Fatalf("migration wrote the wrong rows: %v", migration)
	}
	if code, out := call(http.MethodGet, "prod", "ops", "/v1/records/build.visit/V2", nil); code != http.StatusOK || !strings.Contains(fmt.Sprint(out["record"]), "visit 2") {
		t.Fatalf("prod cannot read a migrated visit: %d %v", code, out)
	}
	code, manifests := call(http.MethodGet, "", "host", "/v1/host/tenants/prod/migrations", nil)
	if code != http.StatusOK || !strings.Contains(fmt.Sprint(manifests["body"]), migration["key"].(string)) {
		t.Fatalf("the console does not list the migration: %d %v", code, manifests)
	}

	// 5. v2 with an optional field: reviewed plan on each environment.
	visitV2 := map[string]any{"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}, {"name": "priority", "title": "Priority", "type": "integer"}}}
	if got := submit("dev", "dana", build.ObjectType, "O1", build.ObjectType+".edit", visitV2); got != "ok" {
		t.Fatalf("dev v2 draft: %s", got)
	}
	v2 := freeze("dev")
	if code, _ := activate("dev", "dana", v2, "activate-v2-plain", ""); code == http.StatusOK {
		t.Fatal("dev activated a storage change without a plan")
	}
	code, review := call(http.MethodGet, "dev", "dana", "/v1/releases/candidates/"+v2, nil)
	plan, _ := review["upgradePlan"].(map[string]any)
	if code != http.StatusOK || plan == nil || plan["id"] == nil {
		t.Fatalf("dev review lacks the plan: %d %v", code, review)
	}
	if code, out := activate("dev", "dana", v2, "activate-v2", plan["id"].(string)); code != http.StatusOK {
		t.Fatalf("dev upgrade: %d %v", code, out)
	}
	// The materialised drafts are named after their assets.
	if got := submit("prod", "dana", build.ObjectType, "visit", build.ObjectType+".edit", visitV2); got != "ok" {
		t.Fatalf("prod v2 draft: %s", got)
	}
	if code, _ := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v2, "key": "promote-v2", "activate": true, "targetGrant": prodGrant}); code != http.StatusConflict {
		t.Fatal("prod activated a storage change during promotion without a plan")
	}
	if code, out := call(http.MethodPost, "", "host", "/v1/host/tenants/prod/promotions", map[string]any{"from": "dev", "candidate": v2, "key": "promote-v2-hold", "activate": false, "targetGrant": prodGrant}); code != http.StatusOK {
		t.Fatalf("promote v2 without activation: %d %v", code, out)
	}
	code, prodReview := call(http.MethodGet, "prod", "pat", "/v1/releases/candidates/"+v2, nil)
	prodPlan, _ := prodReview["upgradePlan"].(map[string]any)
	if code != http.StatusOK || prodPlan == nil {
		t.Fatalf("prod review lacks the plan: %d %v", code, prodReview)
	}
	if code, out := activate("prod", "pat", v2, "activate-v2", prodPlan["id"].(string)); code != http.StatusOK {
		t.Fatalf("prod upgrade: %d %v", code, out)
	}
	if activeRelease("prod") != v2 {
		t.Fatal("prod did not move to v2")
	}
	if got := submit("prod", "ops", "build.visit", "V3", "build.visit.edit", map[string]any{"priority": 9}); got != "ok" {
		t.Fatalf("prod cannot write the new optional field: %s", got)
	}
	// The host overview reflects both environments without reading records.
	code, overview := call(http.MethodGet, "", "host", "/v1/host/tenants/prod", nil)
	if code != http.StatusOK || overview["activeRelease"] != v2 {
		t.Fatalf("host view of prod: %d %v", code, overview)
	}
}
