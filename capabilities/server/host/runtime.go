package host

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
)

type runtime struct {
	runtimekernel.SharedRuntime
	t      *Tenant
	scoped bool
}

func (r runtime) ReadScoped() bool { return r.scoped || r.t != nil && r.t.readOrigin != nil }

var _ platform.Runtime = runtime{}

func (r runtime) CheckEnterpriseUsage(c platform.Caller, id, until string, deleting bool, now time.Time) *kernel.Error {
	return r.t.checkEnterpriseUsage(c, r.t.Projection, id, until, deleting, now)
}
func (r runtime) CheckEnterpriseTenantConnection(c platform.Caller, id string, now time.Time) *kernel.Error {
	return r.t.checkTenantConnection(c, id, now)
}

func (r runtime) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	return r.t.newStagedDecision().runProbe(c, decide)
}

func (r runtime) Publish(c platform.Caller, record *pb.ChangeRecord) {
	r.t.publish(platform.Event{App: c.App, Record: record})
}

func (r runtime) Probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	return r.t.router().Probe(c, protocol, action, id, payload, now)
}

func (r runtime) Request(c platform.Caller, rec *pb.ChangeRecord, q platform.Request) {
	if !c.Replaying {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		r.t.askModel(c, rec, q)
	}
}

func (r runtime) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	return r.t.router().Query(c, protocol, read, provider...)
}

func (r runtime) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	return r.t.notify(c, n, now, to)
}

func (r runtime) Setting(c platform.Caller, name string) string { return r.t.setting(c, name) }

func (r runtime) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	return r.t.emitFor(c, kind, key, entity, data, now)
}

func (r runtime) roles() runtimekernel.OptionalRoles {
	return runtimekernel.OptionalRoles{Directory: r.t.directory, Linker: r.t.linker, Tasks: r.t.tasks}
}

func (r runtime) Units(c platform.Caller, structure string, now time.Time) []string {
	return r.roles().MemberUnits(c, structure, now)
}

func (r runtime) Element(_ platform.Caller, id string, now time.Time) (platform.ElementInfo, bool) {
	return r.roles().Element(id, now)
}

func (r runtime) Related(_ platform.Caller, element, stereotype string, outgoing bool, now time.Time) []string {
	return r.roles().Related(element, stereotype, outgoing, now)
}

func (r runtime) Links(c platform.Caller, entity string) []string { return r.roles().Links(c, entity) }

func (r runtime) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	return r.roles().Link(c, from, to, key, now)
}

func (r runtime) Assign(c platform.Caller, rec *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	return r.roles().Assign(c, rec, a)
}

func (r runtime) Next(c platform.Caller, rec *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error) {
	return r.t.Sequences.Next(rec, c.App, r.t.App(c.App), sequence, date)
}

func (r runtime) Readable(c platform.Caller, ref string) bool {
	return r.t.readable(c.Member, ref, time.Now())
}

func (r runtime) BoundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	return r.t.router().BoundProvider(c, protocol)
}

func (r runtime) Seen(c platform.Caller, keys ...string) { r.t.Notices.MarkKeysRead(c.App, keys) }
func (r runtime) AuthorizationEngine() platform.AuthorizationEngine {
	return r.t.services.Authorization
}
func (d *stagedDecision) AuthorizationEngine() platform.AuthorizationEngine {
	return d.tenant.services.Authorization
}

func (r runtime) Rules() platform.RuleEngine         { return r.t.services.Rules }
func (d *stagedDecision) Rules() platform.RuleEngine { return d.tenant.services.Rules }

func (t *Tenant) RuntimeCapabilities() platform.RuntimeCapabilities { return t.services }
