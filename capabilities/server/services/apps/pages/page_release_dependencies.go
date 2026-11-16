package pages

import (
	"encoding/json"

	"platformserver/platform"
)

func modelDependencies(p platform.Page) []platform.AssetRef {
	refs := []platform.AssetRef{}
	safe, err := Safety(p)
	if err != nil {
		return refs
	}
	for _, binding := range safe.DataBindings {
		for _, model := range binding.ModelBindings() {
			if safe.WindowVariableInterface(binding.Ports["collectionVariable"]) == "" {
				ref := platform.ModelObject(model.Type)
				if binding.Object.Name == model.Type {
					ref = binding.Object
				} else if safe.Object.Name == model.Type {
					ref = safe.Object
				}
				refs = append(refs, ref)
			}
			if model.Relation != nil {
				refs = append(refs, platform.ModelObject(model.Relation.ParentType))
			}
			sets := []*platform.ModelRecordSet{model.Set}
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

func navigationTargets(p platform.Page) []platform.AssetRef {
	refs := []platform.AssetRef{}
	safe, err := Safety(p)
	if err != nil {
		return refs
	}
	for _, trigger := range safe.ActionTriggers {
		for _, event := range trigger.Events {
			if nav := eventNavigation(event); nav != nil {
				refs = append(refs, nav.Page)
			}
		}
	}
	if iface := safe.InputSchema.Interface; iface != nil {
		for _, ports := range []map[string]platform.PagePort{iface.Inputs, iface.Outputs} {
			for _, port := range ports {
				if port.Object != nil {
					refs = append(refs, *port.Object)
				}
			}
		}
	}
	return refs
}

func PageDependencies(original platform.Page) []platform.AssetRef {
	page, err := Safety(original)
	if err != nil {
		return nil
	}
	refs := []platform.AssetRef{}
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, modelDependencies(page)...)
	refs = append(refs, QueryReferences(page)...)
	refs = append(refs, navigationTargets(page)...)
	for _, compute := range computeResources(page) {
		refs = append(refs, compute.Operation.Ref)
	}
	for _, selection := range page.InputSchema.Selections {
		refs = append(refs, selection.Object)
	}
	for _, binding := range page.DataBindings {
		if binding.Object.Name != "" {
			refs = append(refs, binding.Object)
		}
		if binding.Query != nil {
			refs = append(refs, binding.Query.Ref)
		}
		for _, resource := range binding.Resources {
			if resource.Asset != nil {
				refs = append(refs, resource.Asset.Ref)
			}
			if resource.Object != nil {
				refs = append(refs, *resource.Object)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		refs = append(refs, trigger.Actions...)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		if trigger.Function != nil {
			refs = append(refs, trigger.Function.Ref)
		}
		if trigger.Operation != nil {
			refs = append(refs, trigger.Operation.Ref)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
	}
	return refs
}

func PageNavigationEdge(asset platform.ReleaseAsset, target platform.AssetRef) bool {
	if asset.Ref.Kind != platform.AssetPage || target.Kind != platform.AssetPage {
		return false
	}
	var p platform.Page
	if json.Unmarshal(asset.Body, &p) != nil {
		return false
	}
	for _, ref := range navigationTargets(p) {
		if ref == target {
			return true
		}
	}
	return false
}
