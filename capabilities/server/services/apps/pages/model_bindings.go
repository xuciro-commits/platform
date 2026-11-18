package pages

import (
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

func NormalizeModelBindings(p *platform.Page, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	safe, err := Safety(*p)
	if err != nil {
		return err
	}
	safe = resolveBindings(safe, lookup)
	safe.RetainedVersion = ""
	*p = safe
	return nil
}

func CheckModelBindings(p platform.Page, lookup func(string) (platform.RecordTypeInfo, bool), criteria func(platform.AssetRef) (*platform.RuleCriteria, bool), engines ...platform.RuleEngine) error {
	safe, err := Safety(p)
	if err != nil {
		return err
	}
	for _, binding := range safe.DataBindings {
		if err := checkDataBinding(safe, binding, lookup, criteria, engines...); err != nil {
			return err
		}
	}
	return nil
}

func effectiveFields(b platform.PageDataBinding, info platform.RecordTypeInfo) []platform.FieldRef {
	if len(b.Fields) > 0 {
		return slices.Clone(b.Fields)
	}
	if b.Model != nil && len(b.Model.Fields) > 0 {
		fields := []platform.FieldRef{}
		for _, field := range b.Model.Fields {
			if field.Slot == "" || field.Slot == "fields" {
				fields = append(fields, field)
			}
		}
		return fields
	}
	fields := []platform.FieldRef{}
	for _, field := range info.Fields {
		if !field.Aside {
			fields = append(fields, platform.FieldRef{Name: field.Name})
		}
	}
	return fields
}

func defaultModelFields(widget string, info platform.RecordTypeInfo) []platform.FieldRef {
	fields := []platform.FieldRef{}
	if !slices.Contains([]string{"table", "detail", "form", "filter"}, widget) {
		return fields
	}
	for _, field := range info.Fields {
		if field.Aside || widget == "form" && field.ReadOnly || widget == "filter" && !slices.Contains(platform.Filterable, field.Type) {
			continue
		}
		fields = append(fields, platform.FieldRef{Name: field.Name})
	}
	return fields
}

func resolveBindings(p platform.Page, lookup func(string) (platform.RecordTypeInfo, bool)) platform.Page {
	p.DataBindings = slices.Clone(p.DataBindings)
	for i, binding := range p.DataBindings {
		if binding.Model == nil && len(binding.Models) == 0 && binding.Object.Name != "" && (binding.Kind == "record" || binding.Kind == "records") {
			binding.Model = &platform.ModelBinding{Type: binding.Object.Name, Fields: slices.Clone(binding.Fields)}
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
				binding.Fields = effectiveFields(binding, info)
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
							model.Relation = &platform.ModelRelation{ParentType: parentType, ParentSelection: binding.Ports["parentSelection"], Field: field.Name, Name: field.Inverse}
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
				view.Roles = map[string]platform.FieldRef{}
			}
			if _, explicit := view.Roles["label"]; !explicit {
				if _, computed := view.Expressions["label"]; !computed && view.Model.Label == nil && info.Display != "" && info.Display != "id" {
					view.Roles["label"] = platform.FieldRef{Name: info.Display}
				}
			}
			if _, explicit := view.Roles["state"]; !explicit {
				if _, computed := view.Expressions["state"]; !computed && info.Lifecycle != nil {
					view.Roles["state"] = platform.FieldRef{Name: info.Lifecycle.Field}
				}
			}
			binding.Models[port] = view
		}
		p.DataBindings[i] = binding
	}
	return p
}
