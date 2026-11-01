package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func Describe(app string, e Entity, typeOf func(reflect.Type) string) (EntityInfo, error) {
	t := reflect.TypeOf(e.Model)
	if t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 || t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous {
		return EntityInfo{}, fmt.Errorf("entity %s: the model must be a struct embedding platform.Record first", e.Type)
	}
	info := EntityInfo{DataSources: slices.Clone(e.DataSources), LocalRules: e.LocalRules, EnterpriseUse: e.EnterpriseUse, Type: e.Type, Title: e.Title, Description: e.Description, Synonyms: e.Synonyms, KnowledgeFiles: e.KnowledgeFiles, App: app, Display: e.Display, ObjectView: e.ObjectView, Go: t, Scope: e.Scope, Fields: []FieldInfo{}, Standard: []string{}, Implements: e.Implements, Extends: e.Extends, ExtensionSource: e.ExtensionSource, Constraints: append(slices.Clone(e.Constraints), VersioningConstraints(e.Versioning)...), Sets: e.Sets, Versioning: e.Versioning, VersionPins: slices.Clone(e.VersionPins)}
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
		if i < 0 || binding.Ref.Kind != AssetPropertyType || binding.Ref.Check() != nil || binding.SourceVersion == "" {
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
		if x[1] != "" && !(x[0] == "owner" && x[1] == OwnerCreated) {
			f, ok := info.Field(x[1])
			unitReference := x[0] == "unit" && f.Type == "reference" && f.Ref == "enterprise.element"
			if !ok || f.Type != "text" && !unitReference {
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
		if l != "" && l != ScopeTenant && l != ScopeBelow && l != ScopeUnit && l != ScopeOwn && l != ScopeNone ||
			(l == ScopeOwn && e.Scope.Owner == "") || ((l == ScopeUnit || l == ScopeBelow) && (e.Scope.Unit == "" || e.Scope.Structure == "")) {
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
		for i, t := range l.Transitions {
			if to, input := transactionDestination(e.Type, l.Field, t.Txn, t.Payload); len(to) > 0 {
				t.To, t.ToInput = to, input
			}
			ends := append(slices.Clone(t.From), t.To...)
			if t.Name == "" || len(t.From) == 0 || len(t.To) == 0 || slices.ContainsFunc(ends, func(s string) bool { return !states[s] }) ||
				slices.ContainsFunc(l.Transitions[:i], func(x Transition) bool { return x.Name == t.Name }) || t.Name == "create" || t.Name == "edit" || t.Name == "archive" {
				return EntityInfo{}, fmt.Errorf("entity %s: transition %q needs a unique name and states of the lifecycle", e.Type, t.Name)
			}
			if t.ToInput != "" {
				index := slices.IndexFunc(t.Payload, func(f Field) bool { return f.Name == t.ToInput })
				var input Field
				if index >= 0 {
					input = t.Payload[index]
				}
				if index < 0 || input.Type != "string" || !input.Required || input.Ref != "" || len(input.Choices) != len(t.To) || slices.ContainsFunc(t.To, func(state string) bool { return !slices.Contains(input.Choices, state) }) || t.Do == nil && (t.Txn == nil || t.ApplyTxn == nil) {
					return EntityInfo{}, fmt.Errorf("entity %s: transition %s needs its required destination choice and original action", e.Type, t.Name)
				}
			}
			title := t.Title
			if title == "" {
				title = strings.ToUpper(t.Name[:1]) + t.Name[1:]
			}
			info.Lifecycle.Transitions = append(info.Lifecycle.Transitions, TransitionInfo{Name: t.Name, Schema: e.Type + "." + t.Name, Title: title, From: t.From, To: t.To, ToInput: t.ToInput})
		}
	}
	for _, x := range [][2]any{{e.Standard.Create, ".create"}, {e.Standard.Edit, ".edit"}, {e.Standard.Archive, ".archive"}} {
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

func (info EntityInfo) CompileEnterpriseUse(engines ...RuleEngine) (ExprProgram, error) {
	if info.EnterpriseUse == nil {
		return nil, nil
	}
	if info.EnterpriseUse.Kind != "boolean" {
		return nil, fmt.Errorf("current enterprise use must return boolean")
	}
	refs := map[string]ExprType{"archived": {Kind: "boolean"}}
	for _, field := range info.Fields {
		refs[field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
	}
	return ruleEngineOf(engines).CompileExpr(*info.EnterpriseUse, ExprSchema{Refs: refs})
}

func ApplyFieldAliases(info EntityInfo, value reflect.Value) {
	for _, field := range info.Fields {
		if field.AliasOf != "" {
			target, _ := info.Field(field.AliasOf)
			value.FieldByIndex(field.Index).Set(value.FieldByIndex(target.Index))
		}
	}
}

func (info EntityInfo) HasReadSources() bool {
	return len(info.Derived) > 0 || slices.ContainsFunc(info.Fields, func(field FieldInfo) bool { return field.AliasOf != "" })
}

func payloadColumns(columns []FieldInfo) []Field {
	if len(columns) == 0 {
		return nil
	}
	out := make([]Field, 0, len(columns))
	for _, column := range columns {
		if column.ReadOnly {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "json": "json", "lines": "lines"}[column.Type]
		if typ == "" {
			typ = "string"
		}
		field := Field{Name: column.Name, Type: typ, Required: column.Required && column.When == nil, Description: column.Title,
			Choices: column.Choices, Constraints: column.Constraints, Range: column.Range, Fields: payloadColumns(column.Fields)}
		if column.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = column.Ref, column.Stereotype, slices.Clone(column.Stereotypes)
		}
		out = append(out, field)
	}
	return out
}

func EntityActions(e Entity) []Action {
	info, err := Describe("", e, func(reflect.Type) string { return "?" })
	if err != nil {
		panic(err)
	}
	var fields, editable []Field
	for _, f := range info.Fields {
		if f.ReadOnly || f.Aside {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "json": "json", "lines": "lines"}[f.Type]
		if typ == "" {
			typ = "string"
		}
		description := f.Title
		if f.Help != "" {
			description += ": " + f.Help
		}
		if len(f.Choices) > 0 {
			description += " (" + strings.Join(f.Choices, ", ") + ")"
		}
		if f.Example != "" {
			description += ", e.g. " + f.Example
		}
		field := Field{Name: f.Name, Type: typ, Required: f.Required && f.When == nil, Description: description, Choices: f.Choices, Constraints: f.Constraints, ValueType: f.ValueType, Range: f.Range, Group: f.Group,
			Fields: payloadColumns(f.Fields)}
		if f.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = f.Ref, f.Stereotype, slices.Clone(f.Stereotypes)
		}
		fields = append(fields, field)
		field.Required = false
		editable = append(editable, field)
	}
	var out []Action
	capability := e.Standard.Capability
	if capability == "" {
		capability = e.Type
	}
	if e.Standard.Create {
		if e.CreateTxn != nil {
			for _, step := range e.CreateTxn.Steps {
				if step.Kind == "update" && step.Target.Type == e.Type && step.Target.ID == "$subject" && step.When == nil {
					for _, assignment := range step.Changes {
						for i := range fields {
							if fields[i].Name == assignment.Field {
								fields[i].Required = false
							}
						}
					}
				}
			}
		}
		out = append(out, Action{Schema: e.Type + ".create", Target: e.Type, New: true, Capability: capability, Title: "Create " + strings.ToLower(info.Title),
			Description: "Create " + article(info.Title) + ".", Payload: fields, Roles: e.Standard.roles("create")})
	}
	if e.Standard.Edit {
		out = append(out, Action{recordEdit: true, Schema: e.Type + ".edit", Target: e.Type, Capability: capability, Title: "Edit " + strings.ToLower(info.Title),
			Description: "Change fields of " + article(info.Title) + "; fields left out keep their value.", Payload: editable, Roles: e.Standard.roles("edit")})
	}
	if e.Standard.Archive {
		out = append(out, Action{Schema: e.Type + ".archive", Target: e.Type, Capability: capability, Title: "Archive " + strings.ToLower(info.Title),
			Description: "Archive " + article(info.Title) + ": it leaves lists but stays referenced and in history.", Payload: []Field{}, Roles: e.Standard.roles("archive")})
	}
	if e.Lifecycle != nil {
		for i, t := range e.Lifecycle.Transitions {
			c := t.Capability
			if c == "" {
				c = e.Type
			}
			payload := t.Payload
			if payload == nil {
				payload = []Field{}
			}
			description := t.Description
			if description == "" {
				description = fmt.Sprintf("Move %s from %s to %s.", article(info.Title), strings.Join(t.From, " or "), strings.Join(t.To, " or "))
			}
			out = append(out, Action{Schema: e.Type + "." + t.Name, Target: e.Type, Capability: c, Title: info.Lifecycle.Transitions[i].Title,
				Description: description, Payload: payload, Roles: t.Roles, Adoption: t.Adoption, Approval: t.Approval, DeferRequired: t.DeferRequired, ConfirmationOnly: t.ConfirmationOnly})
			if t.Approval != nil && t.Approval.Pending != "" {
				for _, m := range [][2]string{{ApprovalHeld, "Hold for approval"}, {ApprovalRejected, "Mark rejected"}, {ApprovalReturned, "Return from approval"}} {
					suffix, title := m[0], m[1]
					out = append(out, Action{Schema: e.Type + "." + t.Name + suffix, Target: e.Type, Capability: c, Title: title,
						Description: "Moves the record while its approval runs; only the approval takes it.", Payload: []Field{}, Automation: true})
				}
			}
		}
	}
	return out
}

func article(title string) string {
	t := strings.ToLower(title)
	if t != "" && strings.ContainsRune("aeiou", rune(t[0])) {
		return "an " + t
	}
	return "a " + t
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
			case "":
			default:
				return nil, fmt.Errorf("entity %s: field %s has an unknown flag %q", e.Type, name, flag)
			}
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
		case ft == moneyType:
			f.Type = "money"
		case ft.Kind() == reflect.String && sf.Tag.Get("ref") != "":
			f.Type, f.Ref, f.Stereotype = "reference", sf.Tag.Get("ref"), sf.Tag.Get("stereo")
			if values := sf.Tag.Get("stereos"); values != "" {
				f.Stereotypes = strings.Split(values, ",")
			}
		case ft.Kind() == reflect.String && refined == "date", ft.Kind() == reflect.String && refined == "longtext":
			f.Type = refined
		case ft.Kind() == reflect.String && sf.Tag.Get("choices") != "":
			f.Type, f.Choices = "choice", strings.Split(sf.Tag.Get("choices"), ",")
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
		when, err := ParseWhen(sf.Tag.Get("when"))
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

func (f FieldInfo) Reads(role string) bool { return f.ReadsAny([]string{role}) }

func (f FieldInfo) Writes(role string) bool { return f.WritesAny([]string{role}) }

func (f FieldInfo) ReadsAny(roles []string, engines ...RuleEngine) bool {
	return propertyPolicyOf("read", f, engines...).allows(roles)
}

func (f FieldInfo) WritesAny(roles []string, engines ...RuleEngine) bool {
	if len(roles) == 0 {
		roles = []string{""}
	}
	return propertyPolicyOf("write", f, engines...).allows(roles)
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
