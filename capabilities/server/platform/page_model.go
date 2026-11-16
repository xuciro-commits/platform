package platform

import (
	"encoding/json"
	"fmt"
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

func (p Page) CheckEnvelope() error {
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

func (p Page) ScopeOwner(owner, kind string) string {
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

func PageBindingVariables(b PageDataBinding) []string {
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
