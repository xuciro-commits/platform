package store

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"platformserver/kernel/protocol"
	"strconv"
	"testing"
	"time"
)

func benchmarkIdentity(raw []byte) (Identity, error) {
	var result struct{ Tenant, App, Scope, Key, RequestHash string }
	err := json.Unmarshal(raw, &result)
	return Identity{result.Tenant, result.App, result.Scope, result.Key, result.RequestHash}, err
}
func benchmarkEntry(tenant, scope, key, hash string) Entry {
	body, _ := json.Marshal(map[string]string{"tenant": tenant, "app": "app", "scope": scope, "key": key, "requestHash": hash})
	return Entry{App: "app", Kind: "accepted-result", Body: body, Principal: json.RawMessage("{}"), Epoch: FormatEpoch}
}

func BenchmarkFileAppendAccepted10k(b *testing.B) {
	dir := b.TempDir()
	file, err := os.Create(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		b.Fatal(err)
	}
	buffer := bufio.NewWriter(file)
	encoder := json.NewEncoder(buffer)
	for i := 1; i <= 10000; i++ {
		if err := encoder.Encode(fileLine{"tenant", int64(i), benchmarkEntry("tenant", "submission", strconv.Itoa(i), "h")}); err != nil {
			b.Fatal(err)
		}
	}
	if err := buffer.Flush(); err != nil {
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	journal, err := OpenFile(dir, benchmarkIdentity)
	if err != nil {
		b.Fatal(err)
	}
	defer journal.Close()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		key := strconv.Itoa(10001 + i)
		if _, err := journal.AppendAccepted(ctx, "tenant", benchmarkEntry("tenant", "submission", key, "h"), key, "h"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFileAudit1k(b *testing.B) {
	journal, err := OpenFile(b.TempDir(), benchmarkIdentity)
	if err != nil {
		b.Fatal(err)
	}
	defer journal.Close()
	entries := make([]protocol.AuditEntry, 1000)
	for i := range entries {
		entries[i].ID = strconv.Itoa(i)
	}
	if err := journal.SaveAuditEntries("tenant", entries); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 1000; b.Loop(); i++ {
		if err := journal.SaveAuditEntries("tenant", []protocol.AuditEntry{{ID: strconv.Itoa(i)}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPostgresAppendAccepted(b *testing.B) {
	url := os.Getenv("PLATFORM_BENCH_DATABASE")
	if url == "" {
		b.Skip("requires an isolated benchmark database")
	}
	ctx := context.Background()
	j, err := OpenPostgres(ctx, url, benchmarkIdentity)
	if err != nil {
		b.Fatal(err)
	}
	defer j.Close()
	for _, size := range []int{1, 10} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			tenant := fmt.Sprintf("bench-%d", time.Now().UnixNano())
			if _, err := j.Entries(ctx, tenant, 0); err != nil {
				b.Fatal(err)
			}
			if err := j.InitializeState(ctx, tenant, 0, StateDelta{}); err != nil {
				b.Fatal(err)
			}
			j.Delta = func(raw []byte) (StateDelta, error) {
				id, err := benchmarkIdentity(raw)
				var d StateDelta
				for i := 0; i < size; i++ {
					d.Records = append(d.Records, StateRecord{Type: "object", ID: id.Key + "-" + strconv.Itoa(i), Image: json.RawMessage("{}")})
				}
				return d, err
			}
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				key := strconv.Itoa(i)
				if _, err := j.AppendAccepted(ctx, tenant, benchmarkEntry(tenant, "submission", key, "h"), key, "h"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFileAcceptedIdentitySurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	open := func() *File {
		j, err := OpenFile(dir, benchmarkIdentity)
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	ctx := context.Background()
	j := open()
	for _, tenant := range []string{"a", "b"} {
		if _, err := j.Entries(ctx, tenant, 0); err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{{"one|two", "three"}, {"one", "two|three"}} {
			entry := benchmarkEntry(tenant, pair[0], pair[1], "h")
			if _, err := j.AppendAccepted(ctx, tenant, entry, pair[1], "h"); err != nil {
				t.Fatal(err)
			}
		}
	}
	j.Close()
	j = open()
	defer j.Close()
	for _, tenant := range []string{"a", "b"} {
		for _, pair := range [][2]string{{"one|two", "three"}, {"one", "two|three"}} {
			e := benchmarkEntry(tenant, pair[0], pair[1], "h")
			body, err := j.AppendAccepted(ctx, tenant, e, pair[1], "h")
			if err != nil || !bytes.Equal(body, e.Body) {
				t.Fatalf("retry: %s %v", body, err)
			}
			e = benchmarkEntry(tenant, pair[0], pair[1], "new")
			if _, err = j.AppendAccepted(ctx, tenant, e, pair[1], "new"); !errors.Is(err, ErrAcceptedConflict) {
				t.Fatalf("conflict: %v", err)
			}
		}
		if j.Position(tenant) != 2 {
			t.Fatal("retry appended a result")
		}
	}
}
