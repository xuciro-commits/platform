package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	acceptedformat "platformserver/kernel/acceptedformat"

	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
	"platformserver/studio/build"
)

type workDocument struct {
	platform.Record
	Number string `json:"number"`
}

type resultWorker struct {
	ledger *platform.Ledger
	calls  int
	polls  int
	reject bool
	emit   bool
	idle   bool
	nested bool
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
func (w *resultWorker) Snapshot() (json.RawMessage, error)       { return w.ledger.Snapshot() }
func (w *resultWorker) Restore(raw json.RawMessage) error        { return w.ledger.Restore(raw) }
func (w *resultWorker) Submit(c platform.Caller, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return w.ledger.Receive(c, sub, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if w.nested {
			_, err := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
				c.Notify(platform.Notification{Title: "Ready", Key: "ready:" + sub.GetTarget().GetId()}, at, platform.Recipient{Member: "ana"})
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
		}
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
	w.polls++
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
	tn, err := NewTenant(id, newConsole(id, platform.Seat{Subjects: []string{"ana"},
		Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}),
		newStock(id), newResultWorker(id), work.New(id))
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

type stateOnlyJob struct{ *statefulStock }

func (s *stateOnlyJob) Manifest() platform.Manifest {
	m := s.statefulStock.Manifest()
	m.Jobs = []platform.Job{{Name: "state", Title: "Update private state", Every: time.Hour}}
	return m
}

func (s *stateOnlyJob) ForkAcceptedState() (platform.App, error) {
	copy := *s.statefulStock
	return &stateOnlyJob{&copy}, nil
}

func (s *stateOnlyJob) Run(platform.Caller, string, time.Time) *kernel.Error {
	s.accepted++
	return nil
}

func TestAcceptedWorkKeepsStateOnlyJobResult(t *testing.T) {
	const id = "state-only-job"
	compose := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "stock:clerk")}, &stateOnlyJob{&statefulStock{stock: newStock(id)}})
	}
	live := compose()
	owner := live.app("stock").(*stateOnlyJob)
	var entries []Entry
	fail := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if owner.accepted != 0 {
			t.Fatal("private job state escaped before append")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	live.Work(at)
	if owner.accepted != 0 || len(entries) != 0 {
		t.Fatal("failed append applied private job state")
	}
	fail = false
	live.Work(at.Add(time.Hour))
	if owner.accepted != 1 || len(entries) != 1 {
		t.Fatalf("state-only job was discarded as idle: state=%d entries=%d", owner.accepted, len(entries))
	}
	saved, err := decodeAcceptedWork(entries[0].Body)
	if err != nil || len(saved.Changes) != 0 || len(saved.States) != 1 {
		t.Fatalf("state-only work result differs: %+v %v", saved.States, err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil || recovered.app("stock").(*stateOnlyJob).accepted != 1 {
		t.Fatalf("state-only job did not recover: %v", err)
	}
	CheckReplay(t, live, entries, compose)
}

func TestAcceptedWorkLegacyJobPredecessorAndNewApplicationBinding(t *testing.T) {
	newTenant := func() *Tenant {
		tn := resultWorkTenant(t, "legacy-job-binding")
		return tn
	}
	live := newTenant()
	job := live.work.Jobs[0]
	if len(job.Applications) != 0 {
		t.Fatal("startup rewrote the historical job predecessor")
	}
	var entries []Entry
	legacy := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if legacy {
			result, err := decodeAcceptedWork(e.Body)
			if err != nil {
				return nil, err
			}
			// An older writer had no application field. Its original digest
			// and empty polling result still pass recovery unchanged.
			result.After.Applications = nil
			result.Changes, result.Notices, result.Intents = nil, nil, nil
			result.States, result.Control, result.Unique = nil, nil, nil
			result.Digest, err = digestAcceptedWork(result)
			if err != nil {
				return nil, err
			}
			e.Body, err = json.Marshal(result)
			if err != nil {
				return nil, err
			}
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	for i := range 2 {
		live.mu.Lock()
		out := live.run(job, at.Add(time.Duration(i)*time.Hour), false)
		live.mu.Unlock()
		if out != "ok" {
			t.Fatalf("job generation %d: %s", i, out)
		}
		legacy = false
	}
	first, err := decodeAcceptedWork(entries[0].Body)
	if err != nil || len(first.Input.Applications) != 0 || len(first.After.Applications) != 0 {
		t.Fatalf("legacy job image changed: %v", err)
	}
	second, err := decodeAcceptedWork(entries[1].Body)
	if err != nil || len(second.Input.Applications) != 0 || len(second.After.Applications) == 0 {
		t.Fatalf("new labels did not join the accepted result: %v", err)
	}
	CheckReplay(t, live, entries, newTenant)
	wrong := newTenant()
	wrong.work.Jobs[0].Applications = second.After.Applications
	if _, err := wrong.applyAcceptedWork(entries[0].Body); err == nil {
		t.Fatal("a different job predecessor bypassed recovery validation")
	}
}

func TestAcceptedIdleWorkDoesNotWriteWakeOrAdvanceHistory(t *testing.T) {
	live := resultWorkTenant(t, "idle-work")
	worker := live.app("worker").(*resultWorker)
	worker.idle = true
	live.AcceptResult = func(Entry, string, string) ([]byte, error) {
		t.Fatal("idle job reached the journal")
		return nil, nil
	}
	before := snapshot(live)
	seq, data, wake := live.changeState()
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	live.Work(at)
	live.Work(at.Add(time.Hour - time.Nanosecond))
	if worker.polls != 1 {
		t.Fatalf("idle job polled again before its interval: %d", worker.polls)
	}
	live.Work(at.Add(time.Hour))
	after, nextData, _ := live.changeState()
	if worker.polls != 2 || after != seq || nextData != data || snapshot(live) != before {
		t.Fatalf("idle work changed accepted state: polls=%d seq=%d/%d data=%d/%d", worker.polls, seq, after, data, nextData)
	}
	select {
	case <-wake:
		t.Fatal("idle job woke change followers")
	default:
	}
	if job := live.work.All(live.apps)[0]; !job.Due.Equal(at.Add(2*time.Hour)) || job.Attempts != 0 || !job.Last.IsZero() {
		t.Fatalf("idle polling was not shown separately from accepted attempts: %+v", job)
	}
	if len(live.works.All()) != 0 {
		t.Fatal("idle polling created a K9 generation")
	}
	retryAt := at.Add(time.Hour + time.Minute)
	live.mu.Lock()
	retried := live.retry(live.work.Jobs[0].ID, retryAt)
	live.mu.Unlock()
	live.Work(retryAt)
	if !retried || worker.polls != 3 {
		t.Fatal("manual retry did not clear the transient polling deadline")
	}
}

func TestAcceptedWorkReplaysAfterIdlePollsAndFailedAppend(t *testing.T) {
	live := resultWorkTenant(t, "idle-replay")
	worker := live.app("worker").(*resultWorker)
	worker.idle = true
	var entries []Entry
	fail := false
	attempts := 0
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		attempts++
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	live.Work(at)
	worker.idle, fail = false, true
	live.Work(at.Add(time.Hour))
	if attempts != 1 || len(entries) != 0 || live.work.Jobs[0].Attempts != 0 || live.quarantined() {
		t.Fatalf("failed append retried immediately or advanced history: attempts=%d task=%+v", attempts, live.work.Jobs[0])
	}
	fail = false
	live.Work(at.Add(2 * time.Hour))
	worker.idle = true
	live.Work(at.Add(3 * time.Hour))
	worker.idle = false
	live.Work(at.Add(4 * time.Hour))
	if attempts != 3 || live.work.Jobs[0].Attempts != 2 {
		t.Fatalf("accepted generations counted empty or failed polls: attempts=%d task=%+v", attempts, live.work.Jobs[0])
	}
	CheckReplay(t, live, entries, func() *Tenant { return resultWorkTenant(t, live.ID) })
	// Restoring a snapshot drops transient deadlines, but retains the exact
	// accepted predecessor for the next journaled result.
	restored := resultWorkTenant(t, live.ID)
	saved, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	last := len(entries)
	live.Work(at.Add(5 * time.Hour))
	if err := restored.Replay(entries[last:]); err != nil || snapshot(restored) != snapshot(live) {
		t.Fatalf("snapshot plus accepted suffix differs after idle polling: %v", err)
	}
}

func TestAcceptedPlatformJobsDoNotJournalEmptyPolls(t *testing.T) {
	id := "idle-platform-jobs"
	live := composeTenant(t, id, []platform.Seat{seatOf("ana")}, build.New(id), automation.New(id), work.New(id))
	if len(live.work.Jobs) != 6 {
		t.Fatalf("expected all platform jobs, got %d", len(live.work.Jobs))
	}
	live.AcceptResult = func(Entry, string, string) ([]byte, error) {
		t.Fatal("empty platform job reached the journal")
		return nil, nil
	}
	before := snapshot(live)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for i := range 3 {
		live.Work(at.Add(time.Duration(i) * time.Second))
	}
	if live.quarantined() || snapshot(live) != before {
		t.Fatal("empty platform jobs changed accepted state")
	}
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
	task := live.work.Queues["worker"][0]
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
	if out != "ok" || len(entries) != 2 || len(live.work.Queues["worker"]) != 0 ||
		len(noticeValues(live)) != 2 || live.sequences.Clone()["worker/document/0"] != 1 {
		t.Fatalf("accepted work did not apply once: %s fault=%+v", out, live.fault.Load())
	}
	saved, err := decodeAcceptedWork(entries[1].Body)
	if err != nil {
		t.Fatal(err)
	}
	batch, _, err := acceptedformat.DecodeBatch(saved.Changes)
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
		out = live.run(live.work.Jobs[0], when, false)
		live.mu.Unlock()
		if out != "ok" {
			t.Fatalf("job result: %s fault=%+v", out, live.fault.Load())
		}
	}
	if live.sequences.Clone()["worker/document/0"] != 3 {
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
	task := live.work.Queues["worker"][0]
	for _, when := range []time.Time{at, at.Add(time.Second)} {
		live.mu.Lock()
		out := live.attempt(task, when, false)
		live.mu.Unlock()
		if out == "ok" || live.records.types["worker.document"].rows.Len() != 0 ||
			live.sequences.Clone()["worker/document/0"] != 0 || len(noticeValues(live)) != 0 {
			t.Fatalf("refused work exposed partial business changes: %s", out)
		}
	}
	if task.State != "failed" || task.Attempts != 2 || len(live.work.Failed) != 1 {
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
		IdempotencyKey: "work:" + live.work.Jobs[0].ID + ":1", Target: &pb.EntityRef{Type: "worker.document", Id: "denied"},
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
	out := live.run(live.work.Jobs[0], at, false)
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
		recovered.sequences.Clone()["worker/document/0"] != 1 || len(noticeValues(recovered)) != 2 {
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
	out := live.run(live.work.Jobs[0], at, false)
	live.mu.Unlock()
	if out == "ok" || snapshot(live) != before {
		t.Fatal("failed outbound-intent append changed state")
	}
	fail = false
	live.mu.Lock()
	out = live.run(live.work.Jobs[0], at, false)
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

func TestAcceptedWorkerDirectLedgerKeepsParentAcrossSavepoint(t *testing.T) {
	live := resultWorkTenant(t, "worker-parent-ledger")
	live.app("worker").(*resultWorker).nested = true
	at := time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I", map[string]any{"name": "Bolt", "qty": 2, "line": "L1"}, at)
	task := live.work.Queues["worker"][0]
	live.mu.Lock()
	outcome := live.attempt(task, at, false)
	live.mu.Unlock()
	if outcome != "ok" || len(entries) != 2 || len(noticeValues(live)) != 3 {
		t.Fatalf("parent ledger lost its receipt after a child savepoint: %s fault=%v notices=%d entries=%d", outcome, live.fault.Load(), len(noticeValues(live)), len(entries))
	}
	saved, err := decodeAcceptedWork(entries[1].Body)
	if err != nil {
		t.Fatal(err)
	}
	batch, _, err := acceptedformat.DecodeBatch(saved.Changes)
	if err != nil || len(batch.Decisions) != 1 {
		t.Fatalf("parent worker decision not sealed: %v", err)
	}
	CheckReplay(t, live, entries, func() *Tenant { return resultWorkTenant(t, live.ID) })
}

func TestAcceptedWorkCompletionPreservesChildCancellation(t *testing.T) {
	const id = "work-child-cancellation"
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tn := resultWorkTenant(t, id)
	operationID := id + ":operation:other"
	generation, _, refusal := tn.works.Start(operationID, "engine")
	if refusal != nil {
		t.Fatal(refusal)
	}
	tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{ID: operationID, App: "worker",
		Endpoint: acceptedformat.OperationEndpoint, State: "running", Generation: generation, At: at, Due: at}})
	var saved []byte
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		saved = entry.Body
		return nil, errors.New("leave the accepted input unapplied")
	}
	_ = tn.acceptWork(tn.work.Jobs[0], at)
	result, err := decodeAcceptedWork(saved)
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := acceptedformat.DecodeBatch(result.Changes)
	if err != nil {
		t.Fatal(err)
	}
	cancel, err := tn.operationCancellation(operationID)
	if err != nil {
		t.Fatal(err)
	}
	child.Operations = append(child.Operations, cancel)
	child.Digest, _ = acceptedformat.DigestBatch(child)
	result.Changes, _ = json.Marshal(child)
	result.Digest, _ = digestAcceptedWork(result)
	raw, _ := json.Marshal(result)
	if applied, err := tn.applyAcceptedWork(raw); err != nil || !applied {
		t.Fatalf("work with a cancelling child: %v (applied=%t)", err, applied)
	}
	other, refusal := tn.works.Get(operationID)
	if refusal != nil || other.GetState() != pb.WorkState_WORK_STATE_CANCELLED || other.GetGeneration() != generation {
		t.Fatalf("work completion replaced its child's cancellation: %v %v", other, refusal)
	}
	finished, refusal := tn.works.Get(result.Input.ID)
	if refusal != nil || finished.GetState() != pb.WorkState_WORK_STATE_COMPLETED {
		t.Fatalf("parent work completion was lost: %v %v", finished, refusal)
	}
}
