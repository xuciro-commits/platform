package erpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"platformserver"
	"production"
)

func TestAcceptedPlannedOrdersCursorAndRowsAreAtomic(t *testing.T) {
	tn := build(t)
	var entries []platformserver.Entry
	now := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)
	gateway, _ := tn.Member("erp")
	page, _ := json.Marshal(Page{CursorTo: "p1", Orders: []Planned{
		{ID: "PO-1", Product: "P-100", Quantity: 4}, {ID: "PO-2", Product: "P-200", Quantity: 2}}})
	fail := true
	tn.AcceptResult = func(entry platformserver.Entry, _, _ string) ([]byte, error) {
		status := tn.Connectors(now)
		if len(status) != 1 || status[0].Cursor != "" {
			t.Fatal("connector cursor became visible before the result was committed")
		}
		if orders, err := tn.Read(gateway, Orders); err != nil || len(orders.([]production.Order)) != 0 {
			t.Fatalf("planned orders became visible before the commit: %v %v", orders, err)
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, refusal := tn.Input(gateway, "planned-orders", page, now); refusal == nil {
		t.Fatal("append failure accepted a connector page")
	}
	if cursor := tn.Connectors(now)[0].Cursor; cursor != "" {
		t.Fatal("append failure advanced the cursor")
	}
	fail = false
	answer, refusal := tn.Input(gateway, "planned-orders", page, now)
	if refusal != nil || answer != 2 || len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("connector page did not produce one result: %v, %v, entries=%d, health=%+v", answer, refusal, len(entries), tn.Health(now))
	}
	if cursor := tn.Connectors(now)[0].Cursor; cursor != "p1" {
		t.Fatalf("committed cursor: %s", cursor)
	}
	answer, refusal = tn.Input(gateway, "planned-orders", page, now.Add(time.Minute))
	if refusal != nil || answer != 2 || len(entries) != 1 {
		t.Fatalf("retry reran the page: %v, %v", answer, refusal)
	}
	tn.AcceptResult = func(entry platformserver.Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	page2, _ := json.Marshal(Page{CursorFrom: "p1", CursorTo: "p2", Orders: []Planned{
		{ID: "PO-1", Product: "P-100", Quantity: 6}}})
	answer, refusal = tn.Input(gateway, "planned-orders", page2, now.Add(2*time.Minute))
	if refusal != nil || answer != 1 || len(entries) != 2 || tn.Connectors(now)[0].Cursor != "p2" {
		t.Fatalf("next page did not update the record and cursor: %v %v", answer, refusal)
	}
	planner, _ := tn.Member("pat")
	answer, refusal = tn.Input(planner, "planned-orders", page2, now.Add(3*time.Minute))
	if refusal == nil || answer != nil || len(entries) != 3 {
		t.Fatalf("input refusal was not saved: %v %v", answer, refusal)
	}
	if _, again := tn.Input(planner, "planned-orders", page2, now.Add(4*time.Minute)); again == nil || len(entries) != 3 {
		t.Fatal("refused input was decided again")
	}
	restored := build(t)
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if cursor := restored.Connectors(now)[0].Cursor; cursor != "p2" {
		t.Fatalf("recovery did not restore the cursor: %s", cursor)
	}
	platformserver.CheckReplay(t, tn, entries, func() *platformserver.Tenant { return build(t) })
}

func TestJournalAcceptedPlannedOrdersCrashAndRestart(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("adapter-input-%d", time.Now().UnixNano())
	cleanup, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Close(ctx)
	defer cleanup.Exec(ctx, `delete from journal where tenant=$1`, id)
	if entries, err := journal.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new tenant journal: %v %v", entries, err)
	}
	live := buildTenant(t, id)
	member, _ := live.Member("erp")
	now := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	page, _ := json.Marshal(Page{CursorTo: "page-1", Orders: []Planned{{ID: "PO-1", Product: "P-100", Quantity: 4}}})
	live.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated crash after commit")
	}
	if _, refusal := live.Input(member, "planned-orders", page, now); refusal == nil {
		t.Fatal("interrupted answer returned success")
	}
	if live.Connectors(now)[0].Cursor != "" {
		t.Fatal("unapplied result advanced the live cursor")
	}
	reopened, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("committed connector page not recovered: %v %v", entries, err)
	}
	recovered := buildTenant(t, id)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.Connectors(now)[0].Cursor != "page-1" {
		t.Fatal("saved cursor was not applied during recovery")
	}
	recovered.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, entry, key, hash)
	}
	if answer, refusal := recovered.Input(member, "planned-orders", page, now.Add(time.Minute)); refusal != nil ||
		answer != 1 || reopened.Position(id) != 1 {
		t.Fatalf("retry did not return the saved page answer: %v %v", answer, refusal)
	}
	platformserver.CheckReplay(t, recovered, entries, func() *platformserver.Tenant { return buildTenant(t, id) })
}
