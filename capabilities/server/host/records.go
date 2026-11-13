package host

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) Changed(e platform.Event) []string {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return t.Projection.Changed(e.App + "/" + e.Record.GetChangeId())
}

func (t *Tenant) held(ref string) (any, bool) { return t.Projection.Held(ref) }

func jsonOf(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.viewCache().Entities(gen, key); ok {
		return out
	}
	out := t.entityView(m)
	t.viewCache().StoreEntities(gen, key, out)
	return out
}

func (t *Tenant) entityView(m platform.Member) []platform.EntityInfo {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.Projection.Types() {
		if build.ProjectHidden(t.Projection, t.ID, m, et, t.ResourceRole) {
			continue
		}
		roles := m.RolesIn(et.Info().App)
		if len(roles) > 0 && et.Info().Scope.LevelFor(roles, t.services.Authorization) == platform.ScopeNone && et.Info().Scope.Participants == nil {
			continue
		}
		if len(roles) > 0 || et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil {
			view, _ := objects.ViewOf(m, et)
			out = append(out, view.Info())
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
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

func (t *Tenant) recordDetail(m platform.Member, typ, id string, now time.Time, sections ...string) (objects.RecordView, *kernel.Error) {
	return t.objectOwner(t.Projection).RecordDetail(m, typ, id, now, sections...)
}

func (t *Tenant) PersonalReads() []platform.PersonalRead { return t.audit.PersonalReads() }

func (t *Tenant) recordDeliveries(target string, now time.Time) []objects.RecordDelivery {
	matched := t.outbound.Select(func(x actions.Queued) bool { return x.Target == target })
	out := make([]objects.RecordDelivery, 0, len(matched))
	for i := len(matched) - 1; i >= 0; i-- {
		effect := matched[i].Effect
		out = append(out, objects.RecordDelivery{ID: effect.ID, Event: effect.Event, State: effect.State, At: effect.At, Message: effect.PublicMessage})
	}
	return out
}

func (t *Tenant) enterpriseReferencesRead(m platform.Member, element string, offset, limit int, now time.Time) ([]records.EnterpriseReferenceGroup, *kernel.Error) {
	if element == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reference read names the element")
	}
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return records.EnterpriseReferences(t.Projection, element, offset, limit, func(et *records.Type) (func(reflect.Value) bool, []platform.FieldInfo, bool) {
		visible, err := t.visibleIn(t.Projection, m, et, now)
		if err != nil {
			return nil, nil, false
		}
		_, hidden := objects.ViewOf(m, et)
		return visible, hidden, true
	}, m.RolesIn), nil
}

type RecordPage = objects.RecordPage

func (t *Tenant) recordsRead(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(t.Projection).Records(m, typ, q, now)
}

func (t *Tenant) recordsFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(store).RecordsFrom(store, m, typ, q, now)
}

func (t *Tenant) recordSelectionFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(store).SelectionFrom(store, m, typ, q, now)
}

func (t *Tenant) readRecordFrom(store *records.Store, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return t.objectOwner(store).ReadRecordFrom(store, c, typ, id, now)
}

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
