package platformserver

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"platformserver/journal"
	"platformserver/platform"
)

// JournalProvider opens one deployment-wide store. Business owners still seal
// the original accepted result; providers do not reinterpret their decisions.
type JournalProvider struct {
	Module             platform.CapabilityModule
	Durable            bool
	AtomicCurrentState bool
	FixedReadViews     bool
	Open               func(context.Context) (Journals, error)
}

// MemoryStorageModule describes the existing process-local development journal.
// It does not claim durability or implement a delivery storage substitute.
func MemoryStorageModule() platform.CapabilityModule {
	return platform.CapabilityModule{ID: "platform.storage.memory", Title: "In-memory development storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.memory.v1", Provides: []platform.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}
}

func (d *Deployment) storageModule() platform.CapabilityModule {
	if d.Profile == "lightweight" || d.Database != "" || d.Storage != nil {
		return d.journalProvider().Module
	}
	return MemoryStorageModule()
}

func PostgresProvider(address string) JournalProvider {
	return JournalProvider{Module: platform.CapabilityModule{ID: "platform.storage.postgres", Title: "PostgreSQL storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.sql-state.v1", Provides: []platform.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, AtomicCurrentState: true, FixedReadViews: true, Open: func(ctx context.Context) (Journals, error) { return OpenJournal(ctx, address) }}
}

func FileProvider(directory string) JournalProvider {
	return JournalProvider{Module: platform.CapabilityModule{ID: "platform.storage.file", Title: "Local journal storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.file-journal.v1", Provides: []platform.CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}, Durable: true, Open: func(context.Context) (Journals, error) { return OpenFileJournal(directory) }}
}

func (d *Deployment) journalProvider() JournalProvider {
	if d.Storage != nil {
		return *d.Storage
	}
	if d.Profile == "lightweight" {
		return FileProvider(filepath.Join(d.Data, "journal"))
	}
	return PostgresProvider(d.Database)
}

func (d *Deployment) openJournals(ctx context.Context) (Journals, error) {
	provider := d.journalProvider()
	if err := provider.Module.Check(); err != nil {
		return nil, err
	}
	if !slices.Contains(provider.Module.Provides, platform.CapabilityContract{ID: "platform.storage", Version: "1", Semantics: "1"}) {
		return nil, fmt.Errorf("selected storage does not implement platform.storage/1 semantics 1")
	}
	if !provider.Durable || provider.Open == nil || provider.Module.Scope != "deployment" || provider.Module.StateFormat == "" {
		return nil, fmt.Errorf("storage must declare a durable deployment scope, restore format and opening factory")
	}
	if d.Profile != "lightweight" && (!provider.AtomicCurrentState || !provider.FixedReadViews) {
		return nil, fmt.Errorf("delivery storage needs atomic current state and fixed-position read views")
	}
	store, err := provider.Open(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("selected storage returned no journal")
	}
	if provider.AtomicCurrentState {
		_, state := store.(journal.RuntimeState)
		_, recovery := store.(journal.RuntimeStateRecovery)
		_, unique := store.(journal.UniqueState)
		if !state || !recovery || !unique {
			store.Close()
			return nil, fmt.Errorf("selected storage lacks its declared accepted state, recovery or unique-key contract")
		}
	}
	if provider.FixedReadViews {
		if _, ok := store.(journal.StateReadViews); !ok {
			store.Close()
			return nil, fmt.Errorf("selected storage lacks its declared fixed read views")
		}
	}
	return store, nil
}
