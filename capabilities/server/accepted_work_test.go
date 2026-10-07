package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	"platformserver/platform"
)

type workDocument struct {
	platform.Record
	Number string `json:"number"`
}

type resultWorker struct {
	ledger *platform.Ledger
	calls  int
	reject bool
	emit   bool
	idle   bool
}

func newResultWorker(id string) *resultWorker {
	return &resultWorker{ledger: platform.NewLedger(id, "worker", platform.NewCatalog(platform.Action{
		Schema: "worker.document.write", Target: "worker.document", Title: "Write", Description: "Write a numbered document.",
		Payload: []platform.Field{}, Automation: true}), "worker.document")}
}
func (w *resultWorker) Manifest() platform.Manifest {
	return platform.Manifest{ID: "worker", Title: "Worker", Version: "1", Actions: w.ledger.Catalog,
		Entities:  []platform.Entity{{Type: "worker.document", Title: "Document", Model: workDocument{}}},
		Sequences: []platform.Sequence{{Name: "document", Pattern: "D/{n}"}},
		Jobs:      []platform.Job{{Name: "pulse", Title: "Pulse", Every: time.Hour}},
		Emits:     []platform.EffectKind{{Name: "publish", Title: "Publish", Description: "Publish the document."}},
		Retry:     &platform.Retry{Initial: time.Second, Max: time.Minute, Attempts: 2}}
}
func (w *resultWorker) Declarations() []*pb.AuthorityDeclaration { return w.ledger.Declarations() }
func (w *resultWorker) AcceptedLedger() *platform.Ledger         { return w.ledger }
func (*resultWorker) AcceptedWork()                              {}
func (*resultWorker) AcceptedActionSchemas() []string            { return []string{"worker.document.write"} }
func (w *resultWorker) Snapshot() (json.RawMessage, error)       { return w.ledger.Snapshot() }
func (w *resultWorker) Restore(raw json.RawMessage) error        { return w.ledger.Restore(raw) }
func (w *resultWorker) Submit(c platform.Caller, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return w.ledger.Receive(c, sub, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			number, _ := c.Next(r, "document", at)
			c.Put(r, workDocument{Record: platform.Record{ID: sub.GetTarget().GetId()}, Number: number})
			c.Assign(r, platform.Assignment{Title: "Read the document", Key: sub.GetTarget().GetId(),
				Ref: "worker.document/" + sub.GetTarget().GetId(), To: []platform.Recipient{{Member: "ana"}}})
			c.Notify(platform.Notification{Title: "Written", Key: sub.GetTarget().GetId()}, at, platform.Recipient{Member: "ana"})
			if w.emit {
				c.Emit("publish", sub.GetTarget().GetId(), "worker.document/"+sub.GetTarget().GetId(), map[string]string{"number": number}, at)
			}
		}, nil
	})
}
func (*resultWorker) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (*resultWorker) Read(platform.Caller, string) (any, *kernel.Error) { return nil, unknown() }
func (w *resultWorker) write(c platform.Caller, id string, at time.Time) *kernel.Error {
	w.calls++
	_, err := w.Submit(c, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: c.App,
		IdempotencyKey: id, Target: &pb.EntityRef{Type: "worker.document", Id: id},
		Schema: &pb.SchemaRef{Name: "worker.document.write", Version: 1}, Payload: []byte(`{}`)}, at)
	if err == nil && w.reject {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return err
}
func (w *resultWorker) Run(c platform.Caller, _ string, at time.Time) *kernel.Error {
	if w.idle {
		return nil
	}
	return w.write(c, "job-"+at.UTC().Format(time.RFC3339Nano), at)
}
func (w *resultWorker) Handle(c platform.Caller, e platform.Event) *kernel.Error {
	return w.write(c, "event-"+e.Record.GetChangeId(), e.Record.GetRecordedTime().AsTime())
}
func (*resultWorker) Interested(names []string, _ platform.Event) bool {
	return slices.Contains(names, "stock.item.create")
}
func (w *resultWorker) Listen(c platform.Caller, e platform.Event, _ []string, now time.Time) *kernel.Error {
	return w.write(c, "event-"+e.Record.GetChangeId(), now)
}

func resultWorkTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	tn, err := NewTenant(id, NewConsole(id, Seat{Subjects: []string{"ana"},
		Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}),
		newStock(id), newResultWorker(id), work.New(id))
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestAcceptedIdleWorkDoesNotInvalidateBusinessData(t *testing.T) {
	live := resultWorkTenant(t, "idle-work")
	live.app("worker").(*resultWorker).idle = true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	seq, data, _ := live.changeState()
	for _, task := range live.jobs {
		if task.App != "worker" {
			continue
		}
		live.mu.Lock()
		outcome := live.attempt(task, time.Now(), false)
		live.mu.Unlock()
		after, nextData, _ := live.changeState()
		if outcome != "ok" || after != seq+1 || nextData != data {
			t.Fatalf("idle work invalidated data: outcome=%s seq=%d/%d data=%d/%d", outcome, seq, after, data, nextData)
		}
		return
	}
	t.Fatal("worker job missing")
}

func TestAcceptedWorkCommitsAttemptRecordsTasksAndNoticesTogether(t *testing.T) {
	live := resultWorkTenant(t, "work-accepted")
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	var entries []Entry
	fail := false
	var before string
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if e.App == "worker" && snapshot(live) != before {
			t.Fatal("work state, number, records, task or notice escaped before append")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	ana, _ := live.Member("ana")
	sub := &pb.Submission{TenantId: live.ID, PrincipalId: ana.ID, Authority: "stock", IdempotencyKey: "create",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}
	if _, err := live.Submit(ana, sub, at); err != nil {
		t.Fatal(err)
	}
	task := live.queues["worker"][0]
	before, fail = snapshot(live), true
	live.mu.Lock()
	out := live.attempt(task, at, false)
	live.mu.Unlock()
	if out == "ok" || snapshot(live) != before || len(entries) != 1 {
		t.Fatalf("failed append changed owned work: %s", out)
	}
	fail = false
	live.mu.Lock()
	out = live.attempt(task, at, false)
	live.mu.Unlock()
	if out != "ok" || len(entries) != 2 || len(live.queues["worker"]) != 0 ||
		len(live.notices) != 2 || live.sequences.last["worker/document/0"] != 1 {
		t.Fatalf("accepted work did not apply once: %s fault=%+v", out, live.fault.Load())
	}
	saved, err := decodeAcceptedWork(entries[1].Body)
	if err != nil {
		t.Fatal(err)
	}
	batch, _, err := decodeAcceptedBatch(saved.Changes)
	if err != nil || len(batch.Rows) != 2 || len(batch.Decisions) != 1 || batch.Notices == nil {
		t.Fatalf("work omitted foreign task or notice intents: %+v %v", batch, err)
	}
	before = snapshot(live)
	if applied, err := live.applyAcceptedWork(entries[1].Body); err != nil || applied || snapshot(live) != before {
		t.Fatalf("applying the same generation changed it: %v", err)
	}
	CheckReplay(t, live, entries, func() *Tenant { return resultWorkTenant(t, live.ID) })
	recovered := resultWorkTenant(t, live.ID)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.app("worker").(*resultWorker).calls != 0 {
		t.Fatal("recovery re-executed the delivery's business code")
	}
	// Jobs have their own generations too, including a subsequent attempt.
	for _, when := range []time.Time{at.Add(time.Hour), at.Add(2 * time.Hour)} {
		before = snapshot(live)
		live.mu.Lock()
		out = live.run(live.jobs[0], when, false)
		live.mu.Unlock()
		if out != "ok" {
			t.Fatalf("job result: %s fault=%+v", out, live.fault.Load())
		}
	}
	if live.sequences.last["worker/document/0"] != 3 {
		t.Fatal("jobs did not allocate exactly the next numbers")
	}
	CheckReplay(t, live, entries, func() *Tenant { return resultWorkTenant(t, live.ID) })
}

func TestAcceptedWorkRefusalDiscardsBusinessChangesAndCommitsRetry(t *testing.T) {
	live := resultWorkTenant(t, "work-refused")
	live.app("worker").(*resultWorker).reject = true
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	ana, _ := live.Member("ana")
	_, refused := live.Submit(ana, &pb.Submission{TenantId: live.ID, PrincipalId: ana.ID, Authority: "stock",
		IdempotencyKey: "create", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
		Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1}, Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}, at)
	if refused != nil {
		t.Fatal(refused)
	}
	task := live.queues["worker"][0]
	for _, when := range []time.Time{at, at.Add(time.Second)} {
		live.mu.Lock()
		out := live.attempt(task, when, false)
		live.mu.Unlock()
		if out == "ok" || len(live.records.types["worker.document"].rows) != 0 ||
			live.sequences.last["worker/document/0"] != 0 || len(live.notices) != 0 {
			t.Fatalf("refused work exposed partial business changes: %s", out)
		}
	}
	if task.State != "failed" || task.Attempts != 2 || len(live.failed) != 1 {
		t.Fatalf("failed delivery did not retain its K9 generations: %+v", task)
	}
	CheckReplay(t, live, entries, func() *Tenant {
		// Today's implementation now succeeds, but the saved refusal does not
		// change into success during recovery.
		return resultWorkTenant(t, live.ID)
	})
}

func TestJournalAcceptedWorkCrashBeforeApplication(t *testing.T) {
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
	id := fmt.Sprintf("work-crash-%d", time.Now().UnixNano())
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, id)
	live := resultWorkTenant(t, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
	// User-selected keys cannot reserve the host's owned-work namespace.
	ana, _ := live.Member("ana")
	colliding := &pb.Submission{TenantId: id, PrincipalId: ana.ID, Authority: "worker",
		IdempotencyKey: "work:" + live.jobs[0].ID + ":1", Target: &pb.EntityRef{Type: "worker.document", Id: "denied"},
		Schema: &pb.SchemaRef{Name: "worker.document.write", Version: 1}, Payload: []byte(`{}`)}
	if _, err := live.Submit(ana, colliding, at); err == nil || journal.Position(id) != 1 {
		t.Fatalf("user refusal was not durable: %v", err)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		raw, err := journal.AppendAccepted(ctx, id, e, key, hash)
		if err == nil {
			panic("injected crash after commit, before application")
		}
		return raw, err
	}
	before := snapshot(live)
	live.mu.Lock()
	out := live.run(live.jobs[0], at, false)
	live.mu.Unlock()
	if out == "ok" || !live.quarantined() || snapshot(live) != before || journal.Position(id) != 2 {
		t.Fatalf("work crash crossed the application boundary: %s", out)
	}
	other, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	entries, err := other.Entries(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	recovered := resultWorkTenant(t, id)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.app("worker").(*resultWorker).calls != 0 ||
		recovered.sequences.last["worker/document/0"] != 1 || len(recovered.notices) != 2 {
		t.Fatal("durable work result was lost or re-executed")
	}
	CheckReplay(t, recovered, entries, func() *Tenant { return resultWorkTenant(t, id) })
}

func TestAcceptedWorkOutboundIntentCommitsWithoutDispatchOrReplanning(t *testing.T) {
	compose := func() *Tenant {
		tn := resultWorkTenant(t, "work-effect")
		tn.endpoints = []*Endpoint{{ID: "sink", Kind: "webhook", Effects: []string{"worker/publish"}}}
		return tn
	}
	live := compose()
	live.app("worker").(*resultWorker).emit = true
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	fail := true
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if len(live.outbound) != 0 {
			t.Fatal("an outbound intent escaped before append")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	before := snapshot(live)
	live.mu.Lock()
	out := live.run(live.jobs[0], at, false)
	live.mu.Unlock()
	if out == "ok" || snapshot(live) != before {
		t.Fatal("failed outbound-intent append changed state")
	}
	fail = false
	live.mu.Lock()
	out = live.run(live.jobs[0], at, false)
	live.mu.Unlock()
	if out != "ok" || len(live.outbound) != 1 || live.outbound[0].State != "pending" {
		t.Fatalf("accepted work omitted its effect: %s fault=%+v", out, live.fault.Load())
	}
	CheckReplay(t, live, entries, compose)
	recovered := compose() // today's code no longer emits, but the saved intent remains
	if err := recovered.Replay(entries); err != nil ||
		len(recovered.outbound) != 1 || recovered.outbound[0].ID != live.outbound[0].ID ||
		recovered.outbound[0].Body != live.outbound[0].Body {
		t.Fatalf("recovery re-planned or lost the outbound effect: %v", err)
	}
}
