package host

import (
	"context"

	runtimekernel "platformserver/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (t *Tenant) runtimeOwners() runtimekernel.RuntimeOwners {
	return runtimekernel.RuntimeOwners{Apps: t.apps, Sequences: t.Sequences.Clone, Ready: t.enableCurrentReadScopes,
		States: func(app string) platform.AcceptedStateApp { return t.states[app] }}
}

func (t *Tenant) migrateRuntimeState(ctx context.Context, j Journals) error {
	return t.MigrateRuntimeState(ctx, j, nil, t.runtimeOwners())
}

func (t *Tenant) installRuntimeHistory(store runtimekernel.AcceptedStateReader, position func() int64) {
	t.InstallRuntimeHistory(store, position, t.apps)
}

func (t *Tenant) runtimeStateDeltaLocked() (persistence.StateDelta, error) {
	return t.RuntimeStateDelta(t.runtimeOwners())
}
