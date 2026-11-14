package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"reflect"
	"strings"
	"time"
)

func (t *Tenant) Changed(e platform.Event) []string {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return t.Projection.Changed(e.App + "/" + e.Record.GetChangeId())
}

func (t *Tenant) held(ref string) (any, bool) { return t.Projection.Held(ref) }

func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.viewCache().Entities(gen, key); ok {
		return out
	}
	out := objects.EntityView(t.Projection, m, func(et *records.Type) bool { return build.ProjectHidden(t.Projection, t.ID, m, et, t.ResourceRole) }, t.services.Authorization)
	t.viewCache().StoreEntities(gen, key, out)
	return out
}

func (t *Tenant) readableIn(store *records.Store, m platform.Member, ref string, now time.Time) bool {
	return t.readableAs(store, platform.NewCaller(runtime{t: t}, m, "", false, false), ref, now)
}

func (t *Tenant) readableAs(store *records.Store, c platform.Caller, ref string, now time.Time) bool {
	typ, id, _ := strings.Cut(ref, "/")
	if typ == "enterprise.element" && t.directory != nil {
		_, ok := t.directory.Element(id, now.UTC().Format(time.DateOnly))
		return ok
	}
	et := store.Type(typ)
	if et == nil {
		return false
	}
	r := et.Rows().Get(id)
	if r == nil {
		return false
	}
	visible, err := t.visibleForCallerIn(store, platform.NewCaller(runtime{t: t}, c.Member, et.Info().App, c.Replaying, c.Automation), et, now)
	return err == nil && (visible == nil || visible(r.Value))
}

func (t *Tenant) readable(m platform.Member, ref string, now time.Time) bool {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return t.readableIn(t.Projection, m, ref, now)
}

func (t *Tenant) recordOf(m platform.Member, typ, id string, now time.Time) (objects.RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordOf(m, typ, id, now)
}

func (t *Tenant) PersonalReads() []platform.PersonalRead { return t.AuditLog.PersonalReads() }

func (t *Tenant) enterpriseReferencesRead(m platform.Member, element string, offset, limit int, now time.Time) ([]records.EnterpriseReferenceGroup, *kernel.Error) {
	return objects.EnterpriseReferencesRead(t.Projection, m, element, offset, limit, t.Admits, func(et *records.Type) (func(reflect.Value) bool, *kernel.Error) {
		return t.visibleIn(t.Projection, m, et, now)
	})
}

type RecordPage = objects.RecordPage

func (r runtime) Put(c platform.Caller, rec *pb.ChangeRecord, entity any) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new record write requires a private runtime")
	}
	return r.t.Projection.Put(c, rec, entity)
}

func (r runtime) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	return r.t.Projection.Get(c, t, id)
}

func (r runtime) Find(c platform.Caller, t reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	return objects.FindRows(r.t.Projection, c, t, q, time.Now())
}

func (r runtime) Check(c platform.Caller, entity any) *kernel.Error {
	return r.t.Projection.Check(c, entity, time.Now())
}
