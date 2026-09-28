package platformserver

import (
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// stagedDecision is the private view for ADR-0038 19a's first, generated
// create/edit family. Nothing calls it from Tenant.Submit yet: the durable
// result format and accepted-result recovery must exist before activation.
// Unlike runtime, it deliberately has no fallback to live mutators.
type stagedDecision struct {
	tenant  *Tenant
	records *recordStore
	logs    map[*platform.Ledger]*kernel.ChangeLog
	events  []platform.Event
}

var _ platform.Runtime = (*stagedDecision)(nil)

func (t *Tenant) newStagedDecision() *stagedDecision {
	return &stagedDecision{tenant: t, records: t.records.forkRecords(),
		logs: map[*platform.Ledger]*kernel.ChangeLog{}}
}

// DraftChanges is called under the ledger's lock. It keeps both the kernel
// receipt and the host record image private until a durable result exists.
func (d *stagedDecision) DraftChanges(l *platform.Ledger) *kernel.ChangeLog {
	if d.logs[l] == nil {
		d.logs[l] = l.Changes.Fork()
	}
	return d.logs[l]
}

func (d *stagedDecision) Put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	return d.records.put(c, r, entity)
}

func (d *stagedDecision) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	return d.records.get(c, typ, id)
}

func (d *stagedDecision) Check(c platform.Caller, entity any) *kernel.Error {
	return d.records.check(c, entity)
}

func (d *stagedDecision) Publish(c platform.Caller, record *pb.ChangeRecord) {
	d.records.mu.Lock()
	changed := slices.Clone(d.records.changed[c.App+"/"+record.GetChangeId()])
	d.records.mu.Unlock()
	d.events = append(d.events, platform.Event{App: c.App, Record: record, Changed: changed})
}

func (d *stagedDecision) Readable(c platform.Caller, ref string) bool {
	// In the first family, generated edit acts on a previously committed
	// record. A draft-created record cannot be edited in the same decision.
	return d.tenant.Readable(c.Member, ref, time.Now())
}

func (*stagedDecision) Probing() bool { return false }

// The first family excludes nested submissions and all other owned effects.
// A forbidden call must not silently escape to the live tenant.
func unsupportedStagedEffect() { panic("effect outside staged decision's supported family") }

func (*stagedDecision) Probe(platform.Caller, string, string, string, []byte, time.Time) *kernel.Error {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Request(platform.Caller, *pb.ChangeRecord, platform.Request) {
	unsupportedStagedEffect()
}
func (*stagedDecision) Query(platform.Caller, string, string) ([]platform.ProviderResult, *kernel.Error) {
	unsupportedStagedEffect()
	return nil, nil
}
func (*stagedDecision) Notify(platform.Caller, platform.Notification, time.Time, []platform.Recipient) []string {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Setting(platform.Caller, string) string {
	unsupportedStagedEffect()
	return ""
}
func (*stagedDecision) Emit(platform.Caller, string, string, string, any, time.Time) (int, *kernel.Error) {
	unsupportedStagedEffect()
	return 0, nil
}
func (*stagedDecision) Units(platform.Caller, string, time.Time) []string {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Links(platform.Caller, string) []string {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Link(platform.Caller, *pb.EntityRef, *pb.EntityRef, string, time.Time) *kernel.Error {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Deliver(platform.Caller, string, string, string, time.Time) *kernel.Error {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Find(platform.Caller, reflect.Type, platform.Query) ([]any, int, *kernel.Error) {
	unsupportedStagedEffect()
	return nil, 0, nil
}
func (*stagedDecision) Assign(platform.Caller, *pb.ChangeRecord, platform.Assignment) *kernel.Error {
	unsupportedStagedEffect()
	return nil
}
func (*stagedDecision) Next(platform.Caller, *pb.ChangeRecord, string, time.Time) (string, *kernel.Error) {
	unsupportedStagedEffect()
	return "", nil
}
