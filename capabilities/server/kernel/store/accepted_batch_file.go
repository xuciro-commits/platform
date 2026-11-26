package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// AppendAcceptedBatch replaces the existing JSONL file atomically. It leaves
// the on-disk format unchanged, so existing replay and snapshots still work.
// This is a correctness-first path for the Lite file journal; the SQL journal
// is the intended backend for high-volume imports.
func (j *File) AppendAcceptedBatch(ctx context.Context, tenant string, batch []AcceptedAppend) ([][]byte, error) {
	ids, err := batchIdentities(j.identify, tenant, batch)
	if err != nil || len(batch) == 0 {
		return nil, err
	}
	j.writer.Lock()
	defer j.writer.Unlock()
	if j.writeFault != nil {
		return nil, j.writeFault
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	seq, ready := j.next[tenant]
	if !ready {
		return nil, fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	answers := make([][]byte, len(batch))
	var pending []fileLine
	var indexes [][4]string
	var hashes []string
	for i, item := range batch {
		id := ids[i]
		index := [4]string{tenant, id.App, id.Scope, id.Key}
		if prior, ok := j.results[index]; ok {
			if prior.hash != id.Hash {
				return nil, ErrAcceptedConflict
			}
			answers[i] = bytes.Clone(prior.body)
			continue
		}
		line := fileLine{Tenant: tenant, Seq: seq, Entry: item.Entry}
		raw, err := json.Marshal(line)
		if err != nil {
			return nil, err
		}
		var stored fileLine
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, err
		}
		stored.Entry.At = stored.Entry.At.UTC()
		pending = append(pending, stored)
		indexes = append(indexes, index)
		hashes = append(hashes, id.Hash)
		answers[i] = bytes.Clone(stored.Entry.Body)
		seq++
	}
	if len(pending) == 0 {
		return answers, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next, err := os.CreateTemp(j.dir, ".journal-batch-*")
	if err != nil {
		return nil, err
	}
	path := next.Name()
	installed := false
	defer func() {
		if !installed {
			next.Close()
			os.Remove(path)
		}
	}()
	old, err := os.Open(j.entriesPath())
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(next, old)
	old.Close()
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(next)
	for _, line := range pending {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := encoder.Encode(line); err != nil {
			return nil, err
		}
	}
	if err := next.Sync(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := os.Open(j.dir)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err := os.Rename(path, j.entriesPath()); err != nil {
		return nil, err
	}
	installed = true
	previous := j.file
	j.file = next
	previous.Close()
	// Rename is already visible. A directory-sync error cannot be reported as
	// rollback; prevent any further append until recovery.
	if err := dir.Sync(); err != nil {
		j.writeFault = fmt.Errorf("%w: sync journal batch directory: %v", ErrCommitUnknown, err)
		return nil, j.writeFault
	}
	for i, line := range pending {
		j.entries[tenant] = append(j.entries[tenant], line.Entry)
		j.results[indexes[i]] = fileResult{hash: hashes[i], body: line.Entry.Body}
	}
	j.next[tenant] = seq
	return answers, nil
}
