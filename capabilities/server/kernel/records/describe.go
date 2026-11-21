package records

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

type (
	ActionType        = platform.ActionType
	Attachment        = platform.Attachment
	DerivationInfo    = platform.DerivationInfo
	Entity            = platform.RecordType
	EntityInfo        = platform.RecordTypeInfo
	Field             = platform.Field
	FieldInfo         = platform.FieldInfo
	LifecycleInfo     = platform.LifecycleInfo
	Money             = platform.Money
	Record            = platform.Record
	RecordImageFormat = platform.RecordImageFormat
	TransitionInfo    = platform.TransitionInfo
)

type reference interface{ refType() reflect.Type }

var (
	timeType  = reflect.TypeFor[time.Time]()
	moneyType = reflect.TypeFor[Money]()
	refIface  = reflect.TypeFor[reference]()
)

func Describe(app string, e Entity, typeOf func(reflect.Type) string) (EntityInfo, error) {
	t := reflect.TypeOf(e.Model)
	if t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 || t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous {
		return EntityInfo{}, fmt.Errorf("entity %s: the model must be a struct embedding platform.Record first", e.Type)
	}
	info := EntityInfo{DataSources: slices.Clone(e.DataSources), LocalRules: e.LocalRules, EnterpriseUse: e.EnterpriseUse, Type: e.Type, Title: e.Title, Description: e.Description, Synonyms: e.Synonyms, KnowledgeFiles: e.KnowledgeFiles, App: app, Display: e.Display, ObjectView: e.ObjectView, View: slices.Clone(e.View), Go: t, Scope: e.Scope, Fields: []FieldInfo{}, Standard: []string{}, Implements: e.Implements, Extends: e.Extends, ExtensionSource: e.ExtensionSource, Constraints: append(slices.Clone(e.Constraints), protocol.VersioningConstraints(e.Versioning)...), Sets: e.Sets, Versioning: e.Versioning, VersionPins: slices.Clone(e.VersionPins)}
	info.RecordFormat, info.RecordImages = e.RecordFormat, slices.Clone(e.RecordImages)
	if err := checkRecordImageFormats(info); err != nil {
		return EntityInfo{}, err
	}
	if info.Title == "" {
		info.Title = e.Type
	}
	info.Plural = e.Plural
	switch t := info.Title; {
	case info.Plural != "":
	case strings.HasSuffix(t, "y") && !strings.HasSuffix(t, "ay") && !strings.HasSuffix(t, "ey") && !strings.HasSuffix(t, "oy"):
		info.Plural = t[:len(t)-1] + "ies"
	case strings.HasSuffix(t, "s") || strings.HasSuffix(t, "x") || strings.HasSuffix(t, "ch") || strings.HasSuffix(t, "sh"):
		info.Plural = t + "es"
	default:
		info.Plural = t + "s"
	}
	fields, err := describeFields(e, t, 1, typeOf)
	if err != nil {
		return EntityInfo{}, err
	}
	info.Fields = fields
	for name, binding := range e.PropertyBindings {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 || binding.Ref.Kind != platform.AssetPropertyType || binding.Ref.Check() != nil || binding.SourceVersion == "" {
			return EntityInfo{}, fmt.Errorf("invalid property field binding")
		}
		selected := binding
		info.Fields[i].Property = &selected
	}
	if err := describePresentation(&info, e); err != nil {
		return EntityInfo{}, err
	}
	if info.Display == "" {
		info.Display = "id"
		if i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Search }); i >= 0 {
			info.Display = info.Fields[i].Name
		}
	}
	for _, x := range [][2]string{{"owner", e.Scope.Owner}, {"unit", e.Scope.Unit}} {
		if x[1] != "" && !(x[0] == "owner" && x[1] == platform.OwnerCreated) {
			f, ok := info.Field(x[1])
			unitReference := x[0] == "unit" && f.Type == "reference" && f.Ref == "enterprise.element"
			if !ok || f.Type != "text" && !unitReference && !(x[0] == "owner" && f.Type == "member") {
				return EntityInfo{}, fmt.Errorf("entity %s: the scope's %s field %q needs text or the unit's enterprise reference", e.Type, x[0], x[1])
			}
		}
	}
	for _, l := range append(func() []string {
		var out []string
		for _, l := range e.Scope.Levels {
			out = append(out, l)
		}
		return out
	}(), e.Scope.Default) {
		if l != "" && l != platform.ScopeTenant && l != platform.ScopeBelow && l != platform.ScopeUnit && l != platform.ScopeOwn && l != platform.ScopeNone ||
			(l == platform.ScopeOwn && e.Scope.Owner == "") || ((l == platform.ScopeUnit || l == platform.ScopeBelow) && (e.Scope.Unit == "" || e.Scope.Structure == "")) {
			return EntityInfo{}, fmt.Errorf("entity %s: scope level %q without the field it needs", e.Type, l)
		}
	}
	if l := e.Lifecycle; l != nil {
		f, ok := info.Field(l.Field)
		states := map[string]bool{}
		for _, s := range l.States {
			states[s.Name] = true
		}
		if !ok || !f.ReadOnly || (f.Type != "text" && f.Type != "choice") || !states[l.Initial] || len(states) != len(l.States) {
			return EntityInfo{}, fmt.Errorf("entity %s: the lifecycle needs a read-only text or choice status field, unique states and an initial state among them", e.Type)
		}
		info.Lifecycle = &LifecycleInfo{Field: l.Field, Initial: l.Initial, States: l.States, Transitions: []TransitionInfo{}}
		for i, t := range l.Actions {
			if t.Criteria.State == nil || t.Criteria.State.Field != l.Field {
				return EntityInfo{}, fmt.Errorf("entity %s: transition %q needs a unique name and states of the lifecycle", e.Type, t.Name)
			}
			from := t.Criteria.State.In
			var to []string
			var inputName string
			hasFunction, hasTransaction := false, false
			for _, rule := range t.Rules {
				if rule.Function != nil {
					hasFunction = true
				}
				if rule.Edit != nil && rule.Edit.Field == l.Field {
					to, inputName = rule.Edit.Allowed, rule.Edit.Parameter
				}
				if rule.Transaction != nil {
					hasTransaction = rule.Transaction.Spec != nil && rule.Transaction.Run != nil
					to = slices.Clone(from)
					if e.TransactionDestination != nil {
						if dest, input := e.TransactionDestination(e.Type, l.Field, rule.Transaction.Spec, t.Parameters); len(dest) > 0 {
							to, inputName = dest, input
						}
					}
				}
			}
			ends := append(slices.Clone(from), to...)
			if t.Name == "" || len(from) == 0 || len(to) == 0 || slices.ContainsFunc(ends, func(s string) bool { return !states[s] }) ||
				slices.ContainsFunc(l.Actions[:i], func(x ActionType) bool { return x.Name == t.Name }) || t.Name == "create" || t.Name == "edit" || t.Name == "archive" {
				return EntityInfo{}, fmt.Errorf("entity %s: transition %q needs a unique name and states of the lifecycle", e.Type, t.Name)
			}
			if inputName != "" {
				index := slices.IndexFunc(t.Parameters, func(f Field) bool { return f.Name == inputName })
				var input Field
				if index >= 0 {
					input = t.Parameters[index]
				}
				if index < 0 || input.Type != "string" || !input.Required || input.Ref != "" || len(input.Choices) != len(to) || slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(input.Choices, state) }) || !hasFunction && !hasTransaction {
					return EntityInfo{}, fmt.Errorf("entity %s: transition %s needs its required destination choice and original action", e.Type, t.Name)
				}
			}
			title := t.Title
			if title == "" {
				title = strings.ToUpper(t.Name[:1]) + t.Name[1:]
			}
			info.Lifecycle.Transitions = append(info.Lifecycle.Transitions, TransitionInfo{Name: t.Name, Schema: e.Type + "." + t.Name, Title: title, From: from, To: to, ToInput: inputName})
		}
	}
	for _, x := range [][2]any{{e.Standard.Create, ".create"}, {e.Standard.Edit, ".edit"}, {e.Standard.Archive, ".archive"}, {e.Standard.Restore, ".restore"}} {
		if x[0].(bool) {
			info.Standard = append(info.Standard, e.Type+x[1].(string))
		}
	}
	if err := describeDerived(&info, e); err != nil {
		return EntityInfo{}, err
	}
	for _, alias := range info.Fields {
		if alias.AliasOf == "" {
			continue
		}
		target, found := info.Field(alias.AliasOf)
		if !found || target.Name == alias.Name || target.AliasOf != "" || !alias.ReadOnly || alias.Type != target.Type || alias.Ref != target.Ref || alias.Required != target.Required || !slices.Equal(alias.Choices, target.Choices) || info.Go.FieldByIndex(alias.Index).Type != info.Go.FieldByIndex(target.Index).Type {
			return EntityInfo{}, fmt.Errorf("field %s needs one compatible direct read alias target", alias.Name)
		}
	}
	if _, err := info.CompileEnterpriseUse(); err != nil {
		return EntityInfo{}, fmt.Errorf("entity %s: %w", e.Type, err)
	}
	return info, nil
}

func describeFields(e Entity, t reflect.Type, from int, typeOf func(reflect.Type) string) ([]FieldInfo, error) {
	out := []FieldInfo{}
	for i := from; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if !sf.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("entity %s: field %s needs a json name", e.Type, sf.Name)
		}
		f := FieldInfo{Name: name, Title: sf.Tag.Get("title"), Index: sf.Index, Knowledge: sf.Tag.Get("knowledge") == "true",
			Help: sf.Tag.Get("help"), Synonyms: sf.Tag.Get("synonyms"), Example: sf.Tag.Get("example"), Personal: sf.Tag.Get("personal")}
		f.SourceType = sf.Tag.Get("source")
		if policy := sf.Tag.Get("ai"); policy != "" {
			allowed := policy == "allow"
			f.AIInput = &allowed
		}
		f.AliasOf = sf.Tag.Get("alias")
		if f.SourceType != "" && !slices.Contains([]string{"record", "user-input", "external-input", "generated"}, f.SourceType) {
			return nil, fmt.Errorf("field %s has an unknown source type", name)
		}
		if raw := sf.Tag.Get("constraints"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &f.Constraints); err != nil {
				return nil, fmt.Errorf("%s.%s constraints: %w", e.Type, name, err)
			}
		}
		if raw := sf.Tag.Get("range"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &f.Range); err != nil {
				return nil, fmt.Errorf("%s.%s range: %w", e.Type, name, err)
			}
		}
		f.Group = sf.Tag.Get("group")
		f.Aside = sf.Tag.Get("rail") == "true"
		f.OneOf = sf.Tag.Get("oneof")
		if roles := sf.Tag.Get("read"); roles != "" {
			f.Read = strings.Split(roles, ",")
		}
		if roles := sf.Tag.Get("write"); roles != "" {
			f.Write = strings.Split(roles, ",")
		}
		if f.Title == "" {
			f.Title = strings.ToUpper(name[:1]) + name[1:]
		}
		for _, flag := range strings.Split(sf.Tag.Get("field"), ",") {
			switch flag {
			case "required":
				f.Required = true
			case "search":
				f.Search = true
			case "readonly":
				f.ReadOnly = true
			case "aside":
				f.Aside = true
				f.Supplemental = true
			case "internal":
				f.Internal = true
			case "":
			default:
				return nil, fmt.Errorf("entity %s: field %s has an unknown flag %q", e.Type, name, flag)
			}
		}
		f.Provenance = sf.Tag.Get("provenance")
		if f.Provenance == "" && f.ReadOnly && f.AliasOf == "" {
			f.Provenance = "action"
		}
		refined := sf.Tag.Get("type")
		ft := sf.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case refined == "json" && (ft.Kind() == reflect.Map || ft.Kind() == reflect.Struct || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array):
			f.Type = "json"
		case ft.Implements(refIface):
			f.Type = "reference"
			f.Ref = typeOf(reflect.Zero(ft).Interface().(reference).refType())
		case ft.Kind() == reflect.Slice && ft.Elem().Implements(refIface):
			f.Type = "references"
			f.Ref = typeOf(reflect.Zero(ft.Elem()).Interface().(reference).refType())
		case ft == timeType:
			f.Type = "datetime"
		case ft == reflect.TypeFor[Attachment]():
			f.Type = "attachment"
		case ft == moneyType:
			f.Type = "money"
		case ft.Kind() == reflect.String && sf.Tag.Get("ref") != "":
			f.Type, f.Ref, f.Stereotype = "reference", sf.Tag.Get("ref"), sf.Tag.Get("stereo")
			if values := sf.Tag.Get("stereos"); values != "" {
				f.Stereotypes = strings.Split(values, ",")
			}
		case ft.Kind() == reflect.String && slices.Contains([]string{"date", "longtext", "member"}, refined):
			f.Type = refined
		case ft.Kind() == reflect.String && sf.Tag.Get("choices") != "":
			f.Type, f.Choices = "choice", strings.Split(sf.Tag.Get("choices"), ",")
			if titles := sf.Tag.Get("choiceTitles"); titles != "" {
				f.ChoiceTitles = strings.Split(titles, ",")
			}
		case ft.Kind() == reflect.String:
			f.Type = "text"
		case ft.Kind() == reflect.Bool:
			f.Type = "boolean"
		case ft.Kind() >= reflect.Int && ft.Kind() <= reflect.Uint64:
			f.Type = "integer"
		case ft.Kind() == reflect.Float64:
			f.Type = "decimal"
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.String:
			f.Type = "tags"
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct && from > 0:
			f.Type = "lines"
			if columns, err := describeFields(e, ft.Elem(), 0, typeOf); err == nil {
				f.Fields = columns
			}
		default:
			return nil, fmt.Errorf("entity %s: field %s has a type the kit does not know (%s)", e.Type, name, ft)
		}
		if (f.Type == "reference" || f.Type == "references") && f.Ref == "" {
			return nil, fmt.Errorf("entity %s: field %s refers to a struct that is not an entity type of the app", e.Type, name)
		}
		if f.Inverse = sf.Tag.Get("inverse"); f.Inverse != "" && f.Type != "reference" {
			return nil, fmt.Errorf("entity %s: field %s names an inverse but is not a reference", e.Type, name)
		}
		when, err := platform.ParseWhen(sf.Tag.Get("when"))
		if err != nil {
			return nil, fmt.Errorf("entity %s: field %s: %w", e.Type, name, err)
		}
		f.When = when
		out = append(out, f)
	}
	if err := checkConditions(e.Type, out); err != nil {
		return nil, err
	}
	return out, nil
}

func describeDerived(info *EntityInfo, e Entity) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("entity %s: derived content: "+format, append([]any{e.Type}, args...)...)
	}
	if e.Withheld != "" {
		f, ok := info.Field(e.Withheld)
		if !ok || f.Type != "boolean" {
			return fail("the withheld field %q is not a boolean field", e.Withheld)
		}
		info.Withheld = f.Index
	}
	if len(e.Derived) == 0 {
		return nil
	}
	if e.Withheld == "" {
		return fail("a type that derives content names a withheld field, so a reader is told")
	}
	for _, d := range e.Derived {
		out := DerivationInfo{Element: d.Element, Names: append([]string{d.From}, d.Fields...)}
		if d.From == "" {
			return fail("a derivation names no source field")
		}
		holder, at := info.Go, ""
		if d.From == "*" {
			out.All = true
		} else {
			list, last, entered := strings.Cut(d.From, ".")
			if entered {
				f, ok := info.Field(list)
				jsonList := false
				if ok && f.Type == "json" {
					fieldType := info.Go.FieldByIndex(f.Index).Type
					jsonList = fieldType.Kind() == reflect.Slice && elementOf(info.Go, f.Index).Kind() == reflect.Struct
				}
				if !ok || f.Type != "lines" && !jsonList {
					return fail("%q enters %q, which is not a list of records the type holds", d.From, list)
				}
				out.List, holder, at = f.Index, elementOf(info.Go, f.Index), list+"."
			} else {
				last = list
			}
			for _, name := range strings.Split(last, "/") {
				path, err := pathTo(holder, name)
				if err != nil {
					return fail("%q: %v", at+name, err)
				}
				out.Refs = append(out.Refs, path)
			}
			if len(out.Refs) > 2 {
				return fail("%q joins more than a type and an id", d.From)
			}
		}
		if out.Element && len(out.List) == 0 {
			return fail("%q drops an element, but names no list", d.From)
		}
		if !out.Element && len(d.Fields) == 0 {
			return fail("%q empties no field and drops no element", d.From)
		}
		for _, name := range d.Fields {
			path, err := pathTo(holder, name)
			if err != nil {
				return fail("%q: %v", at+name, err)
			}
			out.Fields = append(out.Fields, path)
		}
		info.Derived = append(info.Derived, out)
	}
	return nil
}

func elementOf(t reflect.Type, index []int) reflect.Type {
	f := t.FieldByIndex(index).Type
	for f.Kind() == reflect.Slice || f.Kind() == reflect.Pointer {
		f = f.Elem()
	}
	return f
}

func pathTo(t reflect.Type, name string) ([]int, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s holds no fields", t)
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if json, _, _ := strings.Cut(f.Tag.Get("json"), ","); json == name && f.IsExported() {
			return []int{i}, nil
		}
	}
	return nil, fmt.Errorf("%s has no field %q", t, name)
}

func describePresentation(info *EntityInfo, e Entity) error {
	info.Icon, info.Color = e.Icon, e.Color
	for name, rules := range e.Format {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 {
			return fmt.Errorf("entity %s: format rules name an unknown field %s", e.Type, name)
		}
		info.Fields[i].Format = slices.Clone(rules)
	}
	for name, binding := range e.ValueTypeBindings {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 || binding.Ref.Kind != platform.AssetValueType || binding.Ref.Check() != nil || binding.SourceVersion == "" || info.Fields[i].Property != nil || info.Fields[i].Constraints != nil {
			return fmt.Errorf("entity %s: invalid value type field binding", e.Type)
		}
		selected := binding
		info.Fields[i].ValueType = &selected
	}
	return platform.CheckPresentation(*info)
}

func checkRecordImageFormats(info EntityInfo) error {
	seen := map[string]bool{info.RecordFormat: true}
	for _, image := range info.RecordImages {
		t := reflect.TypeOf(image.Model)
		if seen[image.Format] || t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 ||
			t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous || image.Convert == nil {
			return fmt.Errorf("entity %s: invalid or duplicate historical record format %q", info.Type, image.Format)
		}
		seen[image.Format] = true
	}
	return nil
}

func checkConditions(entity string, fields []FieldInfo) error {
	for _, f := range fields {
		if f.When == nil {
			continue
		}
		i := slices.IndexFunc(fields, func(x FieldInfo) bool { return x.Name == f.When.Field })
		if i < 0 || i == slices.IndexFunc(fields, func(x FieldInfo) bool { return x.Name == f.Name }) {
			return fmt.Errorf("entity %s: field %s depends on %s, which is not another field", entity, f.Name, f.When.Field)
		}
		on := fields[i]
		if on.When != nil {
			return fmt.Errorf("entity %s: field %s depends on %s, which is conditional itself", entity, f.Name, on.Name)
		}
		allowed := on.Choices
		if on.Type == "reference" {
			continue
		}
		if on.Type == "boolean" {
			allowed = []string{"true", "false"}
		} else if on.Type != "choice" {
			return fmt.Errorf("entity %s: field %s depends on %s, which is not a choice, a boolean or a reference", entity, f.Name, on.Name)
		}
		for _, v := range f.When.In {
			if !slices.Contains(allowed, v) {
				return fmt.Errorf("entity %s: field %s depends on %s being %q, which it never is", entity, f.Name, on.Name, v)
			}
		}
	}
	return nil
}
