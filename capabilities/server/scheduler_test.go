package platformserver

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

// ADR-0027 10a: owned work in rounds — ordering by key, fairness between
// tenants, and quotas that defer.
func TestScheduler(t *testing.T) {
	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	build := func(id string) (*Tenant, *[]Entry) {
		dir := NewConsole(id, Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"a": "writer"}}})
		tn, err := NewTenant(id, dir, published{newNotes(id, "a")}, watcher{newNotes(id, "w")})
		if err != nil {
			t.Fatal(err)
		}
		journal := &[]Entry{}
		tn.Record = func(e Entry) { *journal = append(*journal, e) }
		return tn, journal
	}
	submit := func(tn *Tenant, key, topic, text string) {
		t.Helper()
		ana, _ := tn.app(PlatformApp).(*Console).Member("ana")
		if _, err := tn.Submit(ana, note(tn.ID, "ana", "a", key, topic, text), start); err != nil {
			t.Fatal(err)
		}
	}
	outcomes := func(tn *Tenant) string {
		var out []string
		for _, d := range tn.Deliveries() {
			out = append(out, strings.TrimPrefix(d.Target, "a.topic/")+" "+d.Outcome)
		}
		return strings.Join(out, ", ")
	}

	// Ordering by key: a failing delivery holds back the next one about the
	// same target, and no other.
	tn, _ := build("t-1")
	submit(tn, "k1", "x", "wait:z")
	submit(tn, "k2", "x", "after")
	submit(tn, "k3", "y", "other")
	tn.Work(start)
	if got := outcomes(tn); got != "x ERROR_CODE_NOT_FOUND, y ok" {
		t.Fatalf("keyed order: %s", got)
	}
	submit(tn, "k4", "z", "now")
	for s := 1; s <= 4; s++ {
		tn.Work(start.Add(time.Duration(s) * time.Second))
	}
	if got := outcomes(tn); got != "x ERROR_CODE_NOT_FOUND, y ok, z ok, x ok, x ok" {
		t.Fatalf("keyed order after the wait: %s", got)
	}

	// Fairness: a tenant with a burst of work does not keep another waiting
	// beyond one round.
	busy, busyJournal := build("busy")
	quiet, quietJournal := build("quiet")
	var order []string
	busy.Record = func(e Entry) { *busyJournal = append(*busyJournal, e); order = append(order, "busy "+e.Kind) }
	quiet.Record = func(e Entry) { *quietJournal = append(*quietJournal, e); order = append(order, "quiet "+e.Kind) }
	for i := range 50 {
		submit(busy, fmt.Sprint("b", i), fmt.Sprint("t", i), "burst")
	}
	submit(quiet, "q", "t", "one")
	order = nil
	Schedule([]*Tenant{busy, quiet}, start, 5, time.Minute)
	if i := slices.Index(order, "quiet delivery"); i < 0 || i > 5 {
		t.Fatalf("the quiet tenant's delivery came at %d of %d", i, len(order))
	}
	if n := strings.Count(outcomes(busy), " ok"); n != 50 {
		t.Fatalf("the busy tenant's work was not all done: %d", n)
	}

	// Quotas defer: three attempts a minute, the rest waits for the next
	// minute and says so; replay knows nothing of it.
	limited, journal := build("t-3")
	limited.Quota = 3
	for i := range 5 {
		submit(limited, fmt.Sprint("l", i), fmt.Sprint("t", i), "limited")
	}
	limited.Work(start)
	if n := len(limited.Deliveries()); n != 3 || !slices.Equal(limited.Deferred(start), []string{"w"}) {
		t.Fatalf("under a quota of 3: %d deliveries, deferred %v", n, limited.Deferred(start))
	}
	limited.Work(start.Add(time.Minute))
	if n := len(limited.Deliveries()); n != 5 || len(limited.Deferred(start.Add(time.Minute))) != 0 {
		t.Fatalf("the next minute: %d deliveries", n)
	}
	CheckReplay(t, limited, *journal, func() *Tenant { tn, _ := build("t-3"); return tn })
}
