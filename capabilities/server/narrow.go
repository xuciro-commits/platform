package platformserver

import (
	"encoding"
	"encoding/json"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Derived content is never wider than its sources (see docs/Architecture.md).
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

// mayRead reports whether m may read a source now, remembering each answer: a
// record ("<type>/<id>"), one field of it ("<type>/<id>#<field>") or a named
// app read ("read:<name>"). held says the record store's lock is already held.
func (t *Tenant) mayRead(m platform.Member, now time.Time, held bool) func(ref string) bool {
	return t.mayReadIn(t.records, m, now, held)
}

func (t *Tenant) mayReadIn(store *recordStore, m platform.Member, now time.Time, held bool) func(ref string) bool {
	seen := map[string]bool{}
	walk := newAuthorizationWalk()
	return func(ref string) bool {
		if ref == "" {
			return true
		}
		if ok, known := seen[ref]; known {
			return ok
		}
		if name, isRead := strings.CutPrefix(ref, "read:"); isRead {
			ok := t.mayCallRead(m, name)
			seen[ref] = ok
			return ok
		}
		record, field, _ := strings.Cut(ref, "#")
		typ, id, _ := strings.Cut(record, "/")
		if !held {
			store.mu.Lock()
		}
		et := store.types[typ]
		if !held {
			store.mu.Unlock()
		}
		app := ""
		if et != nil {
			app = et.info.App
		}
		c := platform.NewCaller(runtime{t}, m, app, false, false)
		fields := []string{}
		if field != "" {
			fields = append(fields, field)
		}
		decision := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, now, held, 0, walk)
		ok := decision.Allow && len(decision.Withheld) == 0
		seen[ref] = ok
		return ok
	}
}

// mayCallRead reports whether m may call a named app read now, by the rule
// Tenant.Read applies: a role in the app that declares it, or a read its
// manifest opens to every member. A source may name a read instead of a record
// when what a read answers with is not records.
func (t *Tenant) mayCallRead(m platform.Member, name string) bool {
	a := t.owner["read:"+name]
	return a != nil && t.authorizeEntry(m, a.Manifest().ID, name, "read", Now()).Allow
}

// narrowable are the fields a type's derivations may leave out.
func (t *Tenant) narrowable(et *entityType) []string {
	var out []string
	for _, d := range et.info.Derived {
		for _, path := range d.Fields {
			if len(d.List) == 0 {
				if f := fieldName(et.info.Go, path); f != "" {
					out = append(out, f)
				}
			}
		}
		if len(d.List) > 0 {
			if f := fieldName(et.info.Go, d.List); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// fieldName is the JSON name of a field index path in a struct.
func fieldName(t reflect.Type, path []int) string {
	if len(path) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(t.FieldByIndex(path).Tag.Get("json"), ",")
	return name
}

// narrowed is v as m may read it now: every record within it keeps only the
// fields m's role reads, and the app that derives content from other records
// leaves out what m may not read at the source. The host applies it wherever
// records leave for a member — a record page, a record's detail, every app
// read — so no app, page or builder carries a second filter (AGENTS.md rule 6).
func (t *Tenant) narrowed(m platform.Member, v any, now time.Time, held bool) any {
	return t.narrowedSource(m, v, now, held, "")
}

func (t *Tenant) narrowedSource(m platform.Member, v any, now time.Time, held bool, source string) any {
	if v == nil {
		return v
	}
	w := t.narrower(m, now, held)
	w.derivedContext = true
	out, changed := w.valueFrom(reflect.ValueOf(v), originalSource(source))
	if w.opaque {
		return nil
	}
	if !changed {
		return v
	}
	return out.Interface()
}

// narrower narrows what leaves for one member at one moment, remembering each
// answer about a record and each type's restricted fields.
func (t *Tenant) narrower(m platform.Member, now time.Time, held bool) *narrower {
	return t.narrowerIn(t.records, m, now, held)
}

func (t *Tenant) narrowerIn(store *recordStore, m platform.Member, now time.Time, held bool) *narrower {
	m = t.authorizationMember(m)
	return &narrower{t: t, store: store, m: m, at: now, may: t.mayReadIn(store, m, now, held), held: held, hidden: map[reflect.Type][]platform.FieldInfo{}, decisions: map[string]platform.AuthzDecision{}, pointers: map[uintptr]bool{}}
}

type narrower struct {
	derivedContext bool
	t              *Tenant
	store          *recordStore
	m              platform.Member
	at             time.Time
	decisions      map[string]platform.AuthzDecision
	may            func(ref string) bool
	held           bool
	hidden         map[reflect.Type][]platform.FieldInfo
	opaque         bool
	pointers       map[uintptr]bool
}

// entity is the declared type of a Go struct type, or nil.
func (w *narrower) entity(typ reflect.Type) *entityType {
	if !w.held {
		w.store.mu.Lock()
		defer w.store.mu.Unlock()
	}
	return w.store.byGo[typ]
}

// value narrows what it finds inside v — records in slices, maps, pointers,
// interfaces and other structs — and says whether anything changed.
func originalSource(source string) bool {
	return source == "record" || source == "user-input" || source == "external-input"
}

// valueFrom carries a declaration only through unregistered DTO/JSON content.
// A registered entity always resolves its own authorization and provenance.
func (w *narrower) valueFrom(v reflect.Value, declared bool) (reflect.Value, bool) {
	return w.project(v, declared, 0)
}

func (w *narrower) project(v reflect.Value, declared bool, depth int) (reflect.Value, bool) {
	if depth > 16 {
		return reflect.Zero(v.Type()), !v.IsZero()
	}
	if w.derivedContext && !declared && opaqueProjection(v) {
		// A serializer can emit content absent from its exported fields.
		// An undeclared opaque result cannot be projected by reflection.
		w.opaque = true
		return reflect.Zero(v.Type()), true
	}
	if v.IsValid() && v.Type() == reflect.TypeFor[json.RawMessage]() {
		if v.IsNil() {
			return v, false
		}
		data, err := platform.DecodeValue(v.Bytes(), 1<<20)
		if err != nil {
			return reflect.ValueOf(json.RawMessage("null")), true
		}
		if data == nil {
			return v, false
		}
		out, changed := w.project(reflect.ValueOf(data), declared, depth)
		if !changed {
			return v, false
		}
		return reflect.ValueOf(json.RawMessage(platform.Raw(out.Interface()))), true
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return v, false
		}
		if v.Kind() == reflect.Pointer {
			identity := v.Pointer()
			if w.pointers[identity] {
				return reflect.Zero(v.Type()), true
			}
			w.pointers[identity] = true
			defer delete(w.pointers, identity)
		}
		inner, changed := w.project(v.Elem(), declared, depth)
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
		if v.Type().Elem().Kind() == reflect.Uint8 && w.derivedContext && !declared && v.Len() > 0 {
			return reflect.Zero(v.Type()), true
		}
		typ := v.Type()
		if v.Kind() == reflect.Array {
			typ = reflect.SliceOf(typ.Elem())
		}
		out, changed := reflect.MakeSlice(typ, 0, v.Len()), false
		for i := range v.Len() {
			if !w.permitsRecord(v.Index(i)) {
				changed = true
				continue
			}
			x, c := w.project(v.Index(i), declared, depth+1)
			changed = changed || c
			out = reflect.Append(out, x)
		}
		if !changed {
			return v, false
		}
		if v.Kind() == reflect.Array {
			array := reflect.New(v.Type()).Elem()
			reflect.Copy(array, out)
			return array, true
		}
		return out, true
	case reflect.Map:
		out, changed := reflect.MakeMapWithSize(v.Type(), v.Len()), false
		for _, k := range v.MapKeys() {
			if w.derivedContext && !declared && k.Kind() == reflect.String && !w.isRecord(v.MapIndex(k)) {
				// Arbitrary map keys can carry prose too. Only a declared
				// original/derived JSON container may expose that namespace.
				changed = true
				continue
			}
			if !w.permitsRecord(v.MapIndex(k)) {
				changed = true
				continue
			}
			x, c := w.project(v.MapIndex(k), declared, depth+1)
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
			field := v.Type().Field(i)
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			allowed := declared
			source, from := field.Tag.Get("source"), field.Tag.Get("from")
			if source != "" || from != "" {
				allowed = originalSource(source)
				if from != "" {
					refs := projectionRefs(v, from)
					allowed = (allowed || source == "generated") && len(refs) > 0 && w.readable(refs)
				}
			}
			var x reflect.Value
			var c bool
			if from != "" && !allowed || source != "" && !allowed {
				w.opaque = w.opaque || opaqueProjection(v.Field(i))
				x, c = reflect.Zero(v.Field(i).Type()), !v.Field(i).IsZero()
			} else {
				x, c = w.project(v.Field(i), allowed, depth+1)
			}
			if c {
				out.Field(i).Set(x)
				changed = true
			}
		}
		if changed {
			if withheld := out.FieldByName("Withheld"); withheld.IsValid() && withheld.CanSet() && withheld.Kind() == reflect.Bool {
				withheld.SetBool(true)
			}
		}
		return out, changed
	case reflect.String:
		if w.derivedContext && !declared && v.Type() != reflect.TypeFor[json.Number]() && v.Len() > 0 {
			return reflect.Zero(v.Type()), true
		}
	}
	return v, false
}

func opaqueProjection(v reflect.Value) bool {
	if !v.CanInterface() {
		return false
	}
	typ := v.Type()
	if typ == reflect.TypeFor[json.RawMessage]() {
		return false
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[time.Time]() {
		return false
	}
	_, marshalsJSON := v.Interface().(json.Marshaler)
	_, marshalsText := v.Interface().(encoding.TextMarshaler)
	return marshalsJSON || marshalsText
}

func (w *narrower) isRecord(v reflect.Value) bool {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Kind() == reflect.Struct && w.entity(v.Type()) != nil
}

// projectionRefs uses one DTO field holding a ref or []refs. The optional
// #field suffix rechecks the exact source field as well as its row; it does
// not introduce another relationship traversal or authorization evaluator.
func projectionRefs(v reflect.Value, from string) []string {
	name, suffix, field := strings.Cut(from, "#")
	at := v.FieldByName(name)
	if !at.IsValid() {
		return nil
	}
	refs := []string{}
	add := func(ref string) {
		if ref != "" {
			if field {
				ref += "#" + suffix
			}
			refs = append(refs, ref)
		}
	}
	switch at.Kind() {
	case reflect.String:
		add(at.String())
	case reflect.Slice:
		for i := range at.Len() {
			if at.Index(i).Kind() != reflect.String {
				return nil
			}
			add(at.Index(i).String())
		}
	}
	return refs
}

// record narrows one record: first the fields m's role may not read, then the
// content the type declares it took from other records.
func (w *narrower) record(et *entityType, v reflect.Value) (reflect.Value, bool) {
	if !w.permitsRecord(v) {
		return reflect.Zero(v.Type()), true
	}
	hidden, known := w.hidden[et.info.Go]
	if !known {
		_, hidden = viewOf(w.m, et)
		w.hidden[et.info.Go] = hidden
	}
	changed := false
	out := v
	if len(hidden) > 0 {
		out, changed = reflect.ValueOf(records.Mask(et.info, v, hidden)), true
	}
	if !et.info.HasReadSources() && len(et.info.VersionPins) == 0 && !w.derivedContext {
		return out, changed
	}
	derived, narrowed := w.derive(et, out)
	if narrowed {
		return derived, true
	}
	return out, changed
}

func (w *narrower) permitsRecord(value reflect.Value) bool {
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return true
	}
	et := w.entity(value.Type())
	if et == nil {
		for i := range value.NumField() {
			field := value.Type().Field(i)
			from := field.Tag.Get("from")
			if !field.IsExported() || field.Tag.Get("source") != "record" || from == "" || strings.Contains(from, "#") {
				continue
			}
			refs := projectionRefs(value, from)
			if len(refs) == 0 && !value.Field(i).IsZero() || !w.readable(refs) {
				return false
			}
		}
		return true
	}
	copy := records.Copy(et.info.Go, value.Interface())
	id := records.Header(copy).ID
	if id == "" {
		return true
	} // an input shape carries no stored row identity
	key := et.info.Type + "/" + id
	if decision, known := w.decisions[key]; known {
		return decision.Allow
	}
	c := platform.NewCaller(runtime{w.t}, w.m, et.info.App, false, false)
	decision := w.t.authorizeFrom(w.store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.info.Type, Record: id}, w.at, w.held)
	w.decisions[key] = decision
	return decision.Allow
}

// derive applies a type's derivations to a copy of the record: content whose
// source the reader may not read now is emptied, or its element left out, and
// the declared withheld field says so.
func (w *narrower) derive(et *entityType, v reflect.Value) (reflect.Value, bool) {
	out := records.Copy(et.info.Go, v.Interface())
	self := et.info.Type + "/" + records.Header(out).ID
	readable := func(refs []string) bool {
		for _, ref := range refs {
			if ref != self && !w.may(ref) {
				return false
			}
		}
		return true
	}
	withheld, every := false, []string{}
	for _, field := range et.info.Fields {
		if field.AliasOf != "" && !w.may(self+"#"+field.AliasOf) {
			out.FieldByIndex(field.Index).SetZero()
			withheld = true
		}
	}
	if w.derivedContext {
		for _, field := range et.info.Fields {
			if field.Type != "text" && field.Type != "longtext" {
				continue
			}
			declared := slices.ContainsFunc(et.info.Derived, func(d platform.DerivationInfo) bool {
				return len(d.List) == 0 && slices.ContainsFunc(d.Fields, func(path []int) bool { return slices.Equal(path, field.Index) })
			})
			if field.SourceType == "" && !declared || field.SourceType == "generated" && !declared {
				out.FieldByIndex(field.Index).SetZero()
				withheld = true
			}
		}
	}
	pins := records.Header(out).VersionPins
	visiblePins := []platform.RecordVersionPin{}
	for _, pin := range pins {
		reference, visible := et.info.Field(pin.Reference)
		version, known := et.info.Field(pin.VersionField)
		typ, _, _ := strings.Cut(pin.Source, "/")
		if !w.held {
			w.store.mu.Lock()
		}
		source := w.store.types[typ]
		if !w.held {
			w.store.mu.Unlock()
		}
		if source == nil || source.info.Versioning == nil || !visible || !known || !reference.ReadsAny(w.m.RolesIn(et.info.App)) || !version.ReadsAny(w.m.RolesIn(et.info.App)) || !w.may(pin.Source+"#"+source.info.Versioning.Version) {
			if known {
				out.FieldByIndex(version.Index).SetZero()
			}
			withheld = true
			continue
		}
		visiblePins = append(visiblePins, pin)
	}
	records.Header(out).VersionPins = visiblePins
	for _, d := range et.info.Derived { // every source the type names, for "*"
		if !d.All {
			every = append(every, w.sources(out, d)...)
		}
	}
	for _, d := range et.info.Derived {
		fieldsToEmpty := func(refs []string, value reflect.Value, typ reflect.Type) ([][]int, bool) {
			if len(refs) > 0 {
				return d.Fields, !readable(refs)
			}
			if !w.derivedContext {
				return nil, false // the authority's original record is not a copied read
			}
			if d.Element {
				return nil, true
			}
			missing := [][]int{}
			for _, path := range d.Fields {
				if typ == et.info.Go && slices.ContainsFunc(et.info.Derived, func(other platform.DerivationInfo) bool {
					return other.Element && slices.Equal(other.List, path)
				}) {
					continue // each child is independently kept or dropped by its declared source
				}
				field := typ.FieldByIndex(path)
				source := field.Tag.Get("source")
				if typ == et.info.Go {
					if info, known := et.info.Field(fieldName(typ, path)); known {
						source = info.SourceType
					}
				}
				if !originalSource(source) && !value.FieldByIndex(path).IsZero() {
					missing = append(missing, path)
				}
			}
			return missing, len(missing) > 0
		}
		switch {
		case d.All:
			if fields, denied := fieldsToEmpty(every, out, et.info.Go); denied {
				withheld = true
				empty(out, fields)
			}
		case len(d.List) == 0:
			refs := w.refsAt(out, d.Refs)
			if fields, denied := fieldsToEmpty(refs, out, et.info.Go); denied {
				withheld = true
				empty(out, fields)
			}
		default:
			list := out.FieldByIndex(d.List)
			kept := reflect.MakeSlice(list.Type(), 0, list.Len())
			for i := range list.Len() {
				element := list.Index(i)
				refs := w.refsAt(element, d.Refs)
				fields, denied := fieldsToEmpty(refs, element, element.Type())
				if !denied {
					kept = reflect.Append(kept, element)
					continue
				}
				withheld = true
				if d.Element {
					continue // the element is left out altogether
				}
				narrowed := reflect.New(element.Type()).Elem()
				narrowed.Set(element)
				empty(narrowed, fields)
				kept = reflect.Append(kept, narrowed)
			}
			list.Set(kept)
		}
	}
	if !withheld {
		return v, false
	}
	if len(et.info.Withheld) > 0 {
		out.FieldByIndex(et.info.Withheld).SetBool(true)
	}
	return out, true
}

// sources are every record a derivation names, over a list's elements too.
func (w *narrower) sources(v reflect.Value, d platform.DerivationInfo) []string {
	if len(d.List) == 0 {
		return w.refsAt(v, d.Refs)
	}
	var out []string
	list := v.FieldByIndex(d.List)
	for i := range list.Len() {
		out = append(out, w.refsAt(list.Index(i), d.Refs)...)
	}
	return out
}

// refsAt reads the records a derivation's fields name: one field holding a ref
// or a list of them, or two fields joined as "<type>/<id>".
func (w *narrower) refsAt(v reflect.Value, paths [][]int) []string {
	if len(paths) == 2 {
		typ, id := v.FieldByIndex(paths[0]).String(), v.FieldByIndex(paths[1]).String()
		if typ == "" || id == "" {
			return nil
		}
		return []string{typ + "/" + id}
	}
	at := v.FieldByIndex(paths[0])
	if at.Kind() == reflect.Slice {
		var out []string
		for i := range at.Len() {
			if ref := at.Index(i).String(); ref != "" {
				out = append(out, ref)
			}
		}
		return out
	}
	if ref := at.String(); ref != "" {
		return []string{ref}
	}
	return nil
}

// empty sets the fields of a record or an element to their zero value.
func (w *narrower) readable(refs []string) bool {
	return !slices.ContainsFunc(refs, func(ref string) bool { return !w.may(ref) })
}

func empty(v reflect.Value, fields [][]int) {
	for _, path := range fields {
		v.FieldByIndex(path).SetZero()
	}
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
					field := v.Type().Field(i)
					if from := field.Tag.Get("from"); from != "" && !v.Field(i).IsZero() {
						out = append(out, projectionRefs(v, from)...)
					}
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(v))
	slices.Sort(out)
	return slices.Compact(out)
}
