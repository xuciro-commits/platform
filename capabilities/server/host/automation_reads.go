package host

import records "platformserver/kernel/records"

import (
	"encoding/json"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/functions/authoring"
	"platformserver/services/objects"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) queryReads(store *records.Store) objects.QueryReads {
	return objects.QueryReads{Store: store, Named: t.namedQuery, Schema: authoring.QueryInputSchema, Entity: t.Entity, Authorization: t.services.Authorization,
		Version: func(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, bool) {
			q, _, ok := t.memberQueryVersion(m, ref, version)
			return q, ok
		},
		Run: func(m platform.Member, q platform.NamedQuery, of string, now time.Time) (RecordPage, *kernel.Error) {
			return t.runDeclaredQueryFrom(store, m, q, of, now)
		}}
}

func (r runtime) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.queryReads(r.t.Projection).Query(c, app, name, inputs, now)
}
func (d *stagedDecision) ReadQuery(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.queryReads(d.Records).Query(c, app, name, inputs, now)
}
func (r runtime) ReadQueryVersion(c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.queryReads(r.t.Projection).QueryVersion(c, app, name, version, inputs, now)
}
func (d *stagedDecision) ReadQueryVersion(c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.queryReads(d.Records).QueryVersion(c, app, name, version, inputs, now)
}
func (r runtime) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.objectOwner(r.t.Projection).ReadRecordFrom(r.t.Projection, c, typ, id, now)
}
func (d *stagedDecision) ReadRecord(c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.objectOwner(d.Records).ReadRecordFrom(d.Records, c, typ, id, now)
}

func (t *Tenant) readRecordPathFrom(store *records.Store, c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return automation.ReadRecordPath(c, typ, id, path, now, t.Entity, func(typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
		return t.objectOwner(store).ReadRecordFrom(store, c, typ, id, at)
	})
}

func (r runtime) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return r.t.readRecordPathFrom(r.t.Projection, c, typ, id, path, now)
}
func (d *stagedDecision) ReadRecordPath(c platform.Caller, typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	return d.tenant.readRecordPathFrom(d.Records, c, typ, id, path, now)
}

func (r runtime) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return r.t.queryReads(r.t.Projection).RuleQuery(c, set, input, now)
}
func (d *stagedDecision) ReadRuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	return d.tenant.queryReads(d.Records).RuleQuery(c, set, input, now)
}
