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

// An unavailable append must not leak directory or catalog changes. Repeating
// grants exercises slice compaction, rather than only appending a first grant.
func TestAcceptedConsoleAccessIsolation(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("access-isolation", NewConsole("access-isolation",
			Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}},
			Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{}}}), newNotes("access-isolation", "a"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	d := consoleOf(tn)
	var entries []Entry
	seq := 0
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	decide := func(schema, typ, id, payload string) {
		t.Helper()
		seq++
		admin, _ := tn.Member("admin")
		sub := &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: fmt.Sprint(seq), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}
		before, _ := d.AcceptedState()
		permitted := tn.app("a").Manifest().Actions.PermitsAny([]string{"scribe"}, "a.note")
		tn.AcceptResult = func(Entry, string, string) ([]byte, error) {
			got, _ := d.AcceptedState()
			if schema == SchemaMemberSuspend && !d.noticed("bo", "old-session", "", "sign-in", now) {
				t.Fatal("failed append ended a session")
			}
			if string(got) != string(before) {
				t.Fatal("staging changed live directory")
			}
			if tn.app("a").Manifest().Actions.PermitsAny([]string{"scribe"}, "a.note") != permitted {
				t.Fatal("staging changed live catalog")
			}
			return nil, errors.New("append unavailable")
		}
		if _, err := tn.Submit(admin, sub, now); err == nil {
			t.Fatal("failed append succeeded")
		}
		after, _ := d.AcceptedState()
		if string(after) != string(before) {
			t.Fatal("failed append changed state")
		}
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
		if _, err := tn.Submit(admin, sub, now); err != nil {
			t.Fatal(err)
		}
	}
	decide(SchemaRoleSave, RoleType, "scribe", `{"app":"a","actions":["a.note"]}`)
	decide(SchemaGrant, MemberType, "bo", `{"app":"a","role":"scribe"}`)
	decide(SchemaGrant, MemberType, "bo", `{"app":"a","role":"scribe","reason":"replacement"}`)
	decide(SchemaGrant, MemberType, "bo", `{"app":"a","role":"writer"}`)
	decide(SchemaRevoke, MemberType, "bo", `{"app":"a","role":"writer"}`)
	if bo, _ := tn.Member("bo"); !bo.Holds("a", "scribe") || bo.Holds("a", "writer") {
		t.Fatal("role-specific revoke changed another grant")
	}
	decide(SchemaProfileUpdate, ProfileType, "bo", `{"mail":true,"displayName":"Before"}`)
	decide(SchemaProfileUpdate, ProfileType, "bo", `{"mail":false,"displayName":"After"}`)
	if a := d.Account("bo"); a.Mail == nil || *a.Mail || a.DisplayName != "After" {
		t.Fatal("profile not committed")
	}
	d.noticed("bo", "old-session", "", "sign-in", now)
	decide(SchemaTokenIssue, TokenType, "ci", `{"label":"CI"}`)
	secret, ok := d.Minted("ci", "admin", now)
	if !ok {
		t.Fatal("token secret unavailable")
	}
	if !d.noticed("admin", secret, "", "token", now) {
		t.Fatal("token session refused")
	}
	if sessions := d.Sessions("admin", secret); len(sessions) != 1 || sessions[0].Token != "ci" {
		t.Fatal("token session lacks owning token")
	}
	decide(SchemaTokenRevoke, TokenType, "ci", `{}`)
	if len(d.Sessions("admin", secret)) != 0 || d.noticed("admin", secret, "", "token", now) {
		t.Fatal("revoked token session survived")
	}
	decide(SchemaMemberSuspend, MemberType, "bo", `{}`)
	decide(SchemaMemberResume, MemberType, "bo", `{}`)
	if d.noticed("bo", "old-session", "", "sign-in", now) {
		t.Fatal("resume revived a suspended session")
	}
	decide(SchemaRoleRemove, RoleType, "scribe", `{}`)
	if x, _ := tn.Explain("bo", "a.note"); x.Verdict.Allow {
		t.Fatal("removed role still permits actions")
	}
	CheckReplay(t, tn, entries, compose)
}
