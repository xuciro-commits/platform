package platformserver

import (
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

func viewOf(m platform.Member, et *records.Type) (*records.Type, []platform.FieldInfo) {
	info, hidden := records.FieldView(m, et.Info())
	if len(hidden) == 0 {
		return et, nil
	}
	return records.NewTypeView(info, et.Rows(), et.RetainedProjection()), hidden
}

// Entities are the entity types m may read: those of apps m holds a role in.
func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.views.Entities(gen, key); ok {
		return out
	}
	out := t.entityView(m)
	t.views.StoreEntities(gen, key, out)
	return out
}

func (t *Tenant) entityView(m platform.Member) []platform.EntityInfo {
	t.records.Lock()
	defer t.records.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.records.Types() {
		roles := m.RolesIn(et.Info().App)
		if len(roles) > 0 && et.Info().Scope.LevelFor(roles) == platform.ScopeNone && et.Info().Scope.Participants == nil {
			continue // a type none of these roles sees at all
		}
		if len(roles) > 0 || et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil { // participants, and what belongs to a record, are read without a role
			view, _ := viewOf(m, et)
			out = append(out, view.Info())
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

// readableLocked reports whether m may read the record ref ("<type>/<id>"),
// with the store's lock held.
func (t *Tenant) readableLocked(m platform.Member, ref string, now time.Time) bool {
	return t.readableIn(t.records, m, ref, now)
}

func (t *Tenant) readableIn(store *records.Store, m platform.Member, ref string, now time.Time) bool {
	typ, id, _ := strings.Cut(ref, "/")
	if typ == "enterprise.element" && t.directory != nil { // the model is every member's to read
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
	visible, err := t.visibleIn(store, m, et, now)
	return err == nil && (visible == nil || visible(r.Value))
}

// Readable reports whether m may read the record ref ("<type>/<id>").
func (t *Tenant) readable(m platform.Member, ref string, now time.Time) bool {
	t.records.Lock()
	defer t.records.Unlock()
	return t.readableLocked(m, ref, now)
}

// visible is m's scope over a type's records on now's day (D4).
func (t *Tenant) visible(m platform.Member, et *records.Type, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleIn(t.records, m, et, now)
}
