package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// The journal's scope index and committed-body lookup must agree with its
// injected identity decoder, including every accepted-result family.
func TestPostgresAcceptedResultScopes(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	tenant := fmt.Sprintf("journal-scopes-%d", time.Now().UnixNano())
	scopes := []struct{ kind, scope string }{{"record-batch", "submission"}, {"work-result", "work"}, {"input-result", "input"}, {"effect-result", "effect"}, {"host-result", "host"}, {"composite-result", "composite"}, {"usage-result", "usage"}, {"agent-result", "agent"}}
	type envelope struct {
		Tenant  string `json:"tenant"`
		App     string `json:"app"`
		Kind    string `json:"kind"`
		Key     string `json:"key"`
		Hash    string `json:"requestHash"`
		Payload string `json:"payload"`
	}
	identify := func(raw []byte) (Identity, error) {
		var body envelope
		if err := json.Unmarshal(raw, &body); err != nil {
			return Identity{}, err
		}
		scope := "submission"
		for _, candidate := range scopes {
			if body.Kind == candidate.kind {
				scope = candidate.scope
				break
			}
		}
		return Identity{Tenant: body.Tenant, App: body.App, Scope: scope, Key: body.Key, Hash: body.Hash}, nil
	}
	open := func() *Postgres {
		j, err := OpenPostgres(ctx, dsn, identify)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	j := open()
	defer func() { j.pool.Exec(ctx, `delete from journal where tenant=$1`, tenant); j.Close() }()
	if _, err := j.Entries(ctx, tenant, 0); err != nil {
		t.Fatal(err)
	}
	entry := func(kind, scope string) Entry {
		body, _ := json.Marshal(envelope{Tenant: tenant, App: "fixture", Kind: kind, Key: "shared", Hash: "hash-" + scope, Payload: "original"})
		return Entry{App: "fixture", Kind: "accepted-result", Principal: json.RawMessage(`{"id":"fixture"}`), Body: body, At: time.Now().UTC().Truncate(time.Microsecond)}
	}
	// Simulate an existing database with a legacy submission and the old index.
	legacy := entry(scopes[0].kind, scopes[0].scope)
	if raw, err := j.AppendAccepted(ctx, tenant, legacy, "shared", "hash-submission"); err != nil || len(raw) == 0 {
		t.Fatalf("legacy append: %v", err)
	}
	if _, err := j.pool.Exec(ctx, `drop index journal_result_scoped_key`); err != nil {
		t.Fatal(err)
	}
	if _, err := j.pool.Exec(ctx, legacyScopedIndexSQL); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j = open()
	if rows, err := j.Entries(ctx, tenant, 0); err != nil || len(rows) != 1 {
		t.Fatalf("scope migration changed historical rows: %v rows=%d", err, len(rows))
	}
	for _, candidate := range scopes {
		t.Run(candidate.scope, func(t *testing.T) {
			input := entry(candidate.kind, candidate.scope)
			committed, err := j.AppendAccepted(ctx, tenant, input, "shared", "hash-"+candidate.scope)
			if err != nil || len(committed) == 0 || !json.Valid(committed) {
				t.Fatalf("successful append did not return committed JSONB: %v bytes=%d", err, len(committed))
			}
			if bytes.Equal(committed, input.Body) {
				t.Fatal("append returned caller bytes rather than PostgreSQL JSONB")
			}
			saved, err := identify(committed)
			if err != nil || saved.Scope != candidate.scope || saved.Hash != "hash-"+candidate.scope {
				t.Fatalf("committed scope differs: %+v %v", saved, err)
			}
			repeat, err := j.AppendAccepted(ctx, tenant, input, "shared", "hash-"+candidate.scope)
			if err != nil || !bytes.Equal(repeat, committed) {
				t.Fatalf("scope retry lost its original bytes: %v", err)
			}
			var changed envelope
			_ = json.Unmarshal(input.Body, &changed)
			changed.Hash = "another-request"
			input.Body, _ = json.Marshal(changed)
			if _, err := j.AppendAccepted(ctx, tenant, input, "shared", changed.Hash); !errors.Is(err, ErrAcceptedConflict) {
				t.Fatalf("scope key accepted another request: %v", err)
			}
		})
	}
	if rows, err := j.Entries(ctx, tenant, 0); err != nil || len(rows) != len(scopes) {
		t.Fatalf("scopes collided or retries inserted duplicate rows: %v rows=%d", err, len(rows))
	}
	// Startup must not temporarily reconstruct the old scope index over new
	// legitimate same-key rows, and the committed lookup must survive reopen.
	j.Close()
	j = open()
	rows, err := j.Entries(ctx, tenant, 0)
	if err != nil || len(rows) != len(scopes) {
		t.Fatalf("scope migration failed on restart: %v rows=%d", err, len(rows))
	}
	for _, candidate := range scopes {
		if raw, err := j.AppendAccepted(ctx, tenant, entry(candidate.kind, candidate.scope), "shared", "hash-"+candidate.scope); err != nil || len(raw) == 0 {
			t.Fatalf("reopened scope %s lost its answer: %v", candidate.scope, err)
		}
	}
}

func TestPostgresMigrationKeepsCurrentScopedIndex(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenPostgres(ctx, dsn, nil)
	if err != nil {
		t.Fatal(err)
	}
	var before uint32
	if err = j.pool.QueryRow(ctx, `select 'journal_result_scoped_key'::regclass::oid`).Scan(&before); err != nil {
		j.Close()
		t.Fatal(err)
	}
	j.Close()
	j, err = OpenPostgres(ctx, dsn, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var after uint32
	if err = j.pool.QueryRow(ctx, `select 'journal_result_scoped_key'::regclass::oid`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("compatible startup rebuilt the journal index: %d -> %d", before, after)
	}
}
