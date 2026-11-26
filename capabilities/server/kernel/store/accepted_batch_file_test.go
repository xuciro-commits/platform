package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestFileAcceptedBatchAtomicRetry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	j, err := OpenFile(dir, benchmarkIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { j.Close() }()
	for _, tenant := range []string{"tenant", "other"} {
		if _, err := j.Entries(ctx, tenant, 0); err != nil {
			t.Fatal(err)
		}
		e := benchmarkEntry(tenant, "submission", "first", "h")
		if _, err := j.AppendAccepted(ctx, tenant, e, "first", "h"); err != nil {
			t.Fatal(err)
		}
	}
	batch := make([]AcceptedAppend, 1000)
	for i := range batch {
		key := fmt.Sprintf("row-%d", i)
		batch[i] = AcceptedAppend{Entry: benchmarkEntry("tenant", "submission", key, "h"), Key: key, Hash: "h"}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := j.AppendAcceptedBatch(cancelled, "tenant", batch); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if j.Position("tenant") != 1 {
		t.Fatal("cancelled batch wrote records")
	}
	answers, err := j.AppendAcceptedBatch(ctx, "tenant", batch)
	if err != nil || len(answers) != 1000 {
		t.Fatalf("append: %v", err)
	}
	if j.Position("tenant") != 1001 || j.Position("other") != 1 {
		t.Fatal("positions differ")
	}
	j.Close()
	j, err = OpenFile(dir, benchmarkIdentity)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := j.AppendAcceptedBatch(ctx, "tenant", batch)
	if err != nil || !reflect.DeepEqual(retry, answers) {
		t.Fatalf("retry after reopening: %v", err)
	}
	if j.Position("tenant") != 1001 || j.Position("other") != 1 {
		t.Fatal("reopened positions differ")
	}
	conflict := append([]AcceptedAppend{{Entry: benchmarkEntry("tenant", "submission", "new", "h"), Key: "new", Hash: "h"}}, AcceptedAppend{Entry: benchmarkEntry("tenant", "submission", "first", "different"), Key: "first", Hash: "different"})
	if _, err := j.AppendAcceptedBatch(ctx, "tenant", conflict); !errors.Is(err, ErrAcceptedConflict) {
		t.Fatalf("conflict: %v", err)
	}
	if j.Position("tenant") != 1001 {
		t.Fatal("conflict wrote records")
	}
	e := benchmarkEntry("tenant", "submission", "last", "h")
	if _, err := j.AppendAccepted(ctx, "tenant", e, "last", "h"); err != nil {
		t.Fatal(err)
	}
	if j.Position("tenant") != 1002 {
		t.Fatal("single append after batch failed")
	}
	entries, err := j.Entries(ctx, "tenant", 0)
	if err != nil || len(entries) != 1002 {
		t.Fatalf("replay entries: %d %v", len(entries), err)
	}
}
