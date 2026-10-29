package platformserver

import (
	"encoding"
	"encoding/json"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) admits(m platform.Member) *kernel.Error {
	if m.Tenant != t.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return nil
}

func (t *Tenant) mayRead(m platform.Member, now time.Time, held bool) func(ref string) bool {
	return t.mayReadIn(t.records, m, now, held)
}

func (t *Tenant) mayReadIn(store *records.Store, m platform.Member, now time.Time, held bool) func(ref string) bool {
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
			store.Lock()
		}
		et := store.Type(typ)
		if !held {
			store.Unlock()
		}
		app := ""
		if et != nil {
			app = et.Info().App
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

func (t *Tenant) mayCallRead(m platform.Member, name string) bool {
	a := t.owner["read:"+name]
	return a != nil && t.authorizeEntry(m, a.Manifest().ID, name, "read", Now()).Allow
}

func (t *Tenant) narrowable(et *records.Type) []string {
	var out []string
	for _, d := range et.Info().Derived {
		for _, path := range d.Fields {
			if len(d.List) == 0 {
				if f := fieldName(et.Info().Go, path); f != "" {
					out = append(out, f)
				}
			}
		}
		if len(d.List) > 0 {
			if f := fieldName(et.Info().Go, d.List); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

func fieldName(t reflect.Type, path []int) string {
	if len(path) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(t.FieldByIndex(path).Tag.Get("json"), ",")
	return name
}

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

func (t *Tenant) narrower(m platform.Member, now time.Time, held bool) *narrower {
	return t.narrowerIn(t.records, m, now, held)
}

func (t *Tenant) narrowerIn(store *records.Store, m platform.Member, now time.Time, held bool) *narrower {
	m = t.authorizationMember(m)
	return &narrower{t: t, store: store, m: m, at: now, may: t.mayReadIn(store, m, now, held), held: held, hidden: map[reflect.Type][]platform.FieldInfo{}, decisions: map[string]platform.AuthzDecision{}, pointers: map[uintptr]bool{}}
}

type narrower struct {
	derivedContext bool
	t              *Tenant
	store          *records.Store
	m              platform.Member
	at             time.Time
	decisions      map[string]platform.AuthzDecision
	may            func(ref string) bool
	held           bool
	hidden         map[reflect.Type][]platform.FieldInfo
	opaque         bool
	pointers       map[uintptr]bool
}

func (w *narrower) entity(typ reflect.Type) *records.Type {
	if !w.held {
		w.store.Lock()
		defer w.store.Unlock()
	}
	return w.store.GoType(typ)
}

func originalSource(source string) bool {
	return source == "record" || source == "user-input" || source == "external-input"
}

func (w *narrower) valueFrom(v reflect.Value, declared bool) (reflect.Value, bool) {
	return w.project(v, declared, 0)
}

func (w *narrower) project(v reflect.Value, declared bool, depth int) (reflect.Value, bool) {
	if depth > 16 {
		return reflect.Zero(v.Type()), !v.IsZero()
	}
	if w.derivedContext && !declared && opaqueProjection(v) {
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

func (w *narrower) record(et *records.Type, v reflect.Value) (reflect.Value, bool) {
	if !w.permitsRecord(v) {
		return reflect.Zero(v.Type()), true
	}
	hidden, known := w.hidden[et.Info().Go]
	if !known {
		_, hidden = viewOf(w.m, et)
		w.hidden[et.Info().Go] = hidden
	}
	changed := false
	out := v
	if len(hidden) > 0 {
		out, changed = reflect.ValueOf(records.Mask(et.Info(), v, hidden)), true
	}
	if !et.Info().HasReadSources() && len(et.Info().VersionPins) == 0 && !w.derivedContext {
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
	copy := records.Copy(et.Info().Go, value.Interface())
	id := records.Header(copy).ID
	if id == "" {
		return true
	}
	key := et.Info().Type + "/" + id
	if decision, known := w.decisions[key]; known {
		return decision.Allow
	}
	c := platform.NewCaller(runtime{w.t}, w.m, et.Info().App, false, false)
	decision := w.t.authorizeFrom(w.store, c, platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: et.Info().Type, Record: id}, w.at, w.held)
	w.decisions[key] = decision
	return decision.Allow
}

func (w *narrower) derive(et *records.Type, v reflect.Value) (reflect.Value, bool) {
	out := records.Copy(et.Info().Go, v.Interface())
	self := et.Info().Type + "/" + records.Header(out).ID
	readable := func(refs []string) bool {
		for _, ref := range refs {
			if ref != self && !w.may(ref) {
				return false
			}
		}
		return true
	}
	withheld, every := false, []string{}
	for _, field := range et.Info().Fields {
		if field.AliasOf != "" && !w.may(self+"#"+field.AliasOf) {
			out.FieldByIndex(field.Index).SetZero()
			withheld = true
		}
	}
	if w.derivedContext {
		for _, field := range et.Info().Fields {
			if field.Type != "text" && field.Type != "longtext" {
				continue
			}
			declared := slices.ContainsFunc(et.Info().Derived, func(d platform.DerivationInfo) bool {
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
		reference, visible := et.Info().Field(pin.Reference)
		version, known := et.Info().Field(pin.VersionField)
		typ, _, _ := strings.Cut(pin.Source, "/")
		if !w.held {
			w.store.Lock()
		}
		source := w.store.Type(typ)
		if !w.held {
			w.store.Unlock()
		}
		if source == nil || source.Info().Versioning == nil || !visible || !known || !reference.ReadsAny(w.m.RolesIn(et.Info().App)) || !version.ReadsAny(w.m.RolesIn(et.Info().App)) || !w.may(pin.Source+"#"+source.Info().Versioning.Version) {
			if known {
				out.FieldByIndex(version.Index).SetZero()
			}
			withheld = true
			continue
		}
		visiblePins = append(visiblePins, pin)
	}
	records.Header(out).VersionPins = visiblePins
	for _, d := range et.Info().Derived {
		if !d.All {
			every = append(every, w.sources(out, d)...)
		}
	}
	for _, d := range et.Info().Derived {
		fieldsToEmpty := func(refs []string, value reflect.Value, typ reflect.Type) ([][]int, bool) {
			if len(refs) > 0 {
				return d.Fields, !readable(refs)
			}
			if !w.derivedContext {
				return nil, false
			}
			if d.Element {
				return nil, true
			}
			missing := [][]int{}
			for _, path := range d.Fields {
				if typ == et.Info().Go && slices.ContainsFunc(et.Info().Derived, func(other platform.DerivationInfo) bool {
					return other.Element && slices.Equal(other.List, path)
				}) {
					continue
				}
				field := typ.FieldByIndex(path)
				source := field.Tag.Get("source")
				if typ == et.Info().Go {
					if info, known := et.Info().Field(fieldName(typ, path)); known {
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
			if fields, denied := fieldsToEmpty(every, out, et.Info().Go); denied {
				withheld = true
				empty(out, fields)
			}
		case len(d.List) == 0:
			refs := w.refsAt(out, d.Refs)
			if fields, denied := fieldsToEmpty(refs, out, et.Info().Go); denied {
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
					continue
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
	if len(et.Info().Withheld) > 0 {
		out.FieldByIndex(et.Info().Withheld).SetBool(true)
	}
	return out, true
}

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

func (w *narrower) readable(refs []string) bool {
	return !slices.ContainsFunc(refs, func(ref string) bool { return !w.may(ref) })
}

func empty(v reflect.Value, fields [][]int) {
	for _, path := range fields {
		v.FieldByIndex(path).SetZero()
	}
}

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
			t.records.Lock()
			et := t.records.GoType(v.Type())
			t.records.Unlock()
			if et != nil {
				if id := v.Field(0).Interface().(platform.Record).ID; id != "" {
					out = append(out, et.Info().Type+"/"+id)
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
