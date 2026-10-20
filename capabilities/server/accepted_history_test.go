package platformserver

import (
	"encoding/json"
	"platformserver/internal/acceptance"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/records"
	"platformserver/platform"
)

type resultStockCount struct{ *stock }

func TestAcceptedBatchComparesCanonicalHistoryAfterJSONNormalization(t *testing.T) {
	id := "canonical-history"
	build := func() *Tenant {
		seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}, Grants: []platform.Grant{{App: "stock", Role: "clerk"}, {App: "stock", Role: "line"}}}}
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
	row := tn.records.types["stock.item"].rows.Get("I1")
	if len(row.History) == 0 || len(row.History[0].Fields) == 0 {
		t.Fatal("missing first history")
	}
	// PostgreSQL JSONB and json.RawMessage marshal may retain different byte
	// formatting for the same historic field change; semantic bytes stay equal.
	before := slices.Clone(row.History[0].Fields[0].After)
	row.History[0].Fields[0].After = append(append([]byte{' '}, before...), ' ')
	legacyPrior, encodeErr := records.FullImage("stock.item", "I1", row)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	legacyBefore, encodeErr := acceptance.Digest(legacyPrior)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	count := &pb.Submission{TenantId: id, PrincipalId: ana.ID, Authority: "stock", IdempotencyKey: "count",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.count", Version: 1},
		Payload: []byte(`{"qty":4}`)}
	if _, err := tn.Submit(ana, count, now.Add(time.Second)); err != nil || tn.quarantined() {
		t.Fatalf("JSON normalization rejected the same prior history: %v; fault=%+v", err, tn.fault.Load())
	}
	if len(entries) != 2 {
		t.Fatalf("expected two accepted inputs, got %d", len(entries))
	}
	result, _, err := decodeAcceptedBatch(entries[1].Body)
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("count did not save its canonical batch: %+v, %v", result, err)
	}
	if result.Version != 8 || result.Rows[0].HistoryRef == nil || result.Rows[0].HistoryRef.Base != 1 || result.Rows[0].HistoryBase != 1 || len(result.Rows[0].History) != 1 || tn.records.types["stock.item"].rows.Get("I1").HistoryLength() != 2 {
		t.Fatal("a new result must append one change without discarding its history")
	}
	encode := func(batch acceptedBatch) []byte {
		t.Helper()
		batch.Digest, err = digestAcceptedBatch(batch)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	legacy := result
	legacy.Unique = nil
	legacy.Rows = slices.Clone(result.Rows)
	legacy.Version, legacy.Rows[0].HistoryBase = 1, 0
	legacy.Rows[0].Format, legacy.Rows[0].Predecessor, legacy.Rows[0].HistoryRef = "", "", nil
	legacy.Rows[0].Before = legacyBefore
	legacy.Rows[0].History = records.CopyHistory(tn.records.types["stock.item"].rows.Get("I1").RecordHistory())
	legacySecond := entries[1]
	legacySecond.Body = encode(legacy)
	legacySuffix := legacy
	legacySuffix.Version = 2
	legacySuffix.Rows = slices.Clone(legacy.Rows)
	legacySuffix.Rows[0].HistoryBase = 1
	legacySuffix.Rows[0].History = records.CopyHistory(result.Rows[0].History)
	legacySuffixSecond := entries[1]
	legacySuffixSecond.Body = encode(legacySuffix)
	next := proto.Clone(count).(*pb.Submission)
	next.IdempotencyKey, next.Payload = "count-again", []byte(`{"qty":6}`)
	if _, err := tn.Submit(ana, next, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	recovered := build()
	if err := recovered.Replay([]Entry{entries[0], legacySecond, entries[2]}); err != nil || recovered.records.types["stock.item"].rows.Get("I1").HistoryLength() != 3 {
		t.Fatalf("old history followed by a new suffix failed to recover: %v", err)
	}
	recoveredSuffix := build()
	if err := recoveredSuffix.Replay([]Entry{entries[0], legacySuffixSecond, entries[2]}); err != nil || recoveredSuffix.records.types["stock.item"].rows.Get("I1").HistoryLength() != 3 {
		t.Fatalf("old suffix followed by a new suffix failed to recover: %v", err)
	}
	result, _, err = decodeAcceptedBatch(entries[2].Body)
	if err != nil {
		t.Fatal(err)
	}
	result.Rows[0].HistoryBase = 3
	ref := *result.Rows[0].HistoryRef
	result.Rows[0].HistoryRef = &ref
	ref.Base = 3
	ref.Head = ref.Prior
	for i, change := range result.Rows[0].History {
		ref.Head, err = records.AppendHistory(ref.Head, ref.Base+i, jsonOf(change))
		if err != nil {
			t.Fatal(err)
		}
	}
	wrong := entries[2]
	wrong.Body = encode(result)
	refused := build()
	if err := refused.Replay([]Entry{entries[0], legacySecond, wrong}); err == nil || refused.records.types["stock.item"].rows.Get("I1").HistoryLength() != 2 {
		t.Fatal("an incorrect history predecessor was accepted or partly applied")
	}
	if err := build().Replay([]Entry{entries[0], legacySecond}); err != nil {
		t.Fatalf("old complete-history results no longer recover: %v", err)
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
