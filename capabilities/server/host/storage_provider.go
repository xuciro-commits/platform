package host

import (
	"context"
	"path/filepath"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (d *Deployment) storageModule() platform.CapabilityModule {
	if d.Profile == "lightweight" || d.Database != "" || d.Storage != nil {
		return d.journalProvider().Module
	}
	return platform.MemoryStorageModule()
}

func PostgresProvider(address string) persistence.JournalProvider {
	return persistence.JournalProvider{Module: platform.CapabilityModule{ID: "platform.storage.postgres", Title: "PostgreSQL storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.sql-state.v1", Provides: []platform.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, AtomicCurrentState: true, FixedReadViews: true, Open: func(ctx context.Context) (Journals, error) { return OpenJournal(ctx, address) }}
}

func FileProvider(directory string) persistence.JournalProvider {
	return persistence.JournalProvider{Module: platform.CapabilityModule{ID: "platform.storage.file", Title: "Local journal storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.file-persistence.v1", Provides: []platform.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, Open: func(context.Context) (Journals, error) { return OpenFileJournal(directory) }}
}

func (d *Deployment) journalProvider() persistence.JournalProvider {
	if d.Storage != nil {
		return *d.Storage
	}
	if d.Profile == "lightweight" {
		return FileProvider(filepath.Join(d.Data, "journal"))
	}
	return PostgresProvider(d.Database)
}

func (d *Deployment) openJournals(ctx context.Context) (Journals, error) {
	return d.journalProvider().OpenChecked(ctx, d.Profile == "lightweight")
}
