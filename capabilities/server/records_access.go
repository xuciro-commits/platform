package platformserver

import (
	"platformkernel/kernel"
	"platformserver/internal/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

func viewOf(m platform.Member, et *entityType) (*entityType, []platform.FieldInfo) {
	info, hidden := records.FieldView(m, et.info)
	if len(hidden) == 0 {
		return et, nil
	}
	return &entityType{info: info, rows: et.rows}, hidden
}

// Entities are the entity types m may read: those of apps m holds a role in.
func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.records.types {
		roles := m.RolesIn(et.info.App)
		if len(roles) > 0 && et.info.Scope.LevelFor(roles) == platform.ScopeNone && et.info.Scope.Participants == nil {
			continue // a type none of these roles sees at all (ADR-0044 18b)
		}
		if len(roles) > 0 || et.info.Scope.Participants != nil || et.info.Scope.Through != nil { // participants, and what belongs to a record, are read without a role
			view, _ := viewOf(m, et)
			out = append(out, view.info)
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

func (t *Tenant) readableIn(store *recordStore, m platform.Member, ref string, now time.Time) bool {
	typ, id, _ := strings.Cut(ref, "/")
	if typ == "enterprise.element" && t.directory != nil { // the model is every member's to read (ADR-0022 D3)
		_, ok := t.directory.Element(id, now.UTC().Format(time.DateOnly))
		return ok
	}
	et := store.types[typ]
	if et == nil {
		return false
	}
	r := et.rows.Get(id)
	if r == nil {
		return false
	}
	visible, err := t.visibleIn(store, m, et, now)
	return err == nil && (visible == nil || visible(r.Value))
}

// Readable reports whether m may read the record ref ("<type>/<id>").
func (t *Tenant) Readable(m platform.Member, ref string, now time.Time) bool {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	return t.readableLocked(m, ref, now)
}

// visible is m's scope over a type's records on now's day (D4).
func (t *Tenant) visible(m platform.Member, et *entityType, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return t.visibleIn(t.records, m, et, now)
}
