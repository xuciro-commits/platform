package platformserver

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// runtime is what the tenant does for its apps' callers (platform.Runtime):
// the only way from an app into the host.
type runtime struct{ t *Tenant }

var _ platform.Runtime = runtime{}

func (r runtime) Publish(c platform.Caller, record *pb.ChangeRecord) {
	r.t.publish(platform.Event{App: c.App, Record: record})
}

func (r runtime) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	return r.t.invoke(c, protocol, action, id, payload, key, correlation, now)
}

func (r runtime) Query(c platform.Caller, protocol, read string) ([]platform.ProviderResult, *kernel.Error) {
	return r.t.query(c, protocol, read)
}

func (r runtime) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	return r.t.notify(c, n, now, to)
}

func (r runtime) Setting(c platform.Caller, name string) string { return r.t.setting(c, name) }

func (r runtime) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	return r.t.emitFor(c, kind, key, entity, data, now)
}

func (r runtime) Units(c platform.Caller, structure string, now time.Time) []string {
	return r.t.unitsOf(c, structure, now)
}

func (r runtime) Links(c platform.Caller, entity string) []string { return r.t.linksOf(c, entity) }

func (r runtime) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	return r.t.link(c, from, to, key, now)
}

// Deliver accepts one batch from the calling connector (K8).
func (r runtime) Deliver(c platform.Caller, dataClass, from, to string, now time.Time) *kernel.Error {
	r.t.opsMu.Lock()
	defer r.t.opsMu.Unlock()
	return r.t.connectors.Deliver(c.Tenant, c.ID, dataClass, from, to, now)
}

func (r runtime) Assign(c platform.Caller, rec *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	return r.t.assign(c, rec, a)
}

func (r runtime) Probing() bool { return r.t.probing }
