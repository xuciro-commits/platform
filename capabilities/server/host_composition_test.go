package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStaticHostMaintenanceWithoutDynamicDefinitions(t *testing.T) {
	tenant := composeTenant(t, "system", []Seat{seatOf("operator", "platform:admin")})
	host := NewHost(Tokens(map[string]string{"operator-token": "operator", "ops-token": "ops", "viewer-token": "viewer"}), tenant)
	host.HostAdmins = map[string]bool{"ops": true}
	handler := host.Handler()
	get := func(path, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(TenantHeader, tenant.ID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, isolated := range []bool{false, true} {
		if isolated {
			tenant.quarantine(errors.New("dynamic definitions could not be recovered"))
		}
		for _, path := range []string{"/healthz", "/v1/host/me", "/v1/host/overview", "/v1/host/tenants/system"} {
			if w := get(path, "ops-token"); w.Code != http.StatusOK {
				t.Fatalf("static maintenance %s isolated=%t: %d %s", path, isolated, w.Code, w.Body.String())
			}
		}
		if w := get("/v1/health", "operator-token"); w.Code != http.StatusOK {
			t.Fatalf("tenant diagnostics isolated=%t: %d %s", isolated, w.Code, w.Body.String())
		}
		if w := get("/v1/host/overview", "viewer-token"); w.Code != http.StatusUnauthorized {
			t.Fatalf("maintenance leaked to a non-administrator: %d", w.Code)
		}
	}
	if w := get("/v1/me", "operator-token"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("dynamic tenant remained available after isolation: %d", w.Code)
	}
}

func TestFoundationalSelectionPreservesAcceptedStateAndNeedsMaintenanceRestart(t *testing.T) {
	ctx := context.Background()
	store, err := OpenFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providers := DefaultRuntimeProviders()
	selectedRules := &controlledRuleProvider{}
	alternative := providers.Rules[0]
	alternative.Module.ID = "test.alternative-rules"
	alternative.Engine = selectedRules
	providers.Rules = append(providers.Rules, alternative)
	compose := func(id string, services RuntimeCapabilities) (*Tenant, error) {
		return NewTenantWithCapabilities(id, services, NewConsole(id, seatOf("operator", "platform:admin", "stock:clerk")), newStock(id))
	}
	tn, err := compose("provider-restart", DefaultRuntimeCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Entries(ctx, tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	tn.attachJournal(ctx, store)
	at := time.Now().UTC()
	decide(t, tn, "operator", "stock", "stock.bin.create", "stock.bin", "R", map[string]any{"code": "retained"}, at)
	host := NewHost(func(token string) (string, bool) { return token, token != "" }, tn)
	host.HostAdmins = map[string]bool{"ops": true}
	host.RuntimeProviders = providers
	handler := host.Handler()
	send := func(subject string, body CompositionRequest) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/v1/host/tenants/"+tn.ID+"/composition", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+subject)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	selection := tn.services.Selection()
	selection["platform.rules"] = alternative.Module.ID
	request := CompositionRequest{Selection: selection, Reason: "replace compatible implementation", At: at.Add(time.Second)}
	if w := send("operator", request); w.Code != http.StatusUnauthorized {
		t.Fatal("tenant admin selected foundational provider", w.Code)
	}
	oldPosition := store.Position(tn.ID)
	original := tn.AcceptResult
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if w := send("ops", request); w.Code == http.StatusOK || tn.console.suspended() {
		t.Fatal("failed append exposed new selection")
	}
	tn.AcceptResult = original
	if w := send("ops", request); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if !tn.console.suspended() || tn.services.RuleModule.ID == alternative.Module.ID {
		t.Fatal("saved choice mutated active generation")
	}
	if w := send("ops", request); w.Code != http.StatusOK || store.Position(tn.ID) != oldPosition+1 {
		t.Fatal("same request did not reuse accepted result")
	}
	if err = tn.setHostLifecycle("resume", "too early", "ops", at.Add(2*time.Second)); err == nil {
		t.Fatal("resumed before selected implementation activation")
	}
	startup, err := compose(tn.ID, DefaultRuntimeCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	d := Deployment{RuntimeProviders: &providers, RebuildWithCapabilities: compose}
	if err = d.restoreTenants(ctx, store, "", []*Tenant{startup}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	tenants := []*Tenant{startup}
	if err = d.restoreRuntimeSelections(ctx, store, "", tenants); err != nil {
		t.Fatal(err)
	}
	rebuilt := tenants[0]
	if rebuilt == startup || rebuilt.quarantined() || rebuilt.services.Rules != selectedRules || !rebuilt.console.suspended() {
		t.Fatalf("selection lost at restart: %+v", rebuilt.compositionView(providers))
	}
	if len(rebuilt.app("stock").AcceptedLedger().RecordsFor(tn.ID)) != 1 {
		t.Fatal("replacement lost business receipts")
	}
	if err = rebuilt.setHostLifecycle("resume", "verified", "ops", at.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	selection = rebuilt.services.Selection()
	selection["platform.authorization"] = ""
	request = CompositionRequest{Selection: selection, Reason: "remove provider", At: at.Add(4 * time.Second)}
	host.tenants = []*Tenant{rebuilt}
	if w := send("ops", request); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if rebuilt.compositionView(providers).State != "maintenance" || !rebuilt.console.suspended() {
		t.Fatal("missing foundational provider did not preserve maintenance access")
	}
	if err = rebuilt.setHostLifecycle("resume", "invalid", "ops", at.Add(5*time.Second)); err == nil {
		t.Fatal("missing implementation silently defaulted")
	}
	position := store.Position(rebuilt.ID)
	for _, subject := range []string{"operator", "ops"} {
		request := httptest.NewRequest("POST", "/v1/host/tenants/"+rebuilt.ID+"/packages/inspect", bytes.NewBufferString(`{"content":{"id":"test.maintenance","version":"1","namespace":"test.maintenance"}}`))
		request.Header.Set("Authorization", "Bearer "+subject)
		response := httptest.NewRecorder()
		host.Handler().ServeHTTP(response, request)
		if subject == "ops" && response.Code != 200 || subject == "operator" && response.Code != 401 {
			t.Fatal("maintenance review did not preserve its independent authority", subject, response.Code, response.Body.String())
		}
	}
	if store.Position(rebuilt.ID) != position || len(testConsoleOf(rebuilt).PackageViews()) != 0 {
		t.Fatal("maintenance review changed formal package state")
	}
}

func TestRetiredPlatformOwnerKeepsHistoryAndOtherOwnersActive(t *testing.T) {
	ctx := context.Background()
	store, err := OpenFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	compose := func() *Tenant {
		return composeTenant(t, "retired-owner", []Seat{seatOf("operator", "platform:admin", "stock:clerk", "p:member")}, newStock("retired-owner"), newNotes("retired-owner", "p"))
	}
	live := compose()
	if _, err = store.Entries(ctx, live.ID, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, store)
	at := time.Now().UTC()
	decide(t, live, "operator", "stock", "stock.bin.create", "stock.bin", "R", map[string]any{"code": "retained"}, at)
	body := CompositionRequest{Selection: live.services.Selection(), DisabledOwners: []string{"stock"}, Reason: "remove unused service", At: at.Add(time.Second)}
	host := NewHost(func(token string) (string, bool) { return token, token != "" }, live)
	host.HostAdmins = map[string]bool{"ops": true}
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/v1/host/tenants/"+live.ID+"/composition", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer ops")
	w := httptest.NewRecorder()
	host.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	fresh := compose()
	d := Deployment{}
	if err = d.restoreTenants(ctx, store, "", []*Tenant{fresh}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err = d.restoreRuntimeSelections(ctx, store, "", []*Tenant{fresh}); err != nil {
		t.Fatal(err)
	}
	if fresh.quarantined() {
		t.Fatal(fresh.fault.Load())
	}
	if err = fresh.setHostLifecycle("resume", "verified", "ops", at.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if fresh.ownerActive("stock") || !fresh.ownerActive("p") || fresh.owner["action:stock.bin.create"] != nil {
		t.Fatal("retired owner still registered")
	}
	member := memberOf(t, fresh, "operator")
	if _, refusal := fresh.RecordOf(member, "stock.bin", "R", at); refusal != nil {
		t.Fatal("retirement hid retained data", refusal)
	}
	if result := refuse(t, fresh, "operator", "stock", "stock.bin.create", "stock.bin", "NEW", map[string]any{"code": "new"}, at); result == "ok" {
		t.Fatal("retired service accepted new work")
	}
	for _, app := range fresh.Apps() {
		if app.ID == "stock" {
			t.Fatal("retired service remained discoverable")
		}
	}
	if len(fresh.app("stock").AcceptedLedger().RecordsFor(live.ID)) != 1 {
		t.Fatal("retirement erased receipt history")
	}
	host.tenants = []*Tenant{fresh}
	body = CompositionRequest{Selection: fresh.services.Selection(), Reason: "restore original module", At: at.Add(3 * time.Second)}
	raw, _ = json.Marshal(body)
	request := httptest.NewRequest("POST", "/v1/host/tenants/"+fresh.ID+"/composition", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer ops")
	response := httptest.NewRecorder()
	host.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	restored := compose()
	if err := d.restoreTenants(ctx, store, "", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := d.restoreRuntimeSelections(ctx, store, "", []*Tenant{restored}); err != nil {
		t.Fatal(err)
	}
	if err := restored.setHostLifecycle("resume", "restored", "ops", at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !restored.ownerActive("stock") || restored.owner["action:stock.bin.create"] == nil {
		t.Fatal("restored module did not register its services")
	}
	if _, failure := restored.RecordOf(memberOf(t, restored, "operator"), "stock.bin", "R", at); failure != nil {
		t.Fatal("reinstall lost original data", failure)
	}
	decide(t, restored, "operator", "stock", "stock.bin.create", "stock.bin", "AFTER", map[string]any{"code": "after"}, at)
	if len(restored.app("stock").AcceptedLedger().RecordsFor(live.ID)) != 2 {
		t.Fatal("reinstall reset or duplicated original receipts")
	}

}

func TestMaintenanceStopsScheduledJobsAndNewOutsideWork(t *testing.T) {
	id := "maintenance-work"
	owner := &stateOnlyJob{&statefulStock{stock: newStock(id)}}
	tenant := composeTenant(t, id, []Seat{seatOf("operator", "platform:admin", "stock:clerk")}, owner)
	at := time.Now().UTC()
	if err := tenant.setHostLifecycle("suspend", "maintenance", "ops", at); err != nil {
		t.Fatal(err)
	}
	tenant.Work(at.Add(time.Hour))
	if owner.accepted != 0 || len(tenant.dispatches(at)) != 0 || len(tenant.turns(at)) != 0 || len(tenant.continuousSources(at)) != 0 {
		t.Fatal("maintenance started background work")
	}
	if err := tenant.setHostLifecycle("resume", "complete", "ops", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	tenant.Work(at.Add(2 * time.Hour))
	if owner.accepted != 1 {
		t.Fatal("resuming did not restore the original job")
	}
	stop := RunWork(tenant)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = stop(ctx)
	ctx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := stop(ctx); err != nil {
		t.Fatal("worker cleanup could not be joined", err)
	}
}

func TestSelectedHostAuthorizationCannotFallBackToHostRole(t *testing.T) {
	host := NewHost(func(token string) (string, bool) { return token, token != "" })
	host.HostAdmins = map[string]bool{"ops": true}
	provider := &controlledAuthorization{failing: true}
	host.AuthorizationEngine = provider
	r := httptest.NewRequest("GET", "/v1/host/overview", nil)
	r.Header.Set("Authorization", "Bearer ops")
	w := httptest.NewRecorder()
	host.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || provider.calls == 0 {
		t.Fatal("host governance bypassed its selected authorization", w.Code)
	}
}
