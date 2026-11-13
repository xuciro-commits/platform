package pages

import "platformserver/platform"

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
