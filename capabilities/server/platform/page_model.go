package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func (f FieldRef) Segments() []string {
	if len(f.Path) > 0 {
		return f.Path
	}
	return []string{f.Name}
}

func (f FieldRef) Key() string {
	return strings.Join(f.Segments(), ".")
}

func ModelObject(typ string) AssetRef {
	app, _, _ := strings.Cut(typ, ".")
	return AssetRef{App: app, Kind: AssetObject, Name: typ}
}

func (p *Page) NormalizeModelBindings(lookup func(string) (EntityInfo, bool)) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	safe = safe.ResolveBindings(lookup)
	safe.retainedVersion = ""
	*p = safe
	return nil
}

func (p Page) CheckModelBindings(lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool), engines ...RuleEngine) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	for _, binding := range safe.DataBindings {
		if err := safe.CheckDataBinding(binding, lookup, criteria, engines...); err != nil {
			return err
		}
	}
	return nil
}

func CheckRuleExpression(expr Expr, reads RuleReadSpec, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
	schema, err := RuleSchema(expr, reads, lookup, engines...)
	if err != nil {
		return err
	}
	_, err = ruleEngineOf(engines).CompileExpr(expr, schema)
	return err
}

func PageRuleInputFits(variable, kind string) bool {
	return variable == kind || variable == "string" && (kind == "text" || kind == "choice" || kind == "date" || kind == "datetime") || variable == "number" && (kind == "number" || kind == "integer")
}

func (p Page) ModelDependencies() []AssetRef {
	refs := []AssetRef{}
	safe, err := p.Safety()
	if err != nil {
		return refs
	}
	for _, binding := range safe.DataBindings {
		for _, model := range binding.ModelBindings() {
			if safe.WindowVariableInterface(binding.Ports["collectionVariable"]) == "" {
				ref := ModelObject(model.Type)
				if binding.Object.Name == model.Type {
					ref = binding.Object
				} else if safe.Object.Name == model.Type {
					ref = safe.Object
				}
				refs = append(refs, ref)
			}
			if model.Relation != nil {
				refs = append(refs, ModelObject(model.Relation.ParentType))
			}
			sets := []*ModelRecordSet{model.Set}
			if model.Options != nil {
				sets = append(sets, model.Options.Set)
				if model.Options.Rule != nil {
					refs = append(refs, model.Options.Rule.Action)
				}
			}
			for _, set := range sets {
				if set == nil {
					continue
				}
				if set.Query != nil {
					refs = append(refs, set.Query.Ref)
				}
				if set.Name != "" {
					refs = append(refs, safe.RecordResourceObject(set.RecordVariable))
				}
			}
		}
	}
	return refs
}

func (b PageWidgetSpec) IsZero() bool { return b.Format == "" && len(b.Body) == 0 }

func (b PageDataBinding) PrimaryModel() *ModelBinding {
	if b.Model != nil {
		return b.Model
	}
	for _, key := range []string{"nodes", "rows"} {
		if view, ok := b.Models[key]; ok {
			return &view.Model
		}
	}
	return nil
}

func (b PageDataBinding) ModelBindings() []*ModelBinding {
	result := []*ModelBinding{}
	if b.Model != nil {
		result = append(result, b.Model)
	}
	keys := make([]string, 0, len(b.Models))
	for key := range b.Models {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		model := b.Models[key].Model
		result = append(result, &model)
	}
	return result
}

func (b PageDataBinding) ObjectRef(p Page) AssetRef {
	if model := b.PrimaryModel(); model != nil {
		if b.Object.Name == model.Type {
			return b.Object
		}
		return ModelObject(model.Type)
	}
	if b.Object.Name != "" {
		return b.Object
	}
	return p.Object
}

func (p Page) RecordResourceObject(variable string) AssetRef {
	schema := p.InputSchema
	v := schema.Variables[variable]
	if schema.Interface != nil {
		for _, port := range schema.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source == nil {
		return AssetRef{}
	}
	if v.Source.Object != nil && (v.Mode == "shared" || v.Scope == "application") {
		return *v.Source.Object
	}
	if v.Source.Kind == "item" {
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID == v.Owner {
				return p.CollectionInputObject(scope.Bindings["collection"])
			}
		}
	}
	if v.Source.Kind != "record" {
		return AssetRef{}
	}
	for _, binding := range p.DataBindings {
		if binding.ID != v.Source.Section {
			continue
		}
		if binding.Kind == "presentation" {
			return AssetRef{}
		}
		if v.Source.Port != "" {
			for _, resource := range binding.Resources {
				if resource.Kind == "record-output" && resource.ID == v.Source.Port && resource.Outputs["record"] == variable && resource.Object != nil {
					return *resource.Object
				}
				if resource.Kind == "observation" {
					if v.Source.Port == "asset" && resource.Outputs["asset"] == variable && resource.Object != nil {
						return *resource.Object
					}
					if v.Source.Port == "row" && resource.Outputs["row"] == variable {
						return p.CollectionInputObject(binding.Ports["collectionVariable"])
					}
				}
			}
			return AssetRef{}
		}
		return binding.ObjectRef(p)
	}
	return AssetRef{}
}

func (p Page) WindowVariableInterface(variable string) string {
	schema := p.InputSchema
	v := schema.Variables[variable]
	if v.Source != nil && v.Source.Kind == "plan" {
		return schema.Queries[v.Source.Query].Interface
	}
	return ""
}

func (p Page) RecordVariableInterface(variable string) string {
	safe, err := p.Safety()
	if err != nil {
		return ""
	}
	v := safe.InputSchema.Variables[variable]
	if v.Source != nil {
		for _, binding := range safe.DataBindings {
			if binding.ID == v.Source.Section {
				return safe.WindowVariableInterface(binding.Ports["collectionVariable"])
			}
		}
	}
	return ""
}

func (b PageDataBinding) EffectiveFields(info EntityInfo) []FieldRef {
	if len(b.Fields) > 0 {
		return slices.Clone(b.Fields)
	}
	if b.Model != nil && len(b.Model.Fields) > 0 {
		fields := []FieldRef{}
		for _, field := range b.Model.Fields {
			if field.Slot == "" || field.Slot == "fields" {
				fields = append(fields, field)
			}
		}
		return fields
	}
	fields := []FieldRef{}
	for _, field := range info.Fields {
		if !field.Aside {
			fields = append(fields, FieldRef{Name: field.Name})
		}
	}
	return fields
}

func CompositeLimits() (records, cells, bytes int) {
	limits := pageWidgets.Runtime.Composite
	return limits.MaxRecords, limits.MaxCells, limits.MaxBytes
}

func MatrixRuleReads(c PageDataBinding) RuleReadSpec {
	return RuleReadSpec{Inputs: map[string]ExprType{"row": {Kind: "reference", Object: c.Models["rows"].Model.Type}, "column": {Kind: "reference", Object: c.Models["columns"].Model.Type}}}
}

func PageContentVersion(page Page) (string, error) {
	if page.retainedVersion != "" {
		return page.retainedVersion, nil
	}
	page.PresentationWords = nil
	body, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	body, err = CanonicalAssetBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "page.sha256." + hex.EncodeToString(sum[:]), nil
}

func CheckPageContentVersion(version string) error {
	text, ok := strings.CutPrefix(version, "page.sha256.")
	raw, err := hex.DecodeString(text)
	if !ok || err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != text {
		return fmt.Errorf("invalid page content version")
	}
	return nil
}

func ActionParameterCompatible(f FieldInfo, p Field) bool {
	if p.From != "" {
		return false
	}
	switch p.Type {
	case "string":
		if p.Ref != "" {
			return f.Type == "reference" && f.Ref == p.Ref
		}
		return slices.Contains([]string{"text", "longtext", "choice"}, f.Type)
	case "number":
		return f.Type == "decimal" || f.Type == "integer"
	case "integer":
		return f.Type == "integer"
	case "boolean", "date", "datetime":
		return f.Type == p.Type
	}
	return false
}

func (edit *PageInlineEdit) CheckTableEdit(info EntityInfo, action Action) error {
	if edit == nil {
		return nil
	}
	if edit.Action.App != info.App || edit.Action.Name != info.Type+".edit" || action.Schema != edit.Action.Name || action.Target != info.Type || action.New || action.NeedsApproval || action.Approval != nil || !slices.Contains(info.Standard, action.Schema) || slices.ContainsFunc(action.Payload, func(f Field) bool {
		return f.Required
	}) {
		return fmt.Errorf("table editing requires its original standard field patch action without extra required inputs or approval")
	}
	for _, name := range edit.Fields {
		f, ok := info.Field(name)
		if !ok || f.ReadOnly || f.Aside || info.Lifecycle != nil && info.Lifecycle.Field == name || !slices.Contains(pageWidgets.Runtime.TableEditing.FieldTypes, f.Type) || !slices.ContainsFunc(action.Payload, func(p Field) bool {
			return p.Name == name
		}) {
			return fmt.Errorf("table edit field is unavailable or not accepted by its original action")
		}
	}
	return nil
}

func (p Page) checkEnvelope() error {
	if len(p.DataBindings) > 128 || len(p.ActionTriggers) > 256 || len(p.ScopeVisibility.Scopes) > 512 || len(p.WidgetSpec.Body) > 2<<20 || !json.Valid(p.WidgetSpec.Body) {
		return fmt.Errorf("page safety declaration exceeds its bounded format")
	}
	return nil
}

func (b PageDataBinding) HasRecordOutput(port, variable string) bool {
	for _, r := range b.Resources {
		if r.Kind == "record-output" && r.ID == port && r.Outputs["record"] == variable {
			return true
		}
		if r.Kind == "observation" && (port == "row" && r.Outputs["row"] == variable || port == "asset" && r.Outputs["asset"] == variable) {
			return true
		}
	}
	return false
}

func (p Page) scopeOwner(owner, kind string) string {
	seen := map[string]bool{}
	for owner != "" && !seen[owner] {
		seen[owner] = true
		found := false
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID == owner {
				if scope.Kind == kind {
					return scope.ID
				}
				owner = scope.Parent
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	return ""
}

func (p Page) rootPresentationSource(source string) bool {
	owner := p.ScopeVisibility.Owners[source]
	if owner == "" {
		owner = source
	}
	return p.scopeOwner(owner, "overlay") == "" && p.scopeOwner(owner, "loop") == ""
}

func pageBindingVariables(b PageDataBinding) []string {
	out := []string{}
	for key, id := range b.Ports {
		if strings.HasSuffix(key, "Variable") {
			out = append(out, id)
		}
	}
	for _, r := range b.Resources {
		for _, v := range r.Inputs {
			out = append(out, v.Variable)
		}
		for _, id := range r.Outputs {
			out = append(out, id)
		}
	}
	return out
}

func defaultModelFields(widget string, info EntityInfo) []FieldRef {
	fields := []FieldRef{}
	if !slices.Contains([]string{"table", "detail", "form", "filter"}, widget) {
		return fields
	}
	for _, field := range info.Fields {
		if field.Aside || widget == "form" && field.ReadOnly || widget == "filter" && !slices.Contains(Filterable, field.Type) {
			continue
		}
		fields = append(fields, FieldRef{Name: field.Name})
	}
	return fields
}

func (p Page) ResolveBindings(lookup func(string) (EntityInfo, bool)) Page {
	p.DataBindings = slices.Clone(p.DataBindings)
	for i, binding := range p.DataBindings {
		if binding.Model == nil && len(binding.Models) == 0 && binding.Object.Name != "" && (binding.Kind == "record" || binding.Kind == "records") {
			binding.Model = &ModelBinding{Type: binding.Object.Name, Fields: slices.Clone(binding.Fields)}
		}
		binding.Models = maps.Clone(binding.Models)
		if binding.Model != nil {
			model := *binding.Model
			model.Fields = slices.Clone(model.Fields)
			for n, field := range model.Fields {
				if strings.Contains(field.Name, ".") {
					field.Path = strings.Split(field.Name, ".")
					field.Name = ""
				}
				model.Fields[n] = field
			}
			info, ok := lookup(model.Type)
			if ok {
				binding.Fields = binding.EffectiveFields(info)
				if len(model.Fields) == 0 {
					model.Fields = defaultModelFields(binding.Ports["widget"], info)
				}
				if model.Relation == nil && (binding.Ports["relation"] != "" || binding.Ports["parentSelection"] != "") {
					parentType := p.Object.Name
					for _, selection := range p.InputSchema.Selections {
						if selection.Name == binding.Ports["parentSelection"] {
							parentType = selection.Object.Name
						}
					}
					for _, field := range info.Fields {
						if field.Type == "reference" && field.Ref == parentType && (binding.Ports["relation"] == "" || field.Inverse == binding.Ports["relation"]) {
							model.Relation = &ModelRelation{ParentType: parentType, ParentSelection: binding.Ports["parentSelection"], Field: field.Name, Name: field.Inverse}
							break
						}
					}
				}
			}
			binding.Model = &model
		}
		for port, view := range binding.Models {
			info, ok := lookup(view.Model.Type)
			if !ok {
				continue
			}
			view.Roles = maps.Clone(view.Roles)
			if view.Roles == nil {
				view.Roles = map[string]FieldRef{}
			}
			if _, explicit := view.Roles["label"]; !explicit {
				if _, computed := view.Expressions["label"]; !computed && view.Model.Label == nil && info.Display != "" && info.Display != "id" {
					view.Roles["label"] = FieldRef{Name: info.Display}
				}
			}
			if _, explicit := view.Roles["state"]; !explicit {
				if _, computed := view.Expressions["state"]; !computed && info.Lifecycle != nil {
					view.Roles["state"] = FieldRef{Name: info.Lifecycle.Field}
				}
			}
			binding.Models[port] = view
		}
		p.DataBindings[i] = binding
	}
	return p
}
