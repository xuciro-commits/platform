package platformserver

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Derived content is never wider than its sources (#130, docs/Testing.md N2).
// A record may carry what was taken from other records: an agent run's trace,
// the passages it cited, a payload it drafted. Authority changes after that
// content is journaled — an owner moves, a unit closes, a field becomes
// restricted, a grant is revoked — so the reader's authority over each source
// is checked again at every read, here, and not by each app, page or builder.

// admits refuses a member of another tenant. Every member-facing entry of the
// host asks here, once, instead of each app repeating the check (#130, N2).
func (t *Tenant) admits(m platform.Member) *kernel.Error {
	if m.Tenant != t.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return nil
}

// mayRead reports whether m may read a record ("<type>/<id>") or one field of
// it ("<type>/<id>#<field>") now, remembering each answer. held says the
// record store's lock is already held by the caller.
func (t *Tenant) mayRead(m platform.Member, now time.Time, held bool) func(ref string) bool {
	seen := map[string]bool{}
	return func(ref string) bool {
		if ref == "" {
			return true
		}
		if ok, known := seen[ref]; known {
			return ok
		}
		record, field, _ := strings.Cut(ref, "#")
		ok := false
		if held {
			ok = t.readableLocked(m, record, now)
		} else {
			ok = t.Readable(m, record, now)
		}
		if ok && field != "" {
			ok = t.readsField(m, record, field, held)
		}
		seen[ref] = ok
		return ok
	}
}

// readsField reports whether m's role in the owning app reads one field of a
// record's type (ADR-0028 D3): a citation of a restricted field is withheld
// even where the record itself is readable.
func (t *Tenant) readsField(m platform.Member, ref, field string, held bool) bool {
	typ, _, _ := strings.Cut(ref, "/")
	if !held {
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
	}
	et := t.records.types[typ]
	if et == nil {
		return false
	}
	f, ok := et.info.Field(field)
	return ok && f.Reads(m.Roles[et.info.App])
}

// narrowing is the app that owns a type and declares how its records narrow.
func (t *Tenant) narrowing(et *entityType) host.Narrowing {
	n, _ := t.app(et.info.App).(host.Narrowing)
	return n
}

// narrowable are the fields of a type that narrowing may leave out.
func (t *Tenant) narrowable(et *entityType) []string {
	n := t.narrowing(et)
	if n == nil {
		return nil
	}
	var out []string
	for _, name := range n.Narrowable() {
		if typ, field, ok := strings.Cut(name, "."); ok && typ == et.info.Type {
			out = append(out, field)
		}
	}
	return out
}

// narrowed is v as m may read it now: every record within it keeps only the
// fields m's role reads, and the app that derives content from other records
// leaves out what m may not read at the source. The host applies it wherever
// records leave for a member — a record page, a record's detail, every app
// read — so no app, page or builder carries a second filter (rule 11).
func (t *Tenant) narrowed(m platform.Member, v any, now time.Time, held bool) any {
	if v == nil {
		return v
	}
	w := &narrower{t: t, m: m, may: t.mayRead(m, now, held), held: held, hidden: map[reflect.Type][]platform.FieldInfo{}}
	out, changed := w.value(reflect.ValueOf(v))
	if !changed {
		return v
	}
	return out.Interface()
}

type narrower struct {
	t      *Tenant
	m      platform.Member
	may    func(ref string) bool
	held   bool
	hidden map[reflect.Type][]platform.FieldInfo
}

// entity is the declared type of a Go struct type, or nil.
func (w *narrower) entity(typ reflect.Type) *entityType {
	if !w.held {
		w.t.records.mu.Lock()
		defer w.t.records.mu.Unlock()
	}
	return w.t.records.byGo[typ]
}

// value narrows what it finds inside v — records in slices, maps, pointers,
// interfaces and other structs — and says whether anything changed.
func (w *narrower) value(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return v, false
		}
		inner, changed := w.value(v.Elem())
		if !changed {
			return v, false
		}
		if v.Kind() == reflect.Interface {
			out := reflect.New(v.Type()).Elem()
			out.Set(inner)
			return out, true
		}
		p := reflect.New(inner.Type())
		p.Elem().Set(inner)
		return p, true
	case reflect.Slice, reflect.Array:
		out, changed := reflect.MakeSlice(reflect.SliceOf(v.Type().Elem()), v.Len(), v.Len()), false
		for i := range v.Len() {
			x, c := w.value(v.Index(i))
			changed = changed || c
			out.Index(i).Set(x)
		}
		if !changed || v.Kind() == reflect.Array {
			return v, false // an array is left alone; records are never held in one
		}
		return out, true
	case reflect.Map:
		out, changed := reflect.MakeMapWithSize(v.Type(), v.Len()), false
		for _, k := range v.MapKeys() {
			x, c := w.value(v.MapIndex(k))
			changed = changed || c
			out.SetMapIndex(k, x)
		}
		if !changed {
			return v, false
		}
		return out, true
	case reflect.Struct:
		if _, ok := v.Interface().(time.Time); ok {
			return v, false
		}
		if et := w.entity(v.Type()); et != nil {
			return w.record(et, v)
		}
		out, changed := reflect.New(v.Type()).Elem(), false
		out.Set(v)
		for i := range v.NumField() {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			x, c := w.value(v.Field(i))
			if c {
				out.Field(i).Set(x)
				changed = true
			}
		}
		return out, changed
	}
	return v, false
}

// record narrows one record: the fields m's role may not read, then the app's
// own narrowing of what the record derives from other records.
func (w *narrower) record(et *entityType, v reflect.Value) (reflect.Value, bool) {
	hidden, known := w.hidden[et.info.Go]
	if !known {
		_, hidden = viewOf(w.m, et)
		w.hidden[et.info.Go] = hidden
	}
	n, changed := w.t.narrowing(et), false
	out := v
	if len(hidden) > 0 {
		out, changed = reflect.ValueOf(masked(et, v, hidden)), true
	}
	if n == nil {
		return out, changed
	}
	narrow := n.Narrow(out.Interface(), w.may)
	if reflect.TypeOf(narrow) != et.info.Go {
		return out, changed
	}
	if raw, _ := json.Marshal(narrow); !equalJSON(raw, out.Interface()) {
		return reflect.ValueOf(narrow), true
	}
	return out, changed
}

func equalJSON(raw []byte, v any) bool {
	other, _ := json.Marshal(v)
	return string(raw) == string(other)
}

// refsIn are the records ("<type>/<id>") a value carries, at any depth: what a
// derived read, an agent's tool or a report took its content from, so the same
// content is checked against them when it is read again (#130).
func (t *Tenant) refsIn(v any) []string {
	if v == nil {
		return nil
	}
	var out []string
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		case reflect.Map:
			for _, k := range v.MapKeys() {
				walk(v.MapIndex(k))
			}
		case reflect.Struct:
			if _, ok := v.Interface().(time.Time); ok {
				return
			}
			t.records.mu.Lock()
			et := t.records.byGo[v.Type()]
			t.records.mu.Unlock()
			if et != nil {
				if id := v.Field(0).Interface().(platform.Record).ID; id != "" {
					out = append(out, et.info.Type+"/"+id)
				}
				return
			}
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(v))
	slices.Sort(out)
	return slices.Compact(out)
}
