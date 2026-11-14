package host

import (
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
)

func (t *Tenant) runtimeOwners() runtimekernel.RuntimeOwners {
	return runtimekernel.RuntimeOwners{Apps: t.apps, Sequences: t.Sequences.Clone, Ready: t.enableCurrentReadScopes,
		States: func(app string) platform.AcceptedStateApp { return t.states[app] }}
}

func (t *Tenant) installRuntimeHistory(store runtimekernel.AcceptedStateReader, position func() int64) {
	t.InstallRuntimeHistory(store, position, t.apps)
}
