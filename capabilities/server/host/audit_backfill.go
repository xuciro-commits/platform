package host

import runtimekernel "platformserver/kernel"

func (t *Tenant) backfillAudit(entries []Entry) {
	runtimekernel.BackfillAudit(t.ID, entries, t.AuditLog.Remember)
}
