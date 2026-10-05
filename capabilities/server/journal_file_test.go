package platformserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// journalsUnderTest are the backends the same contract is put through: the
// lightweight profile's single file always, PostgreSQL when a test database is
// configured. ADR-0049 S1 asks for one set of tests on both.
func journalsUnderTest(t *testing.T) []struct {
	name    string
	open    func(t *testing.T, tenant string) Journals
	reopen  func(t *testing.T, tenant string) Journals
	cleanup func(t *testing.T, tenant string)
} {
	t.Helper()
	dir := t.TempDir()
	backends := []struct {
		name    string
		open    func(t *testing.T, tenant string) Journals
		reopen  func(t *testing.T, tenant string) Journals
		cleanup func(t *testing.T, tenant string)
	}{
		{
			name: "file",
			open: func(t *testing.T, _ string) Journals {
				j, err := OpenFileJournal(filepath.Join(dir, "journal"))
				if err != nil {
					t.Fatal(err)
				}
				return j
			},
			reopen: func(t *testing.T, _ string) Journals {
				j, err := OpenFileJournal(filepath.Join(dir, "journal"))
				if err != nil {
					t.Fatal(err)
				}
				return j
			},
			cleanup: func(*testing.T, string) {},
		},
	}
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn != "" {
		open := func(t *testing.T, tenant string) Journals {
			j, err := OpenJournal(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			return j
		}
		// The test closes the journals it opened; forgetting opens its own
		// connection so it never runs against a closed pool.
		cleanup := func(t *testing.T, tenant string) {
			j, err := OpenJournal(context.Background(), dsn)
			if err != nil {
				t.Logf("forget %s: %v", tenant, err)
				return
			}
			forgetJournal(t, j, tenant)
		}
		backends = append(backends, struct {
			name    string
			open    func(t *testing.T, tenant string) Journals
			reopen  func(t *testing.T, tenant string) Journals
			cleanup func(t *testing.T, tenant string)
		}{name: "postgres", open: open, reopen: open, cleanup: cleanup})
	}
	return backends
}

// forgetJournal removes everything a contract test wrote for its tenant.
func forgetJournal(t *testing.T, j Journals, tenant string) {
	t.Helper()
	if pg, ok := pool(j); ok {
		for _, table := range []string{"journal", "snapshots", "embeddings", "transcripts"} {
			if _, err := pg.Exec(context.Background(), "delete from "+table+" where tenant=$1", tenant); err != nil {
				t.Logf("cleanup %s: %v", table, err)
			}
		}
	}
	j.Close()
}

// TestJournalsKeepTheSameContract puts both backends through the semantics the
// host relies on: entries in order per tenant, an append that expects the read
// position, a snapshot of this code, an accepted result committed once under
// its key, and derived data that survives a restart (ADR-0049 D2).
func TestJournalsKeepTheSameContract(t *testing.T) {
	at := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			tenant := fmt.Sprintf("journals-%s-%d", backend.name, time.Now().UnixNano())
			t.Cleanup(func() { backend.cleanup(t, tenant) })
			ctx := context.Background()
			j := backend.open(t, tenant)
			defer j.Close()
			compose := func() *Tenant {
				seat := Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}
				tn, err := NewTenant(tenant, NewConsole(tenant, seat))
				if err != nil {
					t.Fatal(err)
				}
				return tn
			}
			// An append before reading refuses: a writer that has not seen the
			// entries cannot continue them.
			if err := j.Append(ctx, tenant, Entry{App: PlatformApp, Kind: "submission", At: at}); err == nil ||
				!strings.Contains(err.Error(), "before reading") {
				t.Fatalf("append before read: %v", err)
			}
			if entries, err := j.Entries(ctx, tenant, 0); err != nil || len(entries) != 0 {
				t.Fatalf("a new journal is not empty: %v %v", entries, err)
			}
			// A decision goes in through the accepted-result path and is applied.
			live := compose()
			live.attachJournal(ctx, j)
			admin, _ := live.Member("admin")
			submit := func(key, id, subject string, when time.Time) (*pb.ChangeRecord, *kernel.Error) {
				return live.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: admin.ID, Authority: PlatformApp,
					IdempotencyKey: key, Target: &pb.EntityRef{Type: MemberType, Id: id},
					Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(fmt.Sprintf(`{"subject":%q}`, subject))}, when)
			}
			first, failure := submit("k1", "m-ana", "user:ana@example.com", at)
			if failure != nil {
				t.Fatalf("submit: %v", failure)
			}
			if _, known := live.app(PlatformApp).(*Console).Member("user:ana@example.com"); !known {
				t.Fatal("the committed decision was not applied")
			}
			if j.Position(tenant) != 1 {
				t.Fatalf("one decision wrote %d entries", j.Position(tenant))
			}
			entries, err := j.Entries(ctx, tenant, 0)
			if err != nil || len(entries) != 1 || entries[0].Kind != "accepted-result" || entries[0].App != PlatformApp {
				t.Fatalf("the journal holds %+v, %v", entries, err)
			}
			// The same request again is the same decision: no second entry, the
			// same answer.
			again, failure := submit("k1", "m-ana", "user:ana@example.com", at)
			if failure != nil {
				t.Fatalf("retry: %v", failure)
			}
			if again.GetChangeId() != first.GetChangeId() || j.Position(tenant) != 1 {
				t.Fatalf("a retry committed twice: %s vs %s at %d", again.GetChangeId(), first.GetChangeId(), j.Position(tenant))
			}
			// The same key for another request is a conflict.
			if _, err := submit("k1", "m-bo", "user:bo@example.com", at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
				t.Fatalf("a reused key: %v", err)
			}
			// A snapshot of this code comes back; another code writes its own.
			state := []byte(`{"member":"ana"}`)
			if err := j.SaveSnapshot(ctx, tenant, j.Position(tenant), "code-a", state); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if seq, got, ok, err := j.Snapshot(ctx, tenant, "code-a"); err != nil || !ok || seq != 1 || string(got) != string(state) {
				t.Fatalf("snapshot: %d %q %v %v", seq, got, ok, err)
			}
			if _, _, ok, err := j.Snapshot(ctx, tenant, "code-b"); err != nil || ok {
				t.Fatalf("another code's snapshot: %v %v", ok, err)
			}
			// A repaired checkpoint replaces this tenant's own.
			if err := j.RepairSnapshot(ctx, tenant, j.Position(tenant), "code-a", []byte(`{"member":"ana","repaired":true}`)); err != nil {
				t.Fatalf("repair: %v", err)
			}
			if _, got, ok, err := j.Snapshot(ctx, tenant, "code-a"); err != nil || !ok || !strings.Contains(string(got), "repaired") {
				t.Fatalf("repaired snapshot: %q %v %v", got, ok, err)
			}
			// Derived data is kept beside the entries and read back.
			j.SaveVectors(tenant, "local/embed", map[string][]float32{"h1": {1, 0, 0.5}})
			if got := j.Vectors(tenant, "local/embed", []string{"h1", "nope"})["h1"]; len(got) != 3 || got[2] != 0.5 {
				t.Fatalf("vectors: %v", got)
			}
			j.SaveTranscript(Transcript{Tenant: tenant, At: at, Member: "admin", Model: "local/stand-in", Run: "R1", Request: []byte(`{"q":1}`), Answer: []byte(`{"a":1}`), Outcome: "ok"})
			j.SaveTranscript(Transcript{Tenant: tenant, At: at.Add(time.Hour), Member: "admin", Model: "local/stand-in", Run: "R2", Request: []byte(`{"q":2}`), Answer: []byte(`{"a":2}`), Outcome: "ok"})
			if got := j.Transcripts(tenant, "R1", 10); len(got) != 1 || got[0].Run != "R1" {
				t.Fatalf("transcripts of a run: %+v", got)
			}
			if got := j.Transcripts(tenant, "", 10); len(got) != 2 || got[0].Run != "R2" {
				t.Fatalf("transcripts, newest first: %+v", got)
			}
			j.PurgeTranscripts(tenant, at.Add(30*time.Minute))
			if got := j.Transcripts(tenant, "", 10); len(got) != 1 || got[0].Run != "R2" {
				t.Fatalf("purged transcripts: %+v", got)
			}
			// A restart: the same journal, a fresh tenant, the entries replayed.
			j.Close()
			reopened := backend.reopen(t, tenant)
			defer reopened.Close()
			entries, err = reopened.Entries(ctx, tenant, 0)
			if err != nil || len(entries) != 1 {
				t.Fatalf("a restarted journal: %+v %v", entries, err)
			}
			restored := compose()
			if err := restored.Replay(entries); err != nil {
				t.Fatalf("replay: %v", err)
			}
			if _, known := restored.app(PlatformApp).(*Console).Member("user:ana@example.com"); !known {
				t.Fatal("the replayed decision did not apply")
			}
			restored.attachJournal(ctx, reopened)
			// The committed key is still the committed decision, and new work
			// continues after the restart.
			admin, _ = restored.Member("admin")
			if _, err := restored.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: admin.ID, Authority: PlatformApp,
				IdempotencyKey: "k1", Target: &pb.EntityRef{Type: MemberType, Id: "m-ana"},
				Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(`{"subject":"user:ana@example.com"}`)}, at); err != nil {
				t.Fatalf("a retry after restart: %v", err)
			}
			if _, err := restored.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: admin.ID, Authority: PlatformApp,
				IdempotencyKey: "k2", Target: &pb.EntityRef{Type: MemberType, Id: "m-bo"},
				Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(`{"subject":"user:bo@example.com"}`)}, at.Add(time.Second)); err != nil {
				t.Fatalf("new work after restart: %v", err)
			}
			if reopened.Position(tenant) != 2 {
				t.Fatalf("the restarted journal holds %d entries", reopened.Position(tenant))
			}
			if got := reopened.Vectors(tenant, "local/embed", []string{"h1"})["h1"]; len(got) != 3 {
				t.Fatalf("vectors after restart: %v", got)
			}
			if got := reopened.Transcripts(tenant, "", 10); len(got) != 1 || got[0].Run != "R2" {
				t.Fatalf("transcripts after restart: %+v", got)
			}
		})
	}
}

// TestFileJournalRefusesADamagedJournal: a journal whose sequence jumps is not
// state a host may run on, and the file journal says so instead of guessing
// (fail-stop, ADR-0049 D1).
func TestFileJournalRefusesADamagedJournal(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := j.Entries(ctx, "t-1", 0); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(ctx, "t-1", Entry{App: "desk", Kind: "submission", At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	j.Close()
	path := filepath.Join(dir, "journal.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	damaged := strings.Replace(string(raw), `"seq":1`, `"seq":2`, 1)
	if damaged == string(raw) {
		t.Fatalf("the line does not name its sequence: %s", raw)
	}
	if err := os.WriteFile(path, []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenFileJournal(dir); err == nil {
		reopened.Close()
		t.Fatal("a damaged journal was opened")
	} else if !errors.Is(err, errTenantJournal) {
		t.Fatalf("the reason a damaged journal is refused: %v", err)
	}
}
