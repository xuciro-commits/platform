package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func isolatedStock(t *testing.T, id string) *Tenant {
	t.Helper()
	seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{PlatformApp: Admin, "stock": "clerk"}}}
	tenant, err := NewTenant(id, NewConsole(id, seat), newStock(id))
	if err != nil {
		t.Fatal(err)
	}
	return tenant
}

func acceptedBin(t *testing.T, tn *Tenant, key string) *pb.Submission {
	t.Helper()
	return &pb.Submission{TenantId: tn.ID, PrincipalId: "ana", Authority: "stock", IdempotencyKey: key,
		Target: &pb.EntityRef{Type: "stock.bin", Id: "B1"}, Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{"code":"A"}`)}
}

func TestCommittedResultCorruptionQuarantinesOnlyItsTenant(t *testing.T) {
	bad := isolatedStock(t, "t-1")
	good := isolatedStock(t, "t-2")
	member, _ := bad.Member("ana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	bad.AcceptResult = func(Entry, string, string) ([]byte, error) { return []byte(`{}`), nil }
	if receipt, err := bad.Submit(member, acceptedBin(t, bad, "bad"), at); receipt != nil || err == nil {
		t.Fatalf("corrupt commit answered: %+v %v", receipt, err)
	}
	if !bad.quarantined() || bad.records.types["stock.bin"].rows["B1"] != nil {
		t.Fatal("partial tenant remained writable")
	}
	if bad.Health(at).Status != "quarantined" || bad.Health(at).RecoveryError == "" {
		t.Fatal("recovery failure absent from health")
	}
	if bad.Round(at, 1) || len(bad.dispatches(at)) != 0 || len(bad.turns(at)) != 0 {
		t.Fatal("quarantined tenant scheduled work")
	}
	if receipt, err := bad.Submit(member, acceptedBin(t, bad, "later"), at); receipt != nil || err == nil {
		t.Fatal("quarantined tenant accepted another input")
	}
	otherMember, _ := good.Member("ana")
	good.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	if receipt, err := good.Submit(otherMember, acceptedBin(t, good, "good"), at); err != nil || receipt == nil {
		t.Fatalf("healthy tenant was affected: %+v %v", receipt, err)
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana"}), bad, good).Handler()
	call := func(path, tenant string) (int, string) {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer ana-token")
		if tenant != "" {
			req.Header.Set(TenantHeader, tenant)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out.Code, out.Body.String()
	}
	if code, body := call("/v1/me", "t-1"); code != 503 || !strings.Contains(body, "TENANT_QUARANTINED") {
		t.Fatalf("bad tenant: %d %s", code, body)
	}
	if code, body := call("/v1/health", "t-1"); code != 200 || !strings.Contains(body, `"status":"quarantined"`) {
		t.Fatalf("health: %d %s", code, body)
	}
	if code, body := call("/v1/me", "t-2"); code != 200 || !strings.Contains(body, `"tenantId":"t-2"`) {
		t.Fatalf("good tenant: %d %s", code, body)
	}
	if code, body := call("/healthz", ""); code != 200 || !strings.Contains(body, `"quarantined":1`) {
		t.Fatalf("process health: %d %s", code, body)
	}
}

func TestReplayCorruptionQuarantinesWithoutSideEffects(t *testing.T) {
	source := isolatedStock(t, "t-1")
	member, _ := source.Member("ana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	var durable Entry
	source.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { durable = e; return e.Body, nil }
	if _, err := source.Submit(member, acceptedBin(t, source, "saved"), at); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(durable.Body, &body); err != nil {
		t.Fatal(err)
	}
	body["digest"] = "tampered"
	durable.Body, _ = json.Marshal(body)
	bad := isolatedStock(t, "t-1")
	bad.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("recovery must never call an external endpoint")
		return nil, nil
	}
	if err := bad.recoverEntries([]Entry{durable}); err == nil || !bad.quarantined() {
		t.Fatalf("corrupt accepted history did not isolate its tenant: %v", err)
	}
	if bad.records.types["stock.bin"].rows["B1"] != nil || bad.Round(at, 1) {
		t.Fatal("corrupt result was applied or work continued")
	}
	healthy := isolatedStock(t, "t-2")
	if err := healthy.recoverEntries(nil); err != nil || healthy.quarantined() {
		t.Fatalf("neighbor's recovery was interrupted: %v", err)
	}
}

func TestReplayPanicCannotTakeDownNeighbor(t *testing.T) {
	source := isolatedStock(t, "t-1")
	member, _ := source.Member("ana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	var durable Entry
	source.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { durable = e; return e.Body, nil }
	if _, err := source.Submit(member, acceptedBin(t, source, "missing-listener"), at); err != nil {
		t.Fatal(err)
	}
	var result acceptedResult
	if err := json.Unmarshal(durable.Body, &result); err != nil {
		t.Fatal(err)
	}
	result.Event.Subscribers = append(result.Event.Subscribers, "removed-app")
	var err error
	result.Digest, err = digestAcceptedResult(result)
	if err != nil {
		t.Fatal(err)
	}
	durable.Body, _ = json.Marshal(result)
	bad := isolatedStock(t, "t-1")
	if err := bad.recoverEntries([]Entry{durable}); err == nil || !bad.quarantined() {
		t.Fatalf("incompatible subscribed work was not isolated: %v", err)
	}
	if receipt, err := bad.Submit(member, acceptedBin(t, bad, "after"), at); receipt != nil || err == nil {
		t.Fatal("partly replayed tenant resumed")
	}
	healthy := isolatedStock(t, "t-2")
	otherMember, _ := healthy.Member("ana")
	if _, err := healthy.Submit(otherMember, acceptedBin(t, healthy, "independent"), at); err != nil {
		t.Fatalf("healthy tenant stopped: %v", err)
	}
}

func TestRestoreFailureQuarantinesOnlyItsTenant(t *testing.T) {
	bad := isolatedStock(t, "t-1")
	good := isolatedStock(t, "t-2")
	if err := bad.recoverSnapshot([]byte(`{"apps":null}`), 7); err == nil || !bad.quarantined() {
		t.Fatalf("bad snapshot did not quarantine tenant: %v", err)
	}
	if good.quarantined() {
		t.Fatal("neighbor was quarantined")
	}
}

func TestJournalCorruptionIsTenantLocal(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	bad, good := fmt.Sprintf("quarantine-bad-%d", time.Now().UnixNano()), fmt.Sprintf("quarantine-good-%d", time.Now().UnixNano())
	defer func() {
		j.Pool().Exec(ctx, `delete from journal where tenant = any($1)`, []string{bad, good})
		j.Pool().Exec(ctx, `delete from snapshots where tenant = $1`, bad)
	}()
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		tenant string
		seq    int
	}{{bad, 2}, {good, 1}} {
		if _, err := j.Pool().Exec(ctx, `insert into journal (tenant,seq,app,kind,principal,body,at) values ($1,$2,'stock','input','{}','{}',$3)`,
			row.tenant, row.seq, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := j.Entries(ctx, bad, 0); !errors.Is(err, errTenantJournal) {
		t.Fatalf("missing entry was not diagnosed as tenant damage: %v", err)
	}
	if entries, err := j.Entries(ctx, good, 0); err != nil || len(entries) != 1 {
		t.Fatalf("neighbor journal failed: %v %v", entries, err)
	}
	if _, err := j.Pool().Exec(ctx, `insert into snapshots (tenant,seq,code,state) values ($1,2,'bad-code',$2)`,
		bad, []byte("not gzip")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := j.Snapshot(ctx, bad, "bad-code"); !errors.Is(err, errTenantSnapshot) {
		t.Fatalf("bad snapshot was not diagnosed as tenant damage: %v", err)
	}
}

func TestQuarantineDropsLateEffectAnswer(t *testing.T) {
	tn := isolatedStock(t, "t-1")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	tn.endpoints = []*Endpoint{{ID: "receiver", Kind: "webhook", URL: "http://localhost:8080/",
		Secret: "hook", AllowPrivate: true}}
	tn.outbound = []*effect{{Effect: platform.Effect{ID: "effect-1", Endpoint: "receiver",
		Event: "stock.bin.create", At: at, Due: at, State: "pending", Body: `{}`}}}
	tn.Secrets = func(string) ([]byte, bool) { return []byte("secret"), true }
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	tn.Record = func(Entry) { t.Error("late answer was journaled after isolation") }
	go func() { tn.Dispatch(at); close(finished) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("effect did not start")
	}
	tn.quarantine(fmt.Errorf("injected recovery failure"))
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight effect did not finish")
	}
	if got := tn.Effects(at); len(got) != 1 || got[0].State != "pending" {
		t.Fatalf("damaged tenant accepted late effect answer: %+v", got)
	}
}

func TestOwnedWorkPanicIsTenantLocal(t *testing.T) {
	bad := isolatedStock(t, "t-1")
	good := isolatedStock(t, "t-2")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	bad.queues["stock"] = []*Task{{ID: "broken", Kind: "delivery", App: "stock", State: "queued", Due: at}}
	if bad.Round(at, 1) || !bad.quarantined() {
		t.Fatal("malformed owned work did not isolate its tenant")
	}
	if good.Round(at, 1) || good.quarantined() {
		t.Fatal("healthy tenant stopped due to its neighbor's work failure")
	}
}

func TestQuarantinedTenantCannotRecordLateWorkOrSnapshot(t *testing.T) {
	tn := isolatedStock(t, "t-1")
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	tn.agents = &Agents{t: tn}
	tn.quarantine(fmt.Errorf("damaged accepted result"))
	tn.Record = func(Entry) { t.Error("quarantined tenant wrote to its journal") }
	tn.settle("missing-effect", platform.Outcome{Effect: "missing-effect"}, now)
	tn.agentStep(stepBody{Run: "missing-run"}, now)
	if _, _, err := tn.Snapshot(func() int64 { return 1 }); err == nil {
		t.Fatal("quarantined tenant produced a snapshot")
	}
}
