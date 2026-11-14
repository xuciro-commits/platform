package host

import (
	"platformserver/kernel/records"
	"time"

	"platformkernel/kernel"
	enterprisemodel "platformserver/services/security/organization"

	"platformserver/platform"
)

func (t *Tenant) enterpriseUsage(store *records.Store) enterprisemodel.ExternalUsage {
	usage := enterprisemodel.ExternalUsage{Tenant: t.ID, Store: store, Rules: t.services.Rules, Admit: t.Admits, Model: t.tenantLinkModel, Caller: t.objectCaller,
		Paths: t.enterpriseHistoricalReferencePaths,
		Record: func(m platform.Member, typ, id string, now time.Time) (any, bool) {
			view, err := t.recordOf(m, typ, id, now)
			return view.Record, err == nil
		},
		Root: func(c platform.Caller) string { return t.setting(c, "enterpriseRoot") }}
	if t.identityOwner != nil {
		usage.GrantUsage = t.identityOwner.UnitGrantUsage
	}
	return usage
}

func (t *Tenant) checkEnterpriseUsage(c platform.Caller, store *records.Store, id, until string, deleting bool, now time.Time) *kernel.Error {
	return t.enterpriseUsage(store).Check(c, id, until, deleting, now)
}

func (t *Tenant) enterpriseUsageRead(m platform.Member, id, until string, now time.Time) (enterprisemodel.EnterpriseUsage, *kernel.Error) {
	return t.enterpriseUsage(t.Projection).Read(m, id, until, now)
}
