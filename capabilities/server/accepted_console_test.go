package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestAcceptedConsoleMemberStateCommitsWithReceipt(t *testing.T) {
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"admin"}, Member: platform.Member{
			ID: "admin", Roles: map[string]string{PlatformApp: Admin, "stock": "clerk"}}}
		tn, err := NewTenant("console-accepted", NewConsole("console-accepted", seat), newStock("console-accepted"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	admin, _ := tn.Member("admin")
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	sub := &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp,
		IdempotencyKey: "add-operator", Target: &pb.EntityRef{Type: MemberType, Id: "operator"},
		Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(`{"subject":"user:operator@example.com"}`)}
	var entries []Entry
	fail := true
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if _, ok := tn.Member("operator"); ok {
			t.Fatal("directory was visible before the durable result")
		}
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, err := tn.Submit(admin, sub, now); err == nil {
		t.Fatal("append failure accepted a member")
	}
	if _, ok := tn.Member("operator"); ok || len(entries) != 0 {
		t.Fatal("append failure changed the directory")
	}
	fail = false
	receipt, err := tn.Submit(admin, sub, now)
	if err != nil || receipt == nil || len(entries) != 1 {
		t.Fatalf("member decision not accepted: %v", err)
	}
	batch, _, decodeErr := decodeAcceptedBatch(entries[0].Body)
	if decodeErr != nil || len(batch.States) != 1 || batch.States[0].App != PlatformApp || len(batch.Rows) != 0 {
		t.Fatalf("directory was not saved as a single owned state: %+v %v", batch, decodeErr)
	}
	if operator, ok := tn.app(PlatformApp).(*Console).Member("user:operator@example.com"); !ok || operator.ID != "operator" {
		t.Fatal("accepted member was not installed")
	}
	retry, err := tn.Submit(admin, sub, now.Add(time.Hour))
	if err != nil || retry.GetChangeId() != receipt.GetChangeId() || len(entries) != 1 {
		t.Fatalf("retry ran another directory decision: %v", err)
	}
	CheckReplay(t, tn, entries, compose)

	damaged := entries[0]
	var result acceptedBatch
	if err := json.Unmarshal(damaged.Body, &result); err != nil {
		t.Fatal(err)
	}
	result.States[0].Image = []byte(`{"members":{},"subjects":{"orphan":"missing"}}`)
	result.Digest, _ = digestAcceptedBatch(result)
	damaged.Body, _ = json.Marshal(result)
	if err := compose().Replay([]Entry{damaged}); err == nil {
		t.Fatal("invalid directory image was accepted on recovery")
	}
}

func TestJournalAcceptedConsoleCrashAfterCommit(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("console-accepted-%d", time.Now().UnixNano())
	defer j.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if entries, err := j.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new tenant's journal is not empty: %v %v", entries, err)
	}
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"admin"}, Member: platform.Member{
			ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}
		tn, err := NewTenant(id, NewConsole(id, seat))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	admin, _ := live.Member("admin")
	now := time.Date(2026, 9, 28, 13, 0, 0, 0, time.UTC)
	sub := &pb.Submission{TenantId: id, PrincipalId: admin.ID, Authority: PlatformApp,
		IdempotencyKey: "create-after-crash", Target: &pb.EntityRef{Type: MemberType, Id: "new-member"},
		Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(`{"subject":"user:new@example.com"}`)}
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if _, ok := live.Member("new-member"); ok {
			t.Fatal("member was visible before PostgreSQL accepted the result")
		}
		if _, err := j.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated crash after PostgreSQL commit, before application")
	}
	if _, err := live.Submit(admin, sub, now); err == nil {
		t.Fatal("lost response reported success")
	}
	if _, ok := live.Member("new-member"); ok {
		t.Fatal("the interrupted process applied a result")
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("result did not survive the simulated crash: %v, %v", entries, err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if member, ok := recovered.app(PlatformApp).(*Console).Member("user:new@example.com"); !ok || member.ID != "new-member" {
		t.Fatal("recovery did not install the persisted directory image")
	}
	recovered.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, entry, key, hash)
	}
	retry, refusal := recovered.Submit(admin, sub, now.Add(time.Minute))
	if refusal != nil || retry == nil || reopened.Position(id) != 1 {
		t.Fatalf("retry did not reuse the committed receipt: %v (position %d)", refusal, reopened.Position(id))
	}
	CheckReplay(t, recovered, entries, compose)
}
