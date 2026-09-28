package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func TestJournalRetryRepairsOnlyTheQuarantinedTenant(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("retry-tenant-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	defer journal.pool.Exec(ctx, `delete from snapshots where tenant=$1`, id)
	compose := func(id string) (*Tenant, error) {
		return NewTenant(id, NewConsole(id,
			Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{PlatformApp: Admin, "stock": "clerk"}}},
			Seat{Subjects: []string{"bob"}, Member: platform.Member{ID: "bob", Roles: map[string]string{"stock": "clerk"}}}),
			newStock(id))
	}
	source, err := compose(id)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := journal.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new journal: %v %v", entries, err)
	}
	source.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
	member, _ := source.Member("ana")
	at := time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC)
	sub := acceptedBin(t, source, "original")
	receipt, refusal := source.Submit(member, sub, at)
	if refusal != nil || receipt == nil {
		t.Fatalf("initial result: %v", refusal)
	}
	entries, err := journal.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("saved result: %v %v", entries, err)
	}
	saved := entries[0].Body
	var damaged map[string]any
	if err := json.Unmarshal(saved, &damaged); err != nil {
		t.Fatal(err)
	}
	damaged["digest"] = "tampered"
	invalid, _ := json.Marshal(damaged)
	if _, err := journal.pool.Exec(ctx, `update journal set body=$2 where tenant=$1 and seq=1`, id, invalid); err != nil {
		t.Fatal(err)
	}
	old, _ := compose(id)
	corrupt, err := journal.Entries(ctx, id, 0)
	if err != nil || old.recoverEntries(corrupt) == nil || !old.quarantined() {
		t.Fatalf("damaged tenant was not quarantined: %v", err)
	}
	healthy := isolatedStock(t, "healthy-neighbor")
	registry := newTenantRegistry([]*Tenant{old, healthy})
	code := CodeOf(old, healthy)
	if err := journal.SaveSnapshot(ctx, id, 1, code, []byte(`{"apps":null}`)); err != nil {
		t.Fatal(err)
	}
	deployment := &Deployment{SnapshotEvery: 1, Rebuild: func(name string) (*Tenant, error) { return compose(name) }}
	host := NewHost(Tokens(map[string]string{"admin-token": "ana", "user-token": "bob"}), old, healthy)
	host.tenantsFrom = registry.list
	host.Recover = func(ctx context.Context, name string) error {
		return deployment.retryTenant(ctx, journal, registry, code, name)
	}
	handler := host.Handler()
	call := func(token, tenant, path, method string) (int, string) {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(TenantHeader, tenant)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code, response.Body.String()
	}
	if code, _ := call("user-token", id, "/v1/recovery/retry", http.MethodPost); code != http.StatusForbidden {
		t.Fatalf("non-admin could retry recovery: %d", code)
	}
	if code, body := call("admin-token", id, "/v1/recovery/retry", http.MethodPost); code != http.StatusConflict ||
		!strings.Contains(body, "quarantined") || registry.current(id) != old {
		t.Fatalf("corrupt journal revived the tenant: %d %s", code, body)
	}
	if code, _ := call("admin-token", "healthy-neighbor", "/v1/me", http.MethodGet); code != http.StatusOK {
		t.Fatalf("healthy neighbor stopped while another tenant was being repaired: %d", code)
	}
	if _, err := journal.pool.Exec(ctx, `update journal set body=$2 where tenant=$1 and seq=1`, id, saved); err != nil {
		t.Fatal(err)
	}
	if code, body := call("admin-token", id, "/v1/recovery/retry", http.MethodPost); code != http.StatusOK ||
		!strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("valid journal did not revive the tenant: %d %s", code, body)
	}
	recovered := registry.current(id)
	if recovered == old || recovered.quarantined() || !old.quarantined() {
		t.Fatal("partially recovered tenant was reused")
	}
	seq, state, ok, err := journal.Snapshot(ctx, id, code)
	if err != nil || !ok || seq != 1 {
		t.Fatalf("repaired checkpoint is absent: %d %t %v", seq, ok, err)
	}
	fromCheckpoint, _ := compose(id)
	if err := fromCheckpoint.recoverSnapshot(state, seq); err != nil ||
		fromCheckpoint.records.types["stock.bin"].rows["B1"] == nil {
		t.Fatalf("repaired checkpoint still contains the damaged state: %v", err)
	}
	if code, _ := call("admin-token", id, "/v1/me", http.MethodGet); code != http.StatusOK {
		t.Fatalf("recovered tenant is not serving: %d", code)
	}
	retry, refusal := recovered.Submit(member, sub, at.Add(time.Minute))
	if refusal != nil || retry.GetChangeId() != receipt.GetChangeId() || journal.Position(id) != 1 {
		t.Fatalf("retry did not use the durable receipt: %v", refusal)
	}
	if code, _ := call("admin-token", id, "/v1/recovery/retry", http.MethodPost); code != http.StatusConflict {
		t.Fatalf("healthy tenant was unexpectedly rebuilt: %d", code)
	}
	CheckReplay(t, recovered, entries, func() *Tenant {
		fresh, err := compose(id)
		if err != nil {
			t.Fatal(err)
		}
		return fresh
	})
}
