package platform

import (
	"fmt"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// TestRecordsForRaceWithReceive verifies that reading tenant change records via
// RecordsFor while concurrent decisions append via Receive is completely safe under -race.
func TestRecordsForRaceWithReceive(t *testing.T) {
	catalog := NewCatalog(Action{Schema: "test.edit", Target: "test", Title: "Edit", Roles: []string{"admin"}})
	ledger := NewLedger("t-1", "test", catalog, "test")
	caller := Caller{Member: Member{ID: "admin", Roles: map[string]string{"test": "admin"}}, App: "test"}

	var wg sync.WaitGroup
	const iterations = 500
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := range iterations {
			records := ledger.RecordsFor("t-1")
			_ = len(records)
			if i%10 == 0 {
				_ = ledger.RecordsFor("other-tenant")
			}
		}
	}()

	go func() {
		defer wg.Done()
		now := time.Now()
		for i := range iterations {
			sub := &pb.Submission{
				TenantId:       "t-1",
				PrincipalId:    "admin",
				Authority:      "test",
				IdempotencyKey: fmt.Sprintf("k-%d", i),
				Target:         &pb.EntityRef{Type: "test", Id: "1"},
				Schema:         &pb.SchemaRef{Name: "test.edit", Version: 1},
				Payload:        []byte("{}"),
			}
			_, _ = ledger.Receive(caller, sub, now.Add(time.Duration(i)*time.Millisecond), nil, nil)
		}
	}()

	wg.Wait()
	if len(ledger.RecordsFor("t-1")) != iterations {
		t.Fatalf("expected %d records, got %d", iterations, len(ledger.RecordsFor("t-1")))
	}
}
