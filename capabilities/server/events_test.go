package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// watcher logs a's notes by writing its own note for each. A note "fail" is
// refused every time; "wait:<topic>" is refused until a has that topic.
type watcher struct{ *notes }

func (w watcher) Manifest() Manifest {
	m := w.notes.Manifest()
	m.Subscribes, m.Requires = []string{"a.note"}, []string{"a"}
	return m
}

func (w watcher) Handle(c Caller, e Event) *kernel.Error {
	text := string(e.Record.GetSubmission().GetPayload())
	if text == "fail" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if topic, ok := strings.CutPrefix(text, "wait:"); ok {
		seen, _ := c.Read("a", "a-notes")
		if _, ok := seen.(map[string]string)[topic]; !ok {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
	}
	return w.notes.write(c, "seen:"+e.Record.GetChangeId(), "saw "+text)
}

func (n *notes) write(c Caller, key, text string) *kernel.Error {
	_, err := n.Submit(c, note(c.Tenant, c.ID, n.id, key, "log", text), time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC))
	return err
}

func TestEventsAreOwnedWork(t *testing.T) {
	if _, err := NewTenant("t", watcher{newNotes("t", "w", "")}); err == nil {
		t.Fatal("a subscription to an app that is not required was accepted")
	}
	var journal []Entry
	build := func() (*Tenant, *notes) {
		dir := NewDirectory("t-1", Seat{Subjects: []string{"ana"}, Member: Member{ID: "ana", Roles: map[string]string{"a": "writer", PlatformApp: Admin}}})
		w := newNotes("t-1", "w", "")
		tn, err := NewTenant("t-1", dir, newNotes("t-1", "a", ""), watcher{w})
		if err != nil {
			t.Fatal(err)
		}
		return tn, w
	}
	tn, w := build()
	tn.Record = func(e Entry) {
		raw, _ := json.Marshal(e) // stored as JSON, as the PostgreSQL journal does
		var stored Entry
		json.Unmarshal(raw, &stored)
		journal = append(journal, stored)
	}
	ana, _ := tn.app(PlatformApp).(*Directory).Member("ana")
	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	submit := func(key, topic, text string, s int) {
		t.Helper()
		if _, err := tn.Submit(ana, note("t-1", "ana", "a", key, topic, text), at(s)); err != nil {
			t.Fatalf("%s: the event's decision must stand whatever the subscriber does: %v", text, err)
		}
	}
	submit("k1", "x1", "one", 0)
	submit("k2", "x2", "wait:y", 0)
	if len(w.texts) != 0 {
		t.Fatal("the handler ran inside the input")
	}
	tn.Work(at(0)) // one: ok; wait:y: refused, retried in 2 s
	submit("k3", "x3", "fail", 1)
	submit("k4", "x4", "two", 1)
	tn.Work(at(1)) // nothing due: the queue keeps its order behind wait:y
	if w.texts["log"] != "saw one" {
		t.Fatalf("log %q", w.texts["log"])
	}
	submit("k5", "y", "why", 1)
	for s := 2; s <= 40; s++ {
		tn.Work(at(s))
	}
	if w.texts["log"] != "saw why" || len(w.ledger.Changes.Records("t-1")) != 4 {
		t.Fatalf("watcher wrote %v (%d records)", w.texts, len(w.ledger.Changes.Records("t-1")))
	}
	outcomes := func(tn *Tenant) []string {
		out := []string{}
		for _, d := range tn.Deliveries() {
			out = append(out, strings.TrimPrefix(d.Target, "a.topic/")+" "+d.Outcome)
		}
		return out
	}
	want := []string{"x1 ok", "x2 ERROR_CODE_NOT_FOUND", "x2 ok", "x3 ERROR_CODE_CONFLICT", "x3 ERROR_CODE_CONFLICT", "x3 ERROR_CODE_CONFLICT",
		"x3 ERROR_CODE_CONFLICT", "x3 ERROR_CODE_CONFLICT", "x4 ok", "y ok"}
	if got := outcomes(tn); !slices.Equal(got, want) {
		t.Fatalf("deliveries %v", got)
	}
	failed := tn.Tasks()
	if len(failed) != 1 || failed[0].State != "failed" || failed[0].Attempts != maxAttempts || failed[0].Error != "ERROR_CODE_CONFLICT" {
		t.Fatalf("work %+v", failed)
	}
	if r := w.ledger.Changes.Records("t-1")[0]; r.GetSubmission().GetPrincipalId() != "app:w" {
		t.Fatalf("the handler's decision is attributed to %s", r.GetSubmission().GetPrincipalId())
	}
	// An administrator retries the failed delivery: a full schedule of attempts again.
	retry := &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: PlatformApp, IdempotencyKey: "r1",
		Target: &pb.EntityRef{Type: WorkType, Id: failed[0].ID}, Schema: &pb.SchemaRef{Name: SchemaWorkRetry, Version: 1}, Payload: []byte("{}")}
	if _, err := tn.Submit(ana, retry, at(50)); err != nil {
		t.Fatal(err)
	}
	tn.Work(at(50))
	if got := tn.Tasks(); len(got) != 1 || got[0].Attempts != maxAttempts+1 || got[0].State != "retrying" {
		t.Fatalf("after retry %+v", got)
	}
	for s := 51; s <= 90; s++ {
		tn.Work(at(s))
	}
	if got := tn.Tasks(); len(got) != 1 || got[0].Attempts != 2*maxAttempts || got[0].State != "failed" {
		t.Fatalf("after the retried schedule %+v", got)
	}
	// The journal holds the inputs and every attempt; a replay rebuilds the
	// handlers' decisions, the deliveries and the owned work.
	again, w2 := build()
	if err := again.Replay(journal); err != nil {
		t.Fatal(err)
	}
	if w2.texts["log"] != "saw why" || !slices.Equal(outcomes(again), outcomes(tn)) || len(again.Tasks()) != 1 || again.Tasks()[0].Attempts != 2*maxAttempts {
		t.Fatalf("replay: %v, %v, %+v", w2.texts, outcomes(again), again.Tasks())
	}
	CheckReplay(t, tn, journal, func() *Tenant { tn, _ := build(); return tn })
	// A replay whose handler ends differently than recorded is refused.
	tampered := slices.Clone(journal)
	for i, e := range tampered {
		if e.Kind == "delivery" {
			tampered[i].Body = json.RawMessage(strings.Replace(string(e.Body), `"ok"`, `"ERROR_CODE_CONFLICT"`, 1))
			break
		}
	}
	if third, _ := build(); third.Replay(tampered) == nil {
		t.Fatal("a replay that disagrees with the recorded outcome was accepted")
	}
	// Reads are for members with a role in the app.
	if _, err := tn.Read(ana, "w-notes"); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("ana has no role in w but read it: %v", err)
	}
}

// echo answers each of its notes with another: a subscription cycle.
type echo struct{ *notes }

func (e echo) Manifest() Manifest {
	m := e.notes.Manifest()
	m.Subscribes = []string{"e.note"}
	return m
}

func (e echo) Handle(c Caller, ev Event) *kernel.Error {
	return e.notes.write(c, "echo:"+ev.Record.GetChangeId(), "again")
}

func TestSubscriptionCycleStops(t *testing.T) {
	dir := NewDirectory("t-1", Seat{Subjects: []string{"ana"}, Member: Member{ID: "ana", Roles: map[string]string{"e": "writer"}}})
	tn, err := NewTenant("t-1", dir, echo{newNotes("t-1", "e", "")})
	if err != nil {
		t.Fatal(err)
	}
	ana, _ := dir.Member("ana")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	if _, err := tn.Submit(ana, note("t-1", "ana", "e", "k", "x", "go"), now); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tn.Work(now)
	}
	d := tn.Deliveries()
	if len(d) != maxHops+2 || !strings.HasPrefix(d[len(d)-1].Outcome, "stopped") || len(tn.Tasks()) != 0 {
		t.Fatalf("%d deliveries, last %+v, work %v", len(d), d[len(d)-1], tn.Tasks())
	}
}
