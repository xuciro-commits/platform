package platformserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"
)

type scopeReadStock struct {
	*formattedStock
	entered chan struct{}
	release chan struct{}
}

func (s *scopeReadStock) Manifest() platform.Manifest {
	m := s.formattedStock.Manifest()
	m.Reads = []string{"stock-pair"}
	return m
}
func (s *scopeReadStock) Read(c platform.Caller, name string) (any, *kernel.Error) {
	first, _ := platform.Get[Item](c, "I1")
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	second, _ := platform.Get[Item](c, "I1")
	return []int{first.Qty, second.Qty}, nil
}

type failingReadStorage struct {
	journal.RuntimeState
}

func (s failingReadStorage) BeginStateRead(context.Context, string, int64) (journal.StateReadView, error) {
	return nil, errors.New("temporary test read unavailable")
}

func TestPostgresRecordReadScopeCoversNestedReadsCommitAndHTTP(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("read-scope-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	app := &scopeReadStock{formattedStock: &formattedStock{stock: newStock(id), format: "stock.ledger.v1"}}
	live := composeTenant(t, id, []Seat{seatOf("ana", "stock:clerk", "platform:admin")}, app)
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	now := time.Now().UTC()
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, now)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	member := memberOf(t, live, "ana")
	assertQty := func(want int) {
		t.Helper()
		page, refusal := live.Records(member, "stock.item", platform.Query{Limit: 10}, now)
		if refusal != nil || page.Total != 1 || page.Records[0].(Item).Qty != want {
			t.Fatal(page, refusal)
		}
		detail, refusal := live.RecordOf(member, "stock.item", "I1", now)
		if refusal != nil || detail.Record.(Item).Qty != want {
			t.Fatal(detail, refusal)
		}
		if live.records.readView != nil || live.records.types["stock.item"].rows.Stored() != 0 {
			t.Fatal("scope retained rows or SQL snapshot")
		}
	}
	assertQty(1)
	app.entered = make(chan struct{})
	app.release = make(chan struct{})
	readDone := make(chan []int, 1)
	go func() {
		out, refusal := live.Read(member, "stock-pair")
		if refusal != nil {
			readDone <- nil
			return
		}
		readDone <- out.([]int)
	}()
	<-app.entered
	writerDone := make(chan *kernel.Error, 1)
	sub := submission(live, member, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2})
	go func() { _, refusal := live.Submit(member, sub, now.Add(time.Second)); writerDone <- refusal }()
	select {
	case err := <-writerDone:
		t.Fatalf("writer passed an unfinished read: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(app.release)
	if pair := <-readDone; len(pair) != 2 || pair[0] != 1 || pair[1] != 1 {
		t.Fatal("nested reads mixed positions", pair)
	}
	if refusal := <-writerDone; refusal != nil {
		t.Fatal(refusal)
	}
	app.entered = nil
	app.release = nil
	assertQty(2)

	// Acceptance has committed SQL but cannot expose a new business view until
	// the same tenant lane finishes applying its saved result.
	original := live.AcceptResult
	committed := make(chan struct{})
	apply := make(chan struct{})
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		raw, err := original(entry, key, hash)
		if err != nil {
			return nil, err
		}
		old := live.records.types["stock.item"].rows.Get("I1")
		if old.Value.Interface().(Item).Qty != 2 {
			return nil, fmt.Errorf("old execution view observed committed future state")
		}
		close(committed)
		<-apply
		return raw, nil
	}
	sub = submission(live, member, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 3})
	go func() { _, refusal := live.Submit(member, sub, now.Add(2*time.Second)); writerDone <- refusal }()
	<-committed
	pageDone := make(chan RecordPage, 1)
	go func() {
		page, _ := live.Records(member, "stock.item", platform.Query{Limit: 10}, now)
		pageDone <- page
	}()
	select {
	case <-pageDone:
		t.Fatal("business read crossed committed-but-unapplied result")
	case <-time.After(30 * time.Millisecond):
	}
	close(apply)
	if refusal := <-writerDone; refusal != nil {
		t.Fatal(refusal)
	}
	if page := <-pageDone; page.Total != 1 || page.Records[0].(Item).Qty != 3 {
		t.Fatal(page)
	}
	live.AcceptResult = original
	assertQty(3)

	// A refused private decision leaves the current SQL view and subsequent
	// independent reads unchanged.
	bad := submission(live, member, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": "bad"})
	if _, refusal := live.Submit(member, bad, now.Add(3*time.Second)); refusal == nil {
		t.Fatal("invalid private write accepted")
	}
	assertQty(3)

	store := live.records.historyStore
	live.records.historyStore = failingReadStorage{RuntimeState: store}
	before := j.Position(id)
	if _, refusal := live.Records(member, "stock.item", platform.Query{}, now); refusal == nil || live.quarantined() {
		t.Fatal("temporary read failure stopped tenant or became empty success")
	}
	bad = submission(live, member, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 4})
	if _, refusal := live.Submit(member, bad, now); refusal == nil || live.quarantined() || j.Position(id) != before {
		t.Fatal("writer bypassed unavailable read view")
	}
	live.records.historyStore = store
	assertQty(3)

	handler := NewHost(Tokens(map[string]string{"ana-token": "ana"}), live).Handler()
	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/records/stock.item", ""},
		{http.MethodGet, "/v1/records/stock.item/I1", ""},
		{http.MethodGet, "/v1/stock-pair", ""},
		{http.MethodGet, "/v1/health", ""},
		{http.MethodPost, "/v1/records/stock.item/query", `{"limit":10}`},
		{http.MethodPost, "/v1/aggregates/stock.item/query", `{"measures":["count"]}`},
	} {
		r := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		r.Header.Set("Authorization", "Bearer ana-token")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatal(request.path, w.Code, w.Body.String())
		}
	}
	app.entered = make(chan struct{})
	app.release = make(chan struct{})
	cancelled, cancel := context.WithCancel(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/stock-pair", nil).WithContext(cancelled)
	r.Header.Set("Authorization", "Bearer ana-token")
	w := httptest.NewRecorder()
	httpDone := make(chan struct{})
	go func() { handler.ServeHTTP(w, r); close(httpDone) }()
	<-app.entered
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var active int
		if err := j.Pool().QueryRow(ctx, `select count(*) from pg_stat_activity where datname=current_database() and pid<>pg_backend_pid() and state='idle in transaction'`).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled HTTP read retained SQL view")
		}
		time.Sleep(time.Millisecond)
	}
	close(app.release)
	<-httpDone
	if live.quarantined() {
		t.Fatal("request cancellation stopped the tenant", live.fault.Load())
	}
	assertQty(3)

	// Cancelling inside StateCurrent unwinds a record-store critical section.
	// Navigation must not leave its mutex held after the request is refused.
	for _, during := range []string{"detail", "authorization"} {
		interrupted := &interruptedRowStorage{RuntimeState: store, views: store.(journal.StateReadViews), entered: make(chan struct{}), release: make(chan struct{})}
		live.records.historyStore = interrupted
		detailCtx, stopDetail := context.WithCancel(ctx)
		detailRequest := httptest.NewRequest(http.MethodGet, "/v1/records/stock.item/I1", nil).WithContext(detailCtx)
		detailRequest.Header.Set("Authorization", "Bearer ana-token")
		detailDone := make(chan struct{})
		go func() {
			if during == "detail" {
				handler.ServeHTTP(httptest.NewRecorder(), detailRequest)
			} else {
				_, _ = coordinatedRecordRead(live, detailCtx, func() (struct{}, *kernel.Error) {
					caller := platform.NewCaller(runtime{live}, member, "stock", false, false)
					live.authorizeFrom(live.records, caller, platform.AuthzRequest{Principal: member.ID, Tenant: live.ID, Kind: "read", Type: "stock.item", Record: "I1"}, now, false)
					return struct{}{}, nil
				})
			}
			close(detailDone)
		}()
		<-interrupted.entered
		stopDetail()
		if err := interrupted.opened.Close(); err != nil {
			t.Fatal(err)
		}
		close(interrupted.release)
		select {
		case <-detailDone:
		case <-time.After(3 * time.Second):
			t.Fatal("cancelled detail request did not finish")
		}
		live.records.historyStore = store
		discoveryDone := make(chan int, 1)
		go func() {
			request := httptest.NewRequest(http.MethodGet, "/v1/actions", nil)
			request.Header.Set("Authorization", "Bearer ana-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			discoveryDone <- response.Code
		}()
		select {
		case status := <-discoveryDone:
			if status != http.StatusOK {
				t.Fatal("discovery failed after cancelled detail", status)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("cancelled detail retained the record-store mutex")
		}
	}
	decide(t, live, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 4}, now.Add(4*time.Second))
	assertQty(4)
}

type interruptedRowStorage struct {
	journal.RuntimeState
	views            journal.StateReadViews
	entered, release chan struct{}
	once             sync.Once
	opened           journal.StateReadView
}

func (s *interruptedRowStorage) BeginStateRead(ctx context.Context, tenant string, position int64) (journal.StateReadView, error) {
	view, err := s.views.BeginStateRead(ctx, tenant, position)
	if err != nil {
		return nil, err
	}
	s.opened = view
	return &interruptedRowView{StateReadView: view, storage: s}, nil
}

type interruptedRowView struct {
	journal.StateReadView
	storage *interruptedRowStorage
}

func (v *interruptedRowView) StateCurrent(ctx context.Context, tenant, typ, id string) (journal.StateCurrent, error) {
	v.storage.once.Do(func() {
		close(v.storage.entered)
		<-v.storage.release
	})
	return v.StateReadView.StateCurrent(ctx, tenant, typ, id)
}
