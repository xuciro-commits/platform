package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pgx "github.com/jackc/pgx/v5"
	"platformserver/kernel/fileio"
)

const TenantBackupFormat = "platform.tenant-backup.v1"

type TenantBackupSnapshot struct {
	Seq   int64           `json:"seq"`
	Code  string          `json:"code"`
	State json.RawMessage `json:"state"`
}

type TenantBackupPayload struct {
	Format   string                `json:"format"`
	Epoch    int                   `json:"epoch"`
	Tenant   string                `json:"tenant"`
	Entries  []Entry               `json:"entries"`
	Snapshot *TenantBackupSnapshot `json:"snapshot,omitempty"`
}

type TenantBackupEnvelope struct {
	Format  string          `json:"format"`
	SHA256  string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

func validateTenantBackup(target string, identify Identify, payload TenantBackupPayload) (map[[3]string]string, error) {
	if payload.Format != TenantBackupFormat {
		return nil, fmt.Errorf("backup format %q differs from %s", payload.Format, TenantBackupFormat)
	}
	if payload.Epoch != FormatEpoch {
		return nil, fmt.Errorf("backup format epoch %d differs from host epoch %d", payload.Epoch, FormatEpoch)
	}
	if strings.TrimSpace(target) == "" || payload.Tenant != target {
		return nil, fmt.Errorf("backup tenant %q differs from target tenant %q", payload.Tenant, target)
	}
	if len(payload.Entries) == 0 {
		return nil, fmt.Errorf("backup for tenant %s has no accepted entries", target)
	}
	if identify == nil {
		return nil, fmt.Errorf("backup verification lacks identity decoder")
	}
	seen := make(map[[3]string]string, len(payload.Entries))
	for i, e := range payload.Entries {
		seq := int64(i + 1)
		if e.Epoch != FormatEpoch {
			return nil, fmt.Errorf("entry %d format epoch %d differs from host epoch %d", seq, e.Epoch, FormatEpoch)
		}
		if e.Kind != "accepted-result" {
			return nil, fmt.Errorf("entry %d has unsupported kind %q", seq, e.Kind)
		}
		if len(e.Principal) == 0 || !json.Valid(e.Principal) || len(e.Body) == 0 || !json.Valid(e.Body) {
			return nil, fmt.Errorf("entry %d has invalid JSON", seq)
		}
		id, err := identify(e.Body)
		if err != nil || id.Tenant != target || id.App != e.App || id.Key == "" || id.Hash == "" {
			return nil, fmt.Errorf("entry %d identity differs from tenant %s: %v", seq, target, err)
		}
		scoped := [3]string{id.App, id.Scope, id.Key}
		if prior, dup := seen[scoped]; dup {
			if prior != id.Hash {
				return nil, fmt.Errorf("entry %d: %w", seq, ErrAcceptedConflict)
			}
			return nil, fmt.Errorf("entry %d duplicates idempotency key %s/%s/%s", seq, id.App, id.Scope, id.Key)
		}
		seen[scoped] = id.Hash
	}
	if snap := payload.Snapshot; snap != nil {
		if snap.Seq < 1 || snap.Seq > int64(len(payload.Entries)) || strings.TrimSpace(snap.Code) == "" || len(snap.State) == 0 || !json.Valid(snap.State) {
			return nil, fmt.Errorf("backup snapshot metadata or JSON is invalid")
		}
		var header struct {
			Runtime    json.RawMessage `json:"runtime"`
			Checkpoint *struct {
				Epoch    int    `json:"epoch"`
				Tenant   string `json:"tenant"`
				Position int64  `json:"position"`
			} `json:"checkpoint"`
		}
		if err := json.Unmarshal(snap.State, &header); err != nil {
			return nil, fmt.Errorf("backup snapshot header: %w", err)
		}
		if len(header.Runtime) > 0 && string(header.Runtime) != "null" {
			return nil, fmt.Errorf("backup snapshot must be portable, not a SQL runtime checkpoint")
		}
		if header.Checkpoint != nil && (header.Checkpoint.Epoch != FormatEpoch || header.Checkpoint.Tenant != target || header.Checkpoint.Position != snap.Seq) {
			return nil, fmt.Errorf("backup snapshot checkpoint header differs from tenant, epoch or position")
		}
	}
	return seen, nil
}

func EncodeTenantBackup(tenant string, identify Identify, entries []Entry, snapshot *TenantBackupSnapshot) ([]byte, error) {
	normalized := make([]Entry, len(entries))
	for i, e := range entries {
		e.At = e.At.UTC()
		normalized[i] = e
	}
	payload := TenantBackupPayload{
		Format:   TenantBackupFormat,
		Epoch:    FormatEpoch,
		Tenant:   tenant,
		Entries:  normalized,
		Snapshot: snapshot,
	}
	if _, err := validateTenantBackup(tenant, identify, payload); err != nil {
		return nil, err
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payloadBytes)
	envelopeBytes, err := json.Marshal(TenantBackupEnvelope{
		Format:  TenantBackupFormat,
		SHA256:  hex.EncodeToString(sum[:]),
		Payload: payloadBytes,
	})
	if err != nil {
		return nil, err
	}
	return packSnapshot(envelopeBytes)
}

func DecodeTenantBackup(raw []byte, target string, identify Identify) (TenantBackupPayload, map[[3]string]string, error) {
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup gzip header: %w", err)
	}
	unpacked, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup gzip stream: %w", err)
	}
	if closeErr != nil {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup gzip trailer: %w", closeErr)
	}
	var env TenantBackupEnvelope
	if err := json.Unmarshal(unpacked, &env); err != nil {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup envelope: %w", err)
	}
	if env.Format != TenantBackupFormat {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup envelope format %q differs from %s", env.Format, TenantBackupFormat)
	}
	if len(env.Payload) == 0 {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup payload is empty")
	}
	sum := sha256.Sum256(env.Payload)
	if !strings.EqualFold(env.SHA256, hex.EncodeToString(sum[:])) {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup sha256 checksum mismatch")
	}
	var payload TenantBackupPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return TenantBackupPayload{}, nil, fmt.Errorf("backup payload: %w", err)
	}
	seen, err := validateTenantBackup(target, identify, payload)
	if err != nil {
		return TenantBackupPayload{}, nil, err
	}
	return payload, seen, nil
}

func WriteTenantBackupFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := fileio.WriteAtomic(path, data); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ExportBackup(ctx context.Context, j Journals, tenant string, snapshot func([]Entry) (*TenantBackupSnapshot, error)) ([]byte, int, error) {
	switch backend := j.(type) {
	case *Postgres:
		return backend.ExportTenantBackup(ctx, tenant, snapshot)
	case *File:
		return backend.ExportTenantBackup(ctx, tenant, snapshot)
	default:
		return nil, 0, fmt.Errorf("selected storage does not support tenant backup export")
	}
}

func ImportBackup(ctx context.Context, j Journals, tenant string, raw []byte) (int, error) {
	switch backend := j.(type) {
	case *Postgres:
		return backend.ImportTenantBackup(ctx, tenant, raw)
	case *File:
		return backend.ImportTenantBackup(ctx, tenant, raw)
	default:
		return 0, fmt.Errorf("selected storage does not support tenant backup import")
	}
}

func (j *Postgres) ExportTenantBackup(ctx context.Context, tenant string, snapshot func([]Entry) (*TenantBackupSnapshot, error)) ([]byte, int, error) {
	entries, err := j.Entries(ctx, tenant, 0)
	if err != nil {
		return nil, 0, err
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("tenant %s has no journal entries", tenant)
	}
	var snap *TenantBackupSnapshot
	if snapshot != nil {
		if snap, err = snapshot(entries); err != nil {
			return nil, 0, err
		}
	}
	raw, err := EncodeTenantBackup(tenant, j.identify, entries, snap)
	if err != nil {
		return nil, 0, err
	}
	return raw, len(entries), nil
}

func (j *Postgres) ImportTenantBackup(ctx context.Context, tenant string, raw []byte) (int, error) {
	payload, _, err := DecodeTenantBackup(raw, tenant, j.identify)
	if err != nil {
		return 0, err
	}
	j.mu.Lock()
	lock := j.locks[tenant]
	if lock == nil {
		lock = &sync.Mutex{}
		j.locks[tenant] = lock
	}
	j.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()

	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1), 63)`, tenant); err != nil {
		return 0, err
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from journal where tenant=$1)
		or exists(select 1 from snapshots where tenant=$1)
		or exists(select 1 from runtime_tenants where tenant=$1)
		or exists(select 1 from runtime_records where tenant=$1)
		or exists(select 1 from runtime_results where tenant=$1)`, tenant).Scan(&occupied); err != nil {
		return 0, err
	}
	if occupied {
		return 0, fmt.Errorf("tenant %s already has state in target database; import requires an empty tenant", tenant)
	}
	const chunk = 500
	for start := 0; start < len(payload.Entries); start += chunk {
		end := min(start+chunk, len(payload.Entries))
		batch := &pgx.Batch{}
		for i := start; i < end; i++ {
			e := payload.Entries[i]
			batch.Queue(`insert into journal (tenant, seq, app, kind, principal, body, at, versions, epoch) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				tenant, int64(i+1), e.App, e.Kind, e.Principal, e.Body, e.At.UTC(), e.Versions, e.Epoch)
		}
		results := tx.SendBatch(ctx, batch)
		for range end - start {
			if _, err = results.Exec(); err != nil {
				_ = results.Close()
				return 0, err
			}
		}
		if err = results.Close(); err != nil {
			return 0, err
		}
	}
	if snap := payload.Snapshot; snap != nil {
		packed, err := packSnapshot(snap.State)
		if err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `insert into snapshots (tenant, seq, code, state) values ($1,$2,$3,$4)`,
			tenant, snap.Seq, snap.Code, packed); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	j.mu.Lock()
	j.next[tenant] = int64(len(payload.Entries)) + 1
	j.mu.Unlock()
	return len(payload.Entries), nil
}

func (j *File) ExportTenantBackup(ctx context.Context, tenant string, snapshot func([]Entry) (*TenantBackupSnapshot, error)) ([]byte, int, error) {
	entries, err := j.Entries(ctx, tenant, 0)
	if err != nil {
		return nil, 0, err
	}
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("tenant %s has no journal entries", tenant)
	}
	var snap *TenantBackupSnapshot
	if snapshot != nil {
		if snap, err = snapshot(entries); err != nil {
			return nil, 0, err
		}
	}
	raw, err := EncodeTenantBackup(tenant, j.identify, entries, snap)
	if err != nil {
		return nil, 0, err
	}
	return raw, len(entries), nil
}

func (j *File) ImportTenantBackup(_ context.Context, tenant string, raw []byte) (int, error) {
	payload, _, err := DecodeTenantBackup(raw, tenant, j.identify)
	if err != nil {
		return 0, err
	}
	j.writer.Lock()
	defer j.writer.Unlock()
	if j.writeFault != nil {
		return 0, j.writeFault
	}
	j.mu.Lock()
	occupied := len(j.entries[tenant]) > 0 || len(j.snapshotsOf(tenant)) > 0
	j.mu.Unlock()
	if occupied {
		return 0, fmt.Errorf("tenant %s already has state in target journal; import requires an empty tenant", tenant)
	}
	var buf bytes.Buffer
	staged := make([]Entry, len(payload.Entries))
	stagedResults := make(map[[4]string]fileResult, len(payload.Entries))
	for i, e := range payload.Entries {
		e.At = e.At.UTC()
		line, err := json.Marshal(fileLine{Tenant: tenant, Seq: int64(i + 1), Entry: e})
		if err != nil {
			return 0, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
		id, err := j.identify(e.Body)
		if err != nil {
			return 0, err
		}
		staged[i] = e
		stagedResults[[4]string{tenant, id.App, id.Scope, id.Key}] = fileResult{id.Hash, e.Body}
	}
	if _, err := j.file.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	if err := j.file.Sync(); err != nil {
		return 0, err
	}
	if snap := payload.Snapshot; snap != nil {
		if err := j.SaveSnapshot(context.Background(), tenant, snap.Seq, snap.Code, snap.State); err != nil {
			return 0, err
		}
	}
	j.mu.Lock()
	j.entries[tenant] = staged
	j.next[tenant] = int64(len(staged)) + 1
	j.mu.Unlock()
	for k, v := range stagedResults {
		j.results[k] = v
	}
	return len(staged), nil
}
