package platformserver

import (
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// watcher counts a's notes by writing its own note for each; a note "fail" is refused.
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
	return w.notes.write(c, "seen:"+e.Record.GetChangeId(), "saw "+text)
}

func (n *notes) write(c Caller, key, text string) *kernel.Error {
	_, err := n.Submit(c, note(c.Tenant, c.ID, n.id, key, "log", text), time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC))
	return err
}

func TestEventsAfterCommit(t *testing.T) {
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
	tn.Record = func(e Entry) { journal = append(journal, e) }
	ana, _ := tn.app(PlatformApp).(*Directory).Member("ana")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for i, text := range []string{"one", "fail", "two"} {
		if _, err := tn.Submit(ana, note("t-1", "ana", "a", string(rune('k'+i)), "x", text), now); err != nil {
			t.Fatalf("%s: the event's decision must stand whatever the subscriber does: %v", text, err)
		}
	}
	if w.texts["log"] != "saw two" || len(w.ledger.Changes.Records("t-1")) != 2 {
		t.Fatalf("watcher wrote %v (%d records)", w.texts, len(w.ledger.Changes.Records("t-1")))
	}
	outcomes := []string{}
	for _, d := range tn.Deliveries() {
		outcomes = append(outcomes, d.Subscriber+" "+d.Outcome)
	}
	if !slices.Equal(outcomes, []string{"w ok", "w ERROR_CODE_CONFLICT", "w ok"}) {
		t.Fatalf("deliveries %v", outcomes)
	}
	if r := w.ledger.Changes.Records("t-1")[0]; r.GetSubmission().GetPrincipalId() != "app:w" {
		t.Fatalf("the handler's decision is attributed to %s", r.GetSubmission().GetPrincipalId())
	}
	// Only the three inputs are journaled; a replay rebuilds what the handler decided.
	again, w2 := build()
	if err := again.Replay(journal); err != nil || len(journal) != 3 || w2.texts["log"] != "saw two" || len(again.Deliveries()) != 3 {
		t.Fatalf("replay: %v, %d entries, %v, %d deliveries", err, len(journal), w2.texts, len(again.Deliveries()))
	}
	// Reads are for members with a role in the app.
	if _, err := tn.Read(ana, "w-notes"); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("ana has no role in w but read it: %v", err)
	}
	if _, err := tn.Read(ana, "a-notes"); err != nil {
		t.Fatal(err)
	}
}
