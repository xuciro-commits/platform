package store

import (
	"context"
	"fmt"
	"slices"

	"platformserver/platform"
)

type JournalProvider struct {
	Module             platform.CapabilityModule
	Durable            bool
	AtomicCurrentState bool
	FixedReadViews     bool
	Open               func(context.Context) (Journals, error)
}

func (p JournalProvider) OpenChecked(ctx context.Context, lightweight bool) (Journals, error) {
	if err := p.Module.Check(); err != nil {
		return nil, err
	}
	if !slices.Contains(p.Module.Provides, platform.CapabilityContract{ID: "platform.storage", Version: "1", Semantics: "1"}) {
		return nil, fmt.Errorf("selected storage does not implement platform.storage/1 semantics 1")
	}
	if !p.Durable || p.Open == nil || p.Module.Scope != "deployment" || p.Module.StateFormat == "" {
		return nil, fmt.Errorf("storage must declare a durable deployment scope, restore format and opening factory")
	}
	if !lightweight && (!p.AtomicCurrentState || !p.FixedReadViews) {
		return nil, fmt.Errorf("delivery storage needs atomic current state and fixed-position read views")
	}
	journals, err := p.Open(ctx)
	if err != nil {
		return nil, err
	}
	if journals == nil {
		return nil, fmt.Errorf("selected storage returned no journal")
	}
	if p.AtomicCurrentState {
		_, state := journals.(RuntimeState)
		_, recovery := journals.(RuntimeStateRecovery)
		_, unique := journals.(UniqueState)
		if !state || !recovery || !unique {
			journals.Close()
			return nil, fmt.Errorf("selected storage lacks its declared accepted state, recovery or unique-key contract")
		}
	}
	if p.FixedReadViews {
		if _, ok := journals.(StateReadViews); !ok {
			journals.Close()
			return nil, fmt.Errorf("selected storage lacks its declared fixed read views")
		}
	}
	return journals, nil
}
