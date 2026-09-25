package sales

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

// ADR-0019 D7: a large tenant restarts from a snapshot in seconds, and
// decisions barely wait while one is taken. Slow and memory-hungry, so on
// request: PLATFORM_SCALE=1000000 go test -run TestSnapshotAtScale -v -timeout 20m
// (measured on 2026-09-25: full replay 22 s, restore 5.5 s, a decision waited
// at most 1 s while an 746 MB snapshot was taken).
func TestSnapshotAtScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("PLATFORM_SCALE"))
	if n == 0 {
		t.Skip("PLATFORM_SCALE not set")
	}
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		tn, err := NewTenant("hotel-a", nil, seats...)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	sales := newWorld(t).members["sales"]
	submit := func(key, schema, typ, id string, p any) {
		raw, _ := json.Marshal(p)
		if _, err := tn.Submit(sales, &pb.Submission{TenantId: "hotel-a", PrincipalId: sales.ID, Authority: "crm-server", IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	submit("a", "crm.account.create", "crm.account", "ACME", map[string]string{"name": "Acme", "kind": "company"})
	for i := range n {
		submit(fmt.Sprint("o", i), "crm.opportunity.open", "crm.opportunity", fmt.Sprint("O", i), map[string]string{"account": "ACME", "title": "deal"})
	}
	started := time.Now()
	if err := build().Replay(journal); err != nil {
		t.Fatal(err)
	}
	t.Logf("full replay of %d entries: %v", len(journal), time.Since(started))
	var slowest time.Duration
	done := make(chan bool)
	go func() { // decisions keep coming while the snapshot is taken
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			began := time.Now()
			submit(fmt.Sprint("during", i), "crm.opportunity.open", "crm.opportunity", fmt.Sprint("D", i), map[string]string{"account": "ACME", "title": "deal"})
			slowest = max(slowest, time.Since(began))
			time.Sleep(5 * time.Millisecond)
		}
	}()
	time.Sleep(20 * time.Millisecond)
	started = time.Now()
	state, _, err := tn.Snapshot(func() int64 { return int64(len(journal)) })
	done <- true
	t.Logf("slowest decision while the snapshot was taken: %v", slowest)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot: %d MB in %v", len(state)>>20, time.Since(started))
	started = time.Now()
	if err := build().Restore(state); err != nil {
		t.Fatal(err)
	}
	t.Logf("restore: %v", time.Since(started))
}
