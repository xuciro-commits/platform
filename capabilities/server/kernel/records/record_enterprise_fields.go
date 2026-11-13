package records

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type EnterpriseElements interface {
	Element(id string, now time.Time) (platform.ElementInfo, bool)
}

func CheckEnterpriseReferences(elements EnterpriseElements, replaying bool, fields []platform.FieldInfo, value, previous reflect.Value, now time.Time) *kernel.Error {
	if replaying {
		return nil
	}
	for _, f := range fields {
		v := value.FieldByIndex(f.Index)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				continue
			}
			v = v.Elem()
		}
		if f.Type == "lines" {
			used := map[int]bool{}
			for i := 0; i < v.Len(); i++ {
				if err := CheckEnterpriseReferences(elements, replaying, f.Fields, v.Index(i), PreviousLine(previous, f, v.Index(i), used), now); err != nil {
					return err
				}
			}
		}
		if f.Ref != "enterprise.element" || v.IsZero() {
			continue
		}
		if previous.IsValid() && reflect.DeepEqual(value.FieldByIndex(f.Index).Interface(), previous.FieldByIndex(f.Index).Interface()) {
			continue
		}
		el, ok := elements.Element(v.String(), now)
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "{field} names an enterprise element that is not active: {element}", f.Title, v.String())
		}
		families := slices.Clone(f.Stereotypes)
		if f.Stereotype != "" {
			families = append(families, f.Stereotype)
		}
		if len(families) > 0 && !slices.Contains(families, el.Stereotype) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} names an enterprise element of an unsupported type", f.Title)
		}
	}
	return nil
}

func PreviousLine(record reflect.Value, field platform.FieldInfo, current reflect.Value, used map[int]bool) reflect.Value {
	if !record.IsValid() {
		return reflect.Value{}
	}
	lines := FieldValue(record, field.Index)
	if !lines.IsValid() || (lines.Kind() != reflect.Slice && lines.Kind() != reflect.Array) {
		return reflect.Value{}
	}
	for i := 0; i < lines.Len(); i++ {
		if used[i] {
			continue
		}
		same := true
		for _, column := range field.Fields {
			if column.Ref == "enterprise.element" && !reflect.DeepEqual(FieldValue(lines.Index(i), column.Index).Interface(), FieldValue(current, column.Index).Interface()) {
				same = false
				break
			}
		}
		if same {
			used[i] = true
			return lines.Index(i)
		}
	}
	return reflect.Value{}
}

type RefPath struct {
	Path       string
	Title      string
	Stereotype string
}

func DeclaresEnterpriseRef(fields []platform.FieldInfo) bool {
	for _, f := range fields {
		if len(f.Fields) > 0 && DeclaresEnterpriseRef(f.Fields) {
			return true
		}
		if f.Ref == "enterprise.element" {
			return true
		}
	}
	return false
}

func MatchingRefs(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string) []RefPath {
	return MatchingRefsWithAccess(fields, prefix, v, element, roles, false)
}

func MatchingRefsWithAccess(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string, all bool) []RefPath {
	var out []RefPath
	for _, f := range fields {
		if !all && !f.ReadsAny(roles) {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		fv := FieldValue(v, f.Index)
		if len(f.Fields) > 0 {
			if !fv.IsValid() || fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
				continue
			}
			for i := 0; i < fv.Len(); i++ {
				out = append(out, MatchingRefsWithAccess(f.Fields, path, fv.Index(i), element, roles, all)...)
			}
			continue
		}
		if f.Ref != "enterprise.element" {
			continue
		}
		if s, ok := ReferenceValue(fv); ok && s == element {
			out = append(out, RefPath{Path: path, Title: f.Title, Stereotype: f.Stereotype})
		}
	}
	return out
}

func FieldValue(v reflect.Value, index []int) reflect.Value {
	for _, at := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || at >= v.NumField() {
			return reflect.Value{}
		}
		v = v.Field(at)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func ReferenceValue(v reflect.Value) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	if v.Kind() == reflect.String {
		return v.String(), true
	}
	if r, ok := v.Interface().(interface{ String() string }); ok {
		return r.String(), true
	}
	return "", false
}

func RecordLabel(info platform.EntityInfo, value any, id string) string {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return id
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return id
	}
	for _, f := range info.Fields {
		if !f.Search {
			continue
		}
		fv := FieldValue(v, f.Index)
		if fv.IsValid() && fv.Kind() == reflect.String && fv.String() != "" {
			return fv.String()
		}
	}
	return id
}

func KnowledgeBearing(info platform.EntityInfo) bool {
	return info.Type == platform.KnowledgeDocumentType || info.Type == platform.FileType || info.KnowledgeFiles ||
		slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Knowledge })
}

type RecordLookup interface{ HasRecord(typ, id string) bool }

func requiredEmpty(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return false
	default:
		return value.IsZero()
	}
}

func CheckFields(info platform.EntityInfo, entity any, v reflect.Value, replaying bool, lookup RecordLookup, resolve func(platform.AssetBinding) (platform.ValueType, bool), engines ...platform.RuleEngine) ([]platform.FieldIssue, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	stringOf := func(name string) string {
		i := slices.IndexFunc(info.Fields, func(x platform.FieldInfo) bool { return x.Name == name })
		if i < 0 {
			return ""
		}
		cv := v.FieldByIndex(info.Fields[i].Index)
		if cv.Kind() == reflect.Pointer {
			if cv.IsNil() {
				return ""
			}
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Bool {
			return strconv.FormatBool(cv.Bool())
		}
		return cv.String()
	}
	if !replaying && slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Constraints != nil || f.ValueType != nil }) {
		raw, marshalErr := json.Marshal(entity)
		if marshalErr != nil {
			return nil, invalid
		}
		var values map[string]any
		if json.Unmarshal(raw, &values) != nil {
			return nil, invalid
		}
		fields := []platform.Field{}
		for _, f := range info.Fields {
			if f.When != nil && !f.Active(stringOf, engines...) {
				continue
			}
			if _, present := values[f.Name]; !present {
				raw, err := json.Marshal(v.FieldByIndex(f.Index).Interface())
				if err != nil {
					return nil, invalid
				}
				var value any
				if json.Unmarshal(raw, &value) != nil {
					return nil, invalid
				}
				values[f.Name] = value
			}
			typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime", "attachment": "attachment"}[f.Type]
			if typ == "" {
				typ = "string"
			}
			fields = append(fields, platform.Field{Name: f.Name, Type: typ, Required: f.Required, Description: f.Title, Constraints: f.Constraints, Choices: f.Choices, ValueType: f.ValueType})
		}
		fields, resolved := platform.ResolveValueTypes(fields, resolve)
		if !resolved {
			return nil, invalid
		}
		if issues := platform.InputIssues(fields, values); len(issues) > 0 {
			return issues, nil
		}
	}
	for _, f := range info.Fields {
		fv := v.FieldByIndex(f.Index)
		if f.When != nil {
			on, _ := info.Field(f.When.Field)
			switch active := f.Active(stringOf, engines...); {
			case active && f.Required && f.Constraints == nil && requiredEmpty(fv):
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is required when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active && !fv.IsZero():
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} applies only when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active:
				continue
			}
		} else if f.Required && f.Constraints == nil && requiredEmpty(fv) {
			return nil, invalid
		}
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		switch f.Type {
		case "choice":
			if !fv.IsZero() && !slices.Contains(f.Choices, fv.String()) {
				return nil, invalid
			}
		}
		if l := info.Lifecycle; l != nil && f.Name == l.Field && !fv.IsZero() && !slices.ContainsFunc(l.States, func(st platform.State) bool { return st.Name == fv.String() }) {
			return nil, invalid
		}
		switch f.Type {
		case "":
		case "json":
			raw, err := json.Marshal(fv.Interface())
			if err != nil {
				return nil, invalid
			}
			if err := platform.ValidateRecordJSON(raw); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field}: {reason}", f.Name, err.Error())
			}
		case "date":
			if _, err := time.Parse(time.DateOnly, fv.String()); !fv.IsZero() && err != nil {
				return nil, invalid
			}
		case "text", "longtext":
			if f.Required && strings.TrimSpace(fv.String()) == "" {
				return nil, invalid
			}
		case "attachment":
			attachment, ok := fv.Interface().(platform.Attachment)
			if !ok {
				return nil, invalid
			}
			media, ok := lookup.(interface{ HasMediaItem(string, string) bool })
			if !ok || !media.HasMediaItem(attachment.MediaSet, attachment.Item) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE}
			}
		case "money":
			if m := fv.Interface().(platform.Money); m.Amount != 0 && len(m.Currency) != 3 {
				return nil, invalid
			}
		case "reference", "references":
			if f.Ref == "enterprise.element" {
				continue
			}
			ids := []string{fv.String()}
			if f.Type == "references" {
				ids = ids[:0]
				for i := range fv.Len() {
					ids = append(ids, fv.Index(i).String())
				}
			}
			for _, id := range ids {
				if id != "" && !lookup.HasRecord(f.Ref, id) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE}
				}
			}
		}
	}
	return nil, nil
}

func Copy(t reflect.Type, v any) reflect.Value {
	raw, _ := json.Marshal(v)
	out := reflect.New(t).Elem()
	json.Unmarshal(raw, out.Addr().Interface())
	return out
}

func EmptyLists(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		switch v.Interface().(type) {
		case time.Time, platform.ReleaseAsset:
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			x := v.Field(i)
			if x.Kind() == reflect.Slice && x.IsNil() && !strings.Contains(f.Tag.Get("json"), "omitempty") && !strings.Contains(f.Tag.Get("json"), "omitzero") && x.CanSet() {
				x.Set(reflect.MakeSlice(x.Type(), 0, 0))
				continue
			}
			EmptyLists(x)
		}
	case reflect.Slice:
		for i := range v.Len() {
			EmptyLists(v.Index(i))
		}
	}
}

func Mask(info platform.EntityInfo, v reflect.Value, hidden []platform.FieldInfo) any {
	c := Copy(info.Go, v.Interface())
	for _, f := range hidden {
		c.FieldByIndex(f.Index).SetZero()
	}
	return c.Interface()
}

func MaskHistory(h []RecordChange, hidden []platform.FieldInfo) []RecordChange {
	if len(hidden) == 0 {
		return h
	}
	out := make([]RecordChange, 0, len(h))
	for _, c := range h {
		c.Fields = slices.DeleteFunc(slices.Clone(c.Fields), func(f FieldChange) bool {
			return slices.ContainsFunc(hidden, func(x platform.FieldInfo) bool { return x.Name == f.Field })
		})
		out = append(out, c)
	}
	return out
}

type EnterpriseReference struct {
	Type       string `json:"type"`
	Field      string `json:"field"`
	FieldTitle string `json:"fieldTitle"`
	Stereotype string `json:"stereotype,omitempty"`
	ID         string `json:"id"`
	Name       string `json:"name"`
}

type EnterpriseReferenceGroup struct {
	Type       string                `json:"type"`
	Title      string                `json:"title"`
	Field      string                `json:"field"`
	FieldTitle string                `json:"fieldTitle"`
	Stereotype string                `json:"stereotype,omitempty"`
	Total      int                   `json:"total"`
	Records    []EnterpriseReference `json:"records"`
}

func EnterpriseReferences(s *Store, element string, offset, limit int, view func(*Type) (func(reflect.Value) bool, []platform.FieldInfo, bool), roles func(app string) []string) []EnterpriseReferenceGroup {
	s.Lock()
	types := make([]*Type, 0, s.TypeCount())
	for _, et := range s.Types() {
		types = append(types, et)
	}
	s.Unlock()
	slices.SortFunc(types, func(a, b *Type) int { return strings.Compare(a.Info().Type, b.Info().Type) })
	groups := []EnterpriseReferenceGroup{}
	for _, et := range types {
		if !DeclaresEnterpriseRef(et.Info().Fields) {
			continue
		}
		visible, hidden, ok := view(et)
		if !ok {
			continue
		}
		var rows []reflect.Value
		func() {
			s.Lock()
			defer s.Unlock()
			rows = make([]reflect.Value, 0, et.Rows().Len())
			for _, r := range et.Rows().All() {
				if visible == nil || visible(r.Value) {
					rows = append(rows, r.Value)
				}
			}

		}()
		slices.SortFunc(rows, func(a, b reflect.Value) int { return strings.Compare(Header(a).ID, Header(b).ID) })
		byPath := map[string]*EnterpriseReferenceGroup{}
		var order []string
		for _, r := range rows {
			id := Header(r).ID
			name := ""
			readable := Mask(et.Info(), r, hidden)
			seen := map[string]bool{}
			for _, f := range MatchingRefs(et.Info().Fields, "", reflect.ValueOf(readable), element, roles(et.Info().App)) {
				if seen[f.Path] {
					continue
				}
				seen[f.Path] = true
				g, ok := byPath[f.Path]
				if !ok {
					g = &EnterpriseReferenceGroup{Type: et.Info().Type, Title: et.Info().Plural, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, Records: []EnterpriseReference{}}
					byPath[f.Path] = g
					order = append(order, f.Path)
				}
				g.Total++
				if g.Total > offset && len(g.Records) < limit {
					if name == "" {
						name = RecordLabel(et.Info(), readable, id)
					}
					g.Records = append(g.Records, EnterpriseReference{Type: et.Info().Type, Field: f.Path, FieldTitle: f.Title, Stereotype: f.Stereotype, ID: id, Name: name})
				}
			}
		}
		for _, path := range order {
			groups = append(groups, *byPath[path])
		}
	}
	return groups
}

type RetainedBody struct {
	Type string
	Raw  json.RawMessage
}

type enterpriseDeclaration struct {
	Type   string                  `json:"type"`
	Name   string                  `json:"name"`
	Ref    string                  `json:"ref"`
	Fields []enterpriseDeclaration `json:"fields"`
	Entity *enterpriseDeclaration  `json:"entity"`
}

func EnterpriseHistoricalPaths(store *Store, objectType, objectPrefix string, typeOf func(string) string, retained []RetainedBody) map[string][][]string {
	paths := map[string][][]string{}
	seen := map[string]bool{}
	add := func(typ string, path []string) {
		key := typ + "/" + strings.Join(path, ".")
		if typ == "" || len(path) == 0 || seen[key] {
			return
		}
		seen[key] = true
		paths[typ] = append(paths[typ], slices.Clone(path))
	}
	var fields func(string, []platform.FieldInfo, []string)
	fields = func(typ string, items []platform.FieldInfo, prefix []string) {
		for _, field := range items {
			path := append(slices.Clone(prefix), field.Name)
			if field.Ref == "enterprise.element" {
				add(typ, path)
			}
			if len(field.Fields) > 0 {
				fields(typ, field.Fields, path)
			}
		}
	}
	bodies := slices.Clone(retained)
	func() {
		store.Lock()
		defer store.Unlock()
		for _, et := range store.Types() {
			fields(et.Info().Type, et.Info().Fields, nil)
		}
		if objects := store.Type(objectType); objects != nil {
			for _, row := range objects.Rows().All() {
				typ := typeOf(BusinessField(objects.Info(), row.Value, "name"))
				if published := BusinessField(objects.Info(), row.Value, "published"); published != "" {
					bodies = append(bodies, RetainedBody{typ, json.RawMessage(published)})
				}
				for _, change := range row.RecordHistory() {
					for _, field := range change.Fields {
						if field.Field != "published" {
							continue
						}
						for _, raw := range []json.RawMessage{field.Before, field.After} {
							var published string
							if json.Unmarshal(raw, &published) == nil && published != "" {
								bodies = append(bodies, RetainedBody{typ, json.RawMessage(published)})
							}
						}
					}
				}
			}
		}
	}()
	for _, body := range bodies {
		var declaration enterpriseDeclaration
		if json.Unmarshal(body.Raw, &declaration) != nil {
			continue
		}
		typ := body.Type
		if declaration.Type != "" {
			typ = declaration.Type
		} else if declaration.Name != "" && strings.HasPrefix(typ, objectPrefix) {
			typ = typeOf(declaration.Name)
		}
		if declaration.Entity != nil {
			declaration = *declaration.Entity
		}
		var walk func([]enterpriseDeclaration, []string)
		walk = func(items []enterpriseDeclaration, prefix []string) {
			for _, field := range items {
				path := append(slices.Clone(prefix), field.Name)
				if field.Ref == "enterprise.element" {
					add(typ, path)
				}
				walk(field.Fields, path)
			}
		}
		walk(declaration.Fields, nil)
	}
	return paths
}

func HistoryNamesEnterprise(paths [][]string, changes []RecordChange, id string) bool {
	for _, change := range changes {
		for _, field := range change.Fields {
			for _, path := range paths {
				if len(path) == 0 || path[0] != field.Field {
					continue
				}
				for _, raw := range []json.RawMessage{field.Before, field.After} {
					if enterpriseHistoryJSONNames(raw, path[1:], id) {
						return true
					}
				}
			}
		}
	}
	return false
}

func enterpriseHistoryJSONNames(raw json.RawMessage, path []string, id string) bool {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return false
	}
	var names func(any, []string) bool
	names = func(value any, path []string) bool {
		if rows, ok := value.([]any); ok {
			return slices.ContainsFunc(rows, func(row any) bool { return names(row, path) })
		}
		if len(path) == 0 {
			reference, ok := value.(string)
			return ok && reference == id
		}
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		return names(object[path[0]], path[1:])
	}
	return names(value, path)
}

func EnterpriseCurrent(info platform.EntityInfo, value reflect.Value, now time.Time, engines ...platform.RuleEngine) bool {
	if Header(value).Archived {
		return false
	}
	if info.EnterpriseUse == nil {
		return true
	}
	program, err := info.CompileEnterpriseUse(engines...)
	if err != nil {
		return true
	}
	context := platform.ExprContext{Refs: map[string]platform.ExprValue{}, Now: now, Today: now.UTC().Format(time.DateOnly)}
	context.Refs["archived"] = platform.ExprPresent("boolean", Header(value).Archived)
	for _, field := range info.Fields {
		context.Refs[field.Name] = platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "value", Data: rowJSON(value.FieldByIndex(field.Index).Interface())}
	}
	result, err := program.Eval(context)
	return err != nil || string(result.Value.Data) != "false"
}
