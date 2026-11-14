package pages

import (
	"slices"
	"strings"

	"platformserver/platform"
)

func ModelRules(definitions []platform.Definition, criteria func(platform.AssetRef) (*platform.RuleCriteria, bool)) []PageModelRule {
	values := []PageModelRule{}
	for _, entry := range definitions {
		if entry.Action == nil {
			continue
		}
		if found, ok := criteria(entry.Ref); ok {
			values = append(values, PageModelRule{Action: entry.Ref, Title: entry.Action.Title, Criteria: *found})
		}
	}
	return values
}

func ModelFields(info platform.EntityInfo, model *platform.ModelBinding, lookup func(string) (platform.EntityInfo, bool)) platform.EntityInfo {
	if model == nil {
		return info
	}
	info.Fields = slices.Clone(info.Fields)
	for _, ref := range model.Fields {
		if len(ref.Segments()) < 2 {
			continue
		}
		field, err := platform.RecordPathField(model.Type, ref.Segments(), lookup)
		if err != nil {
			continue
		}
		field.Name, field.Title, field.ReadOnly = ref.Key(), strings.Join(ref.Segments(), " → "), true
		info.Fields = append(info.Fields, field)
	}
	return info
}
