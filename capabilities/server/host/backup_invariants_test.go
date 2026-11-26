package host

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	build "platformserver/services/resources/authoring"
	"platformserver/services/security/idp"
)

func TestInvariantK1TenantBackupExportImportAndRejections(t *testing.T) {
	const secretNote = "SecretCustomerNote-98765"
	live := newInvariantTenant(t, "invariant-k1-backup")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, revised"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "invariantproject.alpha.create", "invariantproject.alpha", "A1", map[string]any{"note": secretNote})
	live.accept(t, "invariantproject.alpha.edit", "invariantproject.alpha", "A1", map[string]any{"note": secretNote + "-v2"})
	stale := live.submission("invariantproject.alpha.edit", "invariantproject.alpha", "A1", map[string]any{"note": "stale"})
	one := uint32(1)
	stale.ExpectedRevision = &one
	if _, refusal := live.Submit(live.member(t), stale, invariantClock); refusal == nil {
		t.Fatal("stale submission was accepted")
	}
	want := live.recordImage(t)

	repackPayload := func(t *testing.T, payload persistence.TenantBackupPayload, corruptChecksum bool) []byte {
		t.Helper()
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(payloadBytes)
		digest := hex.EncodeToString(sum[:])
		if corruptChecksum {
			payloadBytes = bytes.Replace(payloadBytes, []byte(`"epoch":1`), []byte(`"epoch":99`), 1)
		}
		envBytes, err := json.Marshal(persistence.TenantBackupEnvelope{
			Format:  persistence.TenantBackupFormat,
			SHA256:  digest,
			Payload: payloadBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		var gz bytes.Buffer
		w := gzip.NewWriter(&gz)
		if _, err := w.Write(envBytes); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return gz.Bytes()
	}

	unpackPayload := func(t *testing.T, archive []byte) persistence.TenantBackupPayload {
		t.Helper()
		r, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var env persistence.TenantBackupEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		var payload persistence.TenantBackupPayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	decode := func(t *testing.T, raw json.RawMessage) any {
		t.Helper()
		var value any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	for _, mode := range []string{"file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			srcDir, dstDir := t.TempDir(), t.TempDir()
			var srcURL, dstURL string
			if mode == "postgres" {
				baseURL := os.Getenv("PLATFORM_BENCH_DATABASE")
				if baseURL == "" {
					t.Skip("requires an isolated PostgreSQL database")
				}
				parsed, err := url.Parse(baseURL)
				if err != nil {
					t.Fatal(err)
				}
				stamp := fmt.Sprintf("%d", time.Now().UnixNano())
				srcDB := "backup_src_" + stamp
				dstDB := "backup_dst_" + stamp
				adminConn, err := pgx.Connect(ctx, baseURL)
				if err != nil {
					t.Fatal(err)
				}
				for _, dbName := range []string{srcDB, dstDB} {
					if _, err := adminConn.Exec(ctx, "create database "+dbName); err != nil {
						_ = adminConn.Close(ctx)
						t.Fatal(err)
					}
				}
				_ = adminConn.Close(ctx)
				t.Cleanup(func() {
					conn, connErr := pgx.Connect(context.Background(), baseURL)
					if connErr != nil {
						return
					}
					for _, dbName := range []string{srcDB, dstDB} {
						_, _ = conn.Exec(context.Background(), "drop database if exists "+dbName+" with (force)")
					}
					_ = conn.Close(context.Background())
				})
				srcParsed, dstParsed := *parsed, *parsed
				srcParsed.Path = "/" + srcDB
				dstParsed.Path = "/" + dstDB
				srcURL, dstURL = srcParsed.String(), dstParsed.String()
			}

			openStore := func(which string) persistence.StorageBundle {
				if mode == "file" {
					dir := srcDir
					if which == "dst" {
						dir = dstDir
					}
					b, err := OpenFileJournal(dir)
					if err != nil {
						t.Fatal(err)
					}
					return b
				}
				u := srcURL
				if which == "dst" {
					u = dstURL
				}
				b, err := OpenJournal(ctx, u)
				if err != nil {
					t.Fatal(err)
				}
				return b
			}

			src := openStore("src")
			defer src.Close()
			if _, err := src.Entries(ctx, live.ID, 0); err != nil {
				t.Fatal(err)
			}
			if mode == "postgres" {
				fresh := newInvariantTenantSetup(t, live.ID, nil)
				base, err := fresh.RuntimeStateDelta(fresh.runtimeOwners())
				if err != nil {
					t.Fatal(err)
				}
				if err := src.InitializeState(ctx, live.ID, 0, base); err != nil {
					t.Fatal(err)
				}
			}
			for _, e := range live.entries {
				id, err := acceptedIdentity(e.Body)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := src.AppendAccepted(ctx, live.ID, e, id.Key, id.Hash); err != nil {
					t.Fatal(err)
				}
			}

			archive, exportedCount, err := persistence.ExportBackup(ctx, src.Journals, live.ID, func(entries []persistence.Entry) (*persistence.TenantBackupSnapshot, error) {
				replayed := newInvariantTenantSetup(t, live.ID, nil)
				if err := replayed.Replay(entries); err != nil {
					return nil, err
				}
				raw, seq, err := replayed.Snapshot(func() int64 { return int64(len(entries)) })
				if err != nil {
					return nil, err
				}
				var header struct {
					Checkpoint *runtimekernel.SnapshotCheckpoint `json:"checkpoint"`
				}
				if err := json.Unmarshal(raw, &header); err != nil || header.Checkpoint == nil {
					return nil, fmt.Errorf("missing snapshot checkpoint header")
				}
				key, err := header.Checkpoint.Key(CodeOf(replayed.Tenant))
				if err != nil {
					return nil, err
				}
				return &persistence.TenantBackupSnapshot{Seq: seq, Code: key, State: raw}, nil
			})
			if err != nil || exportedCount != len(live.entries) {
				t.Fatalf("export backup: count=%d err=%v", exportedCount, err)
			}
			backupFile := filepath.Join(t.TempDir(), "official.backup.gz")
			if err := persistence.WriteTenantBackupFile(backupFile, archive); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(backupFile)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("backup file permissions = %o, err = %v; want 0600", info.Mode().Perm(), err)
			}

			dst := openStore("dst")
			defer dst.Close()

			basePayload := unpackPayload(t, archive)
			corruptChecksum := repackPayload(t, basePayload, true)
			if _, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, corruptChecksum); err == nil || !strings.Contains(err.Error(), "sha256 checksum mismatch") {
				t.Fatalf("corrupted sha256 was not rejected first: %v", err)
			}

			badArchiveEpoch := basePayload
			badArchiveEpoch.Epoch = persistence.FormatEpoch + 1
			if _, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, repackPayload(t, badArchiveEpoch, false)); err == nil || !strings.Contains(err.Error(), "epoch") {
				t.Fatalf("archive epoch mismatch was not rejected: %v", err)
			}

			badEntryEpoch := basePayload
			badEntryEpoch.Entries = slices.Clone(basePayload.Entries)
			badEntryEpoch.Entries[len(badEntryEpoch.Entries)-1].Epoch = persistence.FormatEpoch + 1
			if _, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, repackPayload(t, badEntryEpoch, false)); err == nil || !strings.Contains(err.Error(), "epoch") {
				t.Fatalf("entry epoch mismatch was not rejected: %v", err)
			}

			if _, err := persistence.ImportBackup(ctx, dst.Journals, "wrong-tenant", archive); err == nil || !strings.Contains(err.Error(), "tenant") {
				t.Fatalf("target tenant mismatch was not rejected: %v", err)
			}

			dupKey := basePayload
			dupKey.Entries = append(slices.Clone(basePayload.Entries), basePayload.Entries[0])
			if _, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, repackPayload(t, dupKey, false)); err == nil {
				t.Fatal("duplicate entry key in archive was not rejected")
			}

			afterRejections, err := dst.Entries(ctx, live.ID, 0)
			if err != nil || len(afterRejections) != 0 {
				t.Fatalf("rejected imports left %d entries in target store (err=%v)", len(afterRejections), err)
			}

			importedCount, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, archive)
			if err != nil || importedCount != len(live.entries) {
				t.Fatalf("import backup: count=%d err=%v", importedCount, err)
			}
			if _, err := persistence.ImportBackup(ctx, dst.Journals, live.ID, archive); err == nil || !strings.Contains(err.Error(), "empty tenant") {
				t.Fatalf("importing into a non-empty tenant was not rejected: %v", err)
			}

			if mode == "postgres" && dst.RuntimeStateRecovery != nil {
				if pos, active, err := dst.RuntimeStateRecovery.StatePosition(ctx, live.ID); err != nil || active || pos != 0 {
					t.Fatalf("import wrote derived SQL state before host replay: pos=%d active=%v err=%v", pos, active, err)
				}
			}

			for _, snapshotEvery := range []int64{10000, 0} {
				restoredTenant := newInvariantTenantSetup(t, live.ID, nil)
				restoredTenant.Store = dst.Journals
				d := &Deployment{Flags: idp.Flags{SnapshotEvery: snapshotEvery}}
				restoredPos := map[string]int64{}
				freshFlag := map[string]bool{}
				if err := d.tenantRestore(ctx, restoredTenant.Tenant, dst.Journals, CodeOf(restoredTenant.Tenant), restoredPos, freshFlag).Run(ctx); err != nil {
					t.Fatalf("tenantRestore(snapshotEvery=%d): %v", snapshotEvery, err)
				}
				if restoredTenant.quarantined() {
					t.Fatalf("tenantRestore(snapshotEvery=%d) quarantined tenant: %s", snapshotEvery, restoredTenant.Fault.Load().Reason)
				}
				got := restoredTenant.recordImage(t)
				for _, typ := range differingTypes(want, got) {
					if !reflect.DeepEqual(decode(t, want[typ]), decode(t, got[typ])) {
						t.Fatalf("restored(snapshotEvery=%d) %s differs from source", snapshotEvery, typ)
					}
				}
			}

			if mode == "postgres" {
				binPath := filepath.Join(t.TempDir(), "platform-server")
				buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/platform-server")
				buildCmd.Dir = ".."
				if out, err := buildCmd.CombinedOutput(); err != nil {
					t.Fatalf("build platform-server: %v\n%s", err, out)
				}
				cliBackup := filepath.Join(t.TempDir(), "cli.backup.gz")
				for _, badArgs := range [][]string{
					{"-database", srcURL, "-export-tenant", live.ID, "-import-tenant", live.ID, "-backup", cliBackup},
					{"-database", srcURL, "-export-tenant", live.ID},
					{"-export-tenant", live.ID, "-backup", cliBackup},
				} {
					if cmdErr := exec.Command(binPath, badArgs...).Run(); cmdErr == nil {
						t.Fatalf("expected non-zero exit for flags %v", badArgs)
					}
				}
				exportOut, err := exec.Command(binPath, "-database", srcURL, "-export-tenant", live.ID, "-backup", cliBackup).CombinedOutput()
				if err != nil {
					t.Fatalf("CLI export failed: %v\n%s", err, exportOut)
				}
				if strings.Contains(string(exportOut), secretNote) {
					t.Fatalf("CLI export leaked tenant record payload into logs: %s", exportOut)
				}
				cliInfo, err := os.Stat(cliBackup)
				if err != nil || cliInfo.Mode().Perm() != 0o600 {
					t.Fatalf("CLI backup mode = %o, err = %v; want 0600", cliInfo.Mode().Perm(), err)
				}

				baseURL := os.Getenv("PLATFORM_BENCH_DATABASE")
				parsed, _ := url.Parse(baseURL)
				cliDB := fmt.Sprintf("backup_cli_%d", time.Now().UnixNano())
				adminConn, err := pgx.Connect(ctx, baseURL)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := adminConn.Exec(ctx, "create database "+cliDB); err != nil {
					_ = adminConn.Close(ctx)
					t.Fatal(err)
				}
				_ = adminConn.Close(ctx)
				defer func() {
					conn, connErr := pgx.Connect(context.Background(), baseURL)
					if connErr == nil {
						_, _ = conn.Exec(context.Background(), "drop database if exists "+cliDB+" with (force)")
						_ = conn.Close(context.Background())
					}
				}()
				cliParsed := *parsed
				cliParsed.Path = "/" + cliDB
				cliURL := cliParsed.String()

				importOut, err := exec.Command(binPath, "-database", cliURL, "-import-tenant", live.ID, "-backup", cliBackup).CombinedOutput()
				if err != nil {
					t.Fatalf("CLI import into empty DB failed: %v\n%s", err, importOut)
				}
				if cmdErr := exec.Command(binPath, "-database", cliURL, "-import-tenant", live.ID, "-backup", cliBackup).Run(); cmdErr == nil {
					t.Fatal("CLI repeat import into non-empty DB succeeded; want non-zero exit")
				}
			}
		})
	}
}
