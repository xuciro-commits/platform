package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	controlowner "platformserver/kernel/control"
	"strings"
	"testing"
	"time"

	"platformserver/journal"
	"platformserver/platform"
)

type failingAcceptedJournal struct {
	Journals
	tenant  string
	failure error
	commit  bool
}

func (j *failingAcceptedJournal) AppendAccepted(ctx context.Context, tenant string, e Entry, key, hash string) ([]byte, error) {
	if tenant != j.tenant || j.failure == nil {
		return j.Journals.AppendAccepted(ctx, tenant, e, key, hash)
	}
	if j.commit {
		if _, err := j.Journals.AppendAccepted(ctx, tenant, e, key, hash); err != nil {
			return nil, err
		}
	}
	return nil, j.failure
}

func TestAttachedJournalCommitFailureIsTenantLocal(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/committed=%t", backend.name, committed), func(t *testing.T) {
				ctx := context.Background()
				id := fmt.Sprintf("commit-failure-%d", time.Now().UnixNano())
				neighborID := id + "-neighbor"
				t.Cleanup(func() { backend.cleanup(t, id); backend.cleanup(t, neighborID) })
				store := backend.open(t, id)
				defer store.Close()
				compose := func(id string) *Tenant {
					return composeTenant(t, id, []platform.Seat{seatOf("clerk", "stock:clerk")}, newStock(id))
				}
				bad, good := compose(id), compose(neighborID)
				for _, tenant := range []*Tenant{bad, good} {
					if _, err := store.Entries(ctx, tenant.ID, 0); err != nil {
						t.Fatal(err)
					}
				}
				failure := errors.New("append rejected before commit")
				if committed {
					failure = fmt.Errorf("%w: lost commit reply", journal.ErrCommitUnknown)
				}
				injected := &failingAcceptedJournal{Journals: store, tenant: id, failure: failure, commit: committed}
				bad.attachJournal(ctx, injected)
				good.attachJournal(ctx, injected)
				member := memberOf(t, bad, "clerk")
				sub := submission(bad, member, "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "one"})
				at := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
				if receipt, err := bad.Submit(member, sub, at); receipt != nil || err == nil {
					t.Fatalf("failed append answered success: %v %v", receipt, err)
				}
				if bad.quarantined() != committed || rowCount(t, bad, "stock.bin") != 0 ||
					len(bad.app("stock").AcceptedLedger().RecordsFor(id)) != 0 || len(bad.outbound) != 0 {
					t.Fatal("append failure exposed state/effects or used the wrong recovery state")
				}
				decide(t, good, "clerk", "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "neighbor"}, at)
				if good.quarantined() || rowCount(t, good, "stock.bin") != 1 {
					t.Fatal("another tenant stopped after the append failure")
				}
				injected.failure = nil
				if committed {
					if receipt, err := bad.Submit(member, sub, at); receipt != nil || err == nil {
						t.Fatal("unresolved commit accepted another decision")
					}
					registry := newTenantRegistry([]*Tenant{bad, good})
					code := CodeOf(bad, good)
					registry.Add(compose(id + "-added"))
					deployment := Deployment{SnapshotEvery: 1, Rebuild: func(name string) (*Tenant, error) { return compose(name), nil }}
					if err := deployment.retryTenant(ctx, store, registry, code, id); err != nil {
						t.Fatalf("recovery after another tenant was added: %v", err)
					}
					bad = registry.Current(id)
				}
				receipt, err := bad.Submit(member, sub, at.Add(time.Minute))
				if err != nil || receipt == nil || store.Position(id) != 1 || rowCount(t, bad, "stock.bin") != 1 {
					t.Fatalf("original identity did not recover exactly one result: %v %v", receipt, err)
				}
				if duplicate, err := bad.Submit(member, sub, at.Add(2*time.Minute)); err != nil || duplicate.GetChangeId() != receipt.GetChangeId() || store.Position(id) != 1 {
					t.Fatalf("retry made another result: %v %v", duplicate, err)
				}
			})
		}
	}
}

func TestTenantRecoveryChecksOnlyItsStartupComposition(t *testing.T) {
	ctx := context.Background()
	store, err := OpenFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	compose := func(format, version string) *Tenant {
		return composeTenant(t, "composition-recovery", []platform.Seat{seatOf("clerk", "stock:clerk")},
			&formattedStock{stock: newStock("composition-recovery"), format: format, version: version})
	}
	old := compose("stock.v1", "1")
	if _, err := store.Entries(ctx, old.ID, 0); err != nil {
		t.Fatal(err)
	}
	old.attachJournal(ctx, store)
	at := time.Date(2026, 10, 19, 19, 0, 0, 0, time.UTC)
	decide(t, old, "clerk", "stock", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "one"}, at)
	old.quarantine(errors.New("rebuild required"))
	neighbor := isolatedStock(t, "unrelated")
	code := CodeOf(old)
	registry := newTenantRegistry([]*Tenant{neighbor, old})
	for _, change := range []struct{ format, version string }{{"stock.v2", "1"}, {"stock.v1", "2"}} {
		deployment := Deployment{Rebuild: func(string) (*Tenant, error) { return compose(change.format, change.version), nil }}
		if err := deployment.retryTenant(ctx, store, registry, code, old.ID); err == nil || registry.Current(old.ID) != old {
			t.Fatal("different target composition or restore format was accepted")
		}
	}
	deployment := Deployment{Rebuild: func(string) (*Tenant, error) { return compose("stock.v1", "1"), nil }}
	if err := deployment.retryTenant(ctx, store, registry, code, old.ID); err != nil {
		t.Fatalf("unrelated tenant or registry order blocked recovery: %v", err)
	}
	if registry.Current(old.ID) == old || registry.Current(neighbor.ID) != neighbor {
		t.Fatal("recovery replaced the wrong tenant generation")
	}
}

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
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, id)
	defer journal.Pool().Exec(ctx, `delete from snapshots where tenant=$1`, id)
	compose := func(id string) (*Tenant, error) {
		return NewTenant(id, newConsole(id,
			platform.Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, "stock": "clerk"}}},
			platform.Seat{Subjects: []string{"bob"}, Member: platform.Member{ID: "bob", Roles: map[string]string{"stock": "clerk"}}}),
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
	if _, err := journal.Pool().Exec(ctx, `update journal set body=$2 where tenant=$1 and seq=1`, id, invalid); err != nil {
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
	host.tenantsFrom = registry.List
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
		!strings.Contains(body, "quarantined") || registry.Current(id) != old {
		t.Fatalf("corrupt journal revived the tenant: %d %s", code, body)
	}
	if code, _ := call("admin-token", "healthy-neighbor", "/v1/me", http.MethodGet); code != http.StatusOK {
		t.Fatalf("healthy neighbor stopped while another tenant was being repaired: %d", code)
	}
	if _, err := journal.Pool().Exec(ctx, `update journal set body=$2 where tenant=$1 and seq=1`, id, saved); err != nil {
		t.Fatal(err)
	}
	if code, body := call("admin-token", id, "/v1/recovery/retry", http.MethodPost); code != http.StatusOK ||
		!strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("valid journal did not revive the tenant: %d %s", code, body)
	}
	recovered := registry.Current(id)
	if recovered == old || recovered.quarantined() || !old.quarantined() {
		t.Fatal("partially recovered tenant was reused")
	}
	seq, state, ok, err := journal.Snapshot(ctx, id, code)
	if err != nil || !ok || seq != 1 {
		t.Fatalf("repaired checkpoint is absent: %d %t %v", seq, ok, err)
	}
	fromCheckpoint, _ := compose(id)
	if err := fromCheckpoint.recoverSnapshot(state, seq); err != nil ||
		fromCheckpoint.records.Type("stock.bin").Rows().Get("B1") == nil {
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
