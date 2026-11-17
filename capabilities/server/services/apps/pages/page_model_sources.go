package pages

import (
	"platformserver/platform"
	"platformserver/services/ontology"
	"slices"
	"strings"
)

func primaryModel(b platform.PageDataBinding) *platform.ModelBinding {
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

func ModelBindings(b platform.PageDataBinding) []*platform.ModelBinding {
	result := []*platform.ModelBinding{}
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

func bindingObjectRef(b platform.PageDataBinding, p platform.Page) platform.AssetRef {
	if model := primaryModel(b); model != nil {
		if b.Object.Name == model.Type {
			return b.Object
		}
		return ontology.ModelObject(model.Type)
	}
	if b.Object.Name != "" {
		return b.Object
	}
	return p.Object
}

func recordResourceObject(p platform.Page, variable string) platform.AssetRef {
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
		return platform.AssetRef{}
	}
	if v.Source.Object != nil && (v.Mode == "shared" || v.Scope == "application") {
		return *v.Source.Object
	}
	if v.Source.Kind == "item" {
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID == v.Owner {
				return collectionInputObject(p, scope.Bindings["collection"])
			}
		}
	}
	if v.Source.Kind != "record" {
		return platform.AssetRef{}
	}
	for _, binding := range p.DataBindings {
		if binding.ID != v.Source.Section {
			continue
		}
		if binding.Kind == "presentation" {
			return platform.AssetRef{}
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
						return collectionInputObject(p, binding.Ports["collectionVariable"])
					}
				}
			}
			return platform.AssetRef{}
		}
		return bindingObjectRef(binding, p)
	}
	return platform.AssetRef{}
}

func windowVariableInterface(p platform.Page, variable string) string {
	schema := p.InputSchema
	v := schema.Variables[variable]
	if v.Source != nil && v.Source.Kind == "plan" {
		return schema.Queries[v.Source.Query].Interface
	}
	return ""
}

func hasRecordOutput(b platform.PageDataBinding, port, variable string) bool {
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

func scopeOwner(p platform.Page, owner, kind string) string {
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

func collectionInputObject(p platform.Page, variable string) platform.AssetRef {
	return collectionInputObjectSeen(p, variable, map[string]bool{})
}

func collectionInputObjectSeen(p platform.Page, variable string, seen map[string]bool) platform.AssetRef {
	if seen[variable] {
		return platform.AssetRef{}
	}
	seen[variable] = true
	schema := p.InputSchema
	v := schema.Variables[variable]
	if v.Type != "object-set" {
		return platform.AssetRef{}
	}
	if v.Mode == "input" && schema.Interface != nil {
		for _, port := range schema.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source == nil {
		return platform.AssetRef{}
	}
	if v.Source.Object != nil && v.Mode == "shared" {
		return *v.Source.Object
	}
	if v.Source.Kind == "plan" {
		return schema.Queries[v.Source.Query].Object
	}
	if v.Source.Kind == "query" {
		for _, binding := range p.DataBindings {
			if binding.ID == v.Source.Section {
				if source := binding.Ports["collectionVariable"]; source != "" && source != variable {
					return collectionInputObjectSeen(p, source, seen)
				}
				return bindingObjectRef(binding, p)
			}
		}
	}
	return platform.AssetRef{}
}

func recordVariableObject(p platform.Page, variable string) string {
	return recordResourceObject(p, variable).Name
}

func windowVariableObject(p platform.Page, id string) string {
	return collectionInputObject(p, id).Name
}

func variableIsWritable(v platform.PageVariable) bool {
	return v.Mode == "state" || v.Mode == "shared" && v.Writable
}

func fieldSegments(f platform.FieldRef) []string {
	if len(f.Path) > 0 {
		return f.Path
	}
	return []string{f.Name}
}

func fieldKey(f platform.FieldRef) string {
	return strings.Join(fieldSegments(f), ".")
}
