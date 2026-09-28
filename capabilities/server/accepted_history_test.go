package platformserver

import (
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type resultStockCount struct{ *stock }

func (*resultStockCount) AcceptedActionSchemas() []string { return []string{"stock.item.count"} }

func TestAcceptedBatchComparesCanonicalHistoryAfterJSONNormalization(t *testing.T) {
	id := "canonical-history"
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), &resultStockCount{newStock(id)})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	ana, _ := tn.Member("ana")
	now := time.Date(2026, 9, 28, 22, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	create := &pb.Submission{TenantId: id, PrincipalId: ana.ID, Authority: "stock", IdempotencyKey: "create",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}
	if _, err := tn.Submit(ana, create, now); err != nil {
		t.Fatal(err)
	}
	row := tn.records.types["stock.item"].rows["I1"]
	if len(row.history) == 0 || len(row.history[0].Fields) == 0 {
		t.Fatal("missing first history")
	}
	// PostgreSQL JSONB and json.RawMessage marshal may retain different byte
	// formatting for the same historic field change; semantic bytes stay equal.
	before := slices.Clone(row.history[0].Fields[0].After)
	row.history[0].Fields[0].After = append(append([]byte{' '}, before...), ' ')
	ana.Roles["stock"] = "line"
	count := &pb.Submission{TenantId: id, PrincipalId: ana.ID, Authority: "stock", IdempotencyKey: "count",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.count", Version: 1},
		Payload: []byte(`{"qty":4}`)}
	if _, err := tn.Submit(ana, count, now.Add(time.Second)); err != nil || tn.quarantined() {
		t.Fatalf("JSON normalization rejected the same prior history: %v; fault=%+v", err, tn.fault.Load())
	}
	if len(entries) != 2 {
		t.Fatalf("expected two accepted inputs, got %d", len(entries))
	}
	if result, _, err := decodeAcceptedBatch(entries[1].Body); err != nil || len(result.Rows) != 1 {
		t.Fatalf("count did not save its canonical batch: %+v, %v", result, err)
	}
}

func TestAcceptedBatchReplaysPostgresTimestampPrecision(t *testing.T) {
	id := "journal-clock"
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}
		tn, err := NewTenant(id, NewConsole(id, seat), &resultStockCount{newStock(id)})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	member, _ := tn.Member("ana")
	now := time.Date(2026, 9, 28, 22, 0, 0, 123456789, time.UTC)
	var entry Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entry = e
		return e.Body, nil
	}
	sub := &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: "stock", IdempotencyKey: "create",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}
	if _, err := tn.Submit(member, sub, now); err != nil {
		t.Fatal(err)
	}
	entry.At = entry.At.Truncate(time.Microsecond) // PostgreSQL timestamptz round trip
	recovered := build()
	if err := recovered.Replay([]Entry{entry}); err != nil {
		t.Fatalf("valid saved result could not replay from the full journal: %v", err)
	}
	entry.At = entry.At.Add(time.Microsecond)
	if err := build().Replay([]Entry{entry}); err == nil {
		t.Fatal("a different durable journal timestamp was accepted")
	}
}
