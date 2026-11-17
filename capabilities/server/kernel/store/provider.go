package store

import (
	"context"
	"fmt"
	"slices"

	runtimeprotocol "platformserver/kernel/protocol"
)

type JournalProvider struct {
	Module             runtimeprotocol.CapabilityModule
	Durable            bool
	AtomicCurrentState bool
	FixedReadViews     bool
	Open               func(context.Context) (StorageBundle, error)
}

type StorageBundle struct {
	Journals
	RuntimeState
	RuntimeStateRecovery
	UniqueState
	StateReadViews
}

func (p JournalProvider) OpenChecked(ctx context.Context, lightweight bool) (Journals, error) {
	if err := p.Module.Check(); err != nil {
		return nil, err
	}
	if !slices.Contains(p.Module.Provides, runtimeprotocol.CapabilityContract{ID: "platform.storage", Version: "1", Semantics: "1"}) {
		return nil, fmt.Errorf("selected storage does not implement platform.storage/1 semantics 1")
	}
	if !p.Durable || p.Open == nil || p.Module.Scope != "deployment" || p.Module.StateFormat == "" {
		return nil, fmt.Errorf("storage must declare a durable deployment scope, restore format and opening factory")
	}
	if !lightweight && (!p.AtomicCurrentState || !p.FixedReadViews) {
		return nil, fmt.Errorf("delivery storage needs atomic current state and fixed-position read views")
	}
	storage, err := p.Open(ctx)
	if err != nil {
		return nil, err
	}
	if storage.Journals == nil {
		return nil, fmt.Errorf("selected storage returned no journal")
	}
	if p.AtomicCurrentState && (storage.RuntimeState == nil || storage.RuntimeStateRecovery == nil || storage.UniqueState == nil) {
		storage.Journals.Close()
		return nil, fmt.Errorf("selected storage lacks its declared accepted state, recovery or unique-key contract")
	}
	if p.FixedReadViews && storage.StateReadViews == nil {
		storage.Journals.Close()
		return nil, fmt.Errorf("selected storage lacks its declared fixed read views")
	}
	return storage.Journals, nil
}

func FileBundle(j *File) StorageBundle {
	return StorageBundle{Journals: j}
}

func OpenFileBundle(dir string, identify Identify) (StorageBundle, error) {
	j, err := OpenFile(dir, identify)
	if err != nil {
		return StorageBundle{}, err
	}
	return FileBundle(j), nil
}

func PostgresBundle(j *Postgres) StorageBundle {
	return StorageBundle{Journals: j, RuntimeState: j, RuntimeStateRecovery: j, UniqueState: j, StateReadViews: j}
}
