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

func (r runtime) Probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	return r.t.probe(c, protocol, action, id, payload, now)
}

func (r runtime) Request(c platform.Caller, rec *pb.ChangeRecord, q platform.Request) {
	if c.Replaying {
		return // the journal holds what the request decided
	}
	n := 0 // the decision's requests are numbered for their keys
	for _, x := range r.t.requests {
		if x.record == rec {
			n++
		}
	}
	r.t.requests = append(r.t.requests, request{Request: q, caller: c, record: rec, n: n})
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
	if r.t.directory == nil {
		return nil
	}
	return r.t.directory.Units("member:"+c.ID, structure, now.UTC().Format(time.DateOnly))
}

func (r runtime) Links(c platform.Caller, entity string) []string {
	if r.t.linker == nil {
		return nil
	}
	return r.t.linker.Links(c, entity)
}

func (r runtime) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if r.t.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return r.t.linker.Link(c, from, to, key, now)
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
