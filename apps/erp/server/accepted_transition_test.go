package erp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

// Callback-free lifecycle transitions share the same commit-before-apply
// boundary as generated record actions. In particular, closing an accounting
// period must not become visible if the journal rejects the result.
func TestAcceptedPeriodTransitionCommitRetryAndRecovery(t *testing.T) {
	live := build(t)
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	controller, _ := live.Member("cy")
	var entries []platformserver.Entry
	fail := false
	live.Record = func(e platformserver.Entry) { entries = append(entries, e) }
	live.AcceptResult = func(e platformserver.Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(key, schema string) *pb.ChangeRecord {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{})
		r, refused := live.Submit(controller, &pb.Submission{
			TenantId: "t", PrincipalId: controller.ID, Authority: ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: PeriodType, Id: "2026-10"},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: payload,
		}, now)
		if refused != nil {
			t.Fatalf("%s: %v", schema, refused)
		}
		return r
	}
	submit("create", SchemaPeriodOpen)
	fail = true
	_, refused := live.Submit(controller, &pb.Submission{
		TenantId: "t", PrincipalId: controller.ID, Authority: ID, IdempotencyKey: "close",
		Target: &pb.EntityRef{Type: PeriodType, Id: "2026-10"},
		Schema: &pb.SchemaRef{Name: PeriodType + ".close", Version: 1}, Payload: []byte(`{}`),
	}, now)
	if refused == nil || len(entries) != 1 {
		t.Fatalf("failed close escaped the commit boundary: %v, %d entries", refused, len(entries))
	}
	period, err := live.RecordOf(controller, PeriodType, "2026-10", now)
	if err != nil || period.Record.(Period).State != "open" {
		t.Fatalf("failed close changed the period: %+v, %v", period, err)
	}
	fail = false
	close := submit("close", PeriodType+".close")
	if close.GetRevision() != 2 || len(entries) != 2 || entries[1].Kind != "accepted-result" {
		t.Fatalf("close was not committed as one result: %+v, %d entries", close, len(entries))
	}
	var result struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal(entries[1].Body, &result); err != nil ||
		result.Version != 4 || result.Kind != "pure-transition" {
		t.Fatalf("close used the wrong result format: %+v, %v", result, err)
	}
	retry := submit("close", PeriodType+".close")
	if retry.GetChangeId() != close.GetChangeId() || len(entries) != 2 {
		t.Fatal("retry did not return the same committed result")
	}
	submit("reopen", PeriodType+".reopen")
	if len(entries) != 3 {
		t.Fatal("reopen did not commit one result")
	}
	period, err = live.RecordOf(controller, PeriodType, "2026-10", now)
	if err != nil || period.Record.(Period).State != "open" {
		t.Fatalf("reopen did not restore the open period: %+v, %v", period, err)
	}
	platformserver.CheckReplay(t, live, entries, func() *platformserver.Tenant { return build(t) })
}

func TestJournalAcceptedPeriodTransitionCrashAndRestart(t *testing.T) {
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
	id := fmt.Sprintf("period-%d", time.Now().UnixNano())
	defer func() {
		pool, err := pgxpool.New(ctx, url)
		if err != nil {
			t.Error(err)
			return
		}
		defer pool.Close()
		if _, err := pool.Exec(ctx, `delete from journal where tenant=$1`, id); err != nil {
			t.Error(err)
		}
	}()
	compose := func() *platformserver.Tenant { return buildTenant(t, id) }
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := compose()
	live.Record = func(e platformserver.Entry) {
		if err := journal.Append(ctx, id, e); err != nil {
			panic(err)
		}
	}
	live.AcceptResult = func(e platformserver.Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, e, key, hash); err != nil {
			return nil, err
		}
		panic("injected crash after durable append")
	}
	member, _ := live.Member("cy")
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	request := func(key, schema string) *pb.Submission {
		return &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: PeriodType, Id: "2026-10"},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(`{}`)}
	}
	if _, err := live.Submit(member, request("open", SchemaPeriodOpen), at); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("failure after append did not interrupt the application")
			}
		}()
		live.Submit(member, request("close", PeriodType+".close"), at)
	}()
	period, refused := live.RecordOf(member, PeriodType, "2026-10", at)
	if refused != nil || period.Record.(Period).State != "open" {
		t.Fatalf("unapplied result changed the live period: %+v, %v", period, refused)
	}
	reopened, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 2 || entries[1].Kind != "accepted-result" {
		t.Fatalf("missing durable transition: %d entries, %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	period, refused = recovered.RecordOf(member, PeriodType, "2026-10", at)
	if refused != nil || period.Record.(Period).State != "closed" {
		t.Fatalf("durable transition was not recovered: %+v, %v", period, refused)
	}
	recovered.AcceptResult = func(e platformserver.Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, e, key, hash)
	}
	if _, err := recovered.Submit(member, request("close", PeriodType+".close"), at.Add(time.Hour)); err != nil ||
		reopened.Position(id) != 2 {
		t.Fatalf("recovered retry advanced the journal: %v, position=%d", err, reopened.Position(id))
	}
	platformserver.CheckReplay(t, recovered, entries, compose)
}
