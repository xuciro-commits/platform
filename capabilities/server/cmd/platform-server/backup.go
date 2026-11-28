package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	platformserver "platformserver/host"
	runtimekernel "platformserver/kernel"
	runtimeprotocol "platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/services/devops/packages"
	"platformserver/services/security"
)

type backupOptions struct {
	exportTenant string
	importTenant string
	backupFile   string
}

func registerBackupFlags() *backupOptions {
	opts := &backupOptions{}
	flag.StringVar(&opts.exportTenant, "export-tenant", "", "export accepted journal entries and a portable snapshot of this tenant to -backup and exit (requires -database)")
	flag.StringVar(&opts.importTenant, "import-tenant", "", "import a tenant backup from -backup into an empty database and exit (requires -database)")
	flag.StringVar(&opts.backupFile, "backup", "", "file path of the tenant backup archive for -export-tenant or -import-tenant")
	return opts
}

func (o *backupOptions) active() bool {
	return o.exportTenant != "" || o.importTenant != "" || o.backupFile != ""
}

func (o *backupOptions) run(deployment *platformserver.Deployment, development runtimeprotocol.TenantSpec) error {
	exportID := strings.TrimSpace(o.exportTenant)
	importID := strings.TrimSpace(o.importTenant)
	path := strings.TrimSpace(o.backupFile)
	if exportID != "" && importID != "" {
		return fmt.Errorf("-export-tenant and -import-tenant are mutually exclusive")
	}
	if exportID == "" && importID == "" {
		return fmt.Errorf("-backup requires -export-tenant or -import-tenant")
	}
	if path == "" {
		return fmt.Errorf("-backup is required with -export-tenant or -import-tenant")
	}
	if deployment.NewKey || deployment.Mint != "" {
		return fmt.Errorf("-export-tenant and -import-tenant cannot be combined with -idp-new-key or -mint-token")
	}
	if strings.TrimSpace(deployment.Database) == "" {
		return fmt.Errorf("-export-tenant and -import-tenant require -database")
	}
	if err := deployment.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	bundle, err := platformserver.OpenJournal(ctx, deployment.Database)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer bundle.Journals.Close()

	if exportID != "" {
		deployment.Tenants(development)
		pkgIndex, err := packages.LoadPackageIndex(platformserver.PublicationResolver(), deployment.Packages)
		if err != nil {
			return err
		}
		archive, count, err := persistence.ExportBackup(ctx, bundle.Journals, exportID, func(entries []persistence.Entry) (*persistence.TenantBackupSnapshot, error) {
			services := runtimeprotocol.DefaultRuntimeCapabilities(security.Standard{})
			services.StorageModule = platformserver.PostgresProvider(deployment.Database).Module
			t, err := deployment.RebuildWithCapabilities(exportID, services)
			if err != nil {
				return nil, err
			}
			if pkgIndex != nil && t.PackageCatalog != nil {
				t.PackageCatalog.BindPackageIndex(pkgIndex)
			}
			if err := t.Replay(entries); err != nil {
				return nil, err
			}
			raw, seq, err := t.Snapshot(func() int64 { return int64(len(entries)) })
			if err != nil {
				return nil, err
			}
			var header struct {
				Checkpoint *runtimekernel.SnapshotCheckpoint `json:"checkpoint"`
			}
			if err := json.Unmarshal(raw, &header); err != nil || header.Checkpoint == nil {
				return nil, fmt.Errorf("snapshot for tenant %s lacks compatibility header", exportID)
			}
			key, err := header.Checkpoint.Key(platformserver.CodeOf(t))
			if err != nil {
				return nil, err
			}
			return &persistence.TenantBackupSnapshot{Seq: seq, Code: key, State: raw}, nil
		})
		if err != nil {
			return err
		}
		if err := persistence.WriteTenantBackupFile(path, archive); err != nil {
			return err
		}
		log.Printf("exported tenant %s (%d entries)", exportID, count)
		return nil
	}

	archive, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	count, err := persistence.ImportBackup(ctx, bundle.Journals, importID, archive)
	if err != nil {
		return err
	}
	log.Printf("imported tenant %s (%d entries)", importID, count)
	return nil
}
