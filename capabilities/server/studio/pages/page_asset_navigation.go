package pages

import (
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
)

// Discovery uses the same member-filtered assets. Hidden input sources and
// target pages remove their dependent buttons without exposing placeholders.
func FilterNavigation(definitions []platform.Definition) []platform.Definition {
	visible := map[platform.AssetRef]bool{}
	for _, d := range definitions {
		visible[d.Ref] = true
	}
	for i, d := range definitions {
		if d.Page == nil {
			continue
		}
		page, err := d.Page.Safety()
		if err != nil {
			continue
		}
		page.InputSchema.Variables = maps.Clone(page.InputSchema.Variables)
		if page.InputSchema.Interface != nil {
			for _, port := range page.InputSchema.Interface.Inputs {
				if port.Object != nil && !visible[*port.Object] {
					delete(page.InputSchema.Variables, port.Variable)
				}
			}
		}
		removed := map[string]bool{}
		controls := map[string]map[string]bool{}
		page.ActionTriggers = slices.Clone(page.ActionTriggers)
		for j, trigger := range page.ActionTriggers {
			trigger.Events = slices.DeleteFunc(slices.Clone(trigger.Events), func(e platform.PageEventBinding) bool {
				if n := e.Navigation(); n != nil && !visible[n.Page] {
					directory := slices.ContainsFunc(page.DataBindings, func(binding platform.PageDataBinding) bool {
						return binding.ID == e.Source && slices.ContainsFunc(binding.Resources, func(resource platform.PageResourceBinding) bool {
							return resource.Kind == "asset" && resource.ID == e.Control
						})
					})
					if directory {
						if controls[e.Source] == nil {
							controls[e.Source] = map[string]bool{}
						}
						controls[e.Source][e.Control] = true
					} else {
						removed[e.Source] = true
					}
					return true
				}
				return false
			})
			page.ActionTriggers[j] = trigger
		}
		page.DataBindings = slices.Clone(page.DataBindings)
		page.DataBindings = slices.DeleteFunc(page.DataBindings, func(binding platform.PageDataBinding) bool {
			if removed[binding.ID] {
				return true
			}
			for _, resource := range binding.Resources {
				if resource.Kind == "page" && resource.Asset != nil && !visible[resource.Asset.Ref] {
					return true
				}
			}
			if record := binding.Ports["recordVariable"]; record != "" {
				_, exists := page.InputSchema.Variables[record]
				return !exists
			}
			return false
		})
		for j, binding := range page.DataBindings {
			if len(controls[binding.ID]) > 0 {
				binding.Resources = slices.DeleteFunc(slices.Clone(binding.Resources), func(resource platform.PageResourceBinding) bool {
					return resource.Kind == "asset" && controls[binding.ID][resource.ID]
				})
				page.DataBindings[j] = binding
			}
		}
		page = page.VisibleSafety()
		page = d.Page.WithSafetyProjection(page)
		definitions[i].Page = &page
	}
	return definitions
}

func CheckNavigation(p platform.Page, owner string, definitions []platform.Definition) error {
	page, err := p.Safety()
	if err != nil {
		return err
	}
	for _, trigger := range page.ActionTriggers {
		for _, event := range trigger.Events {
			nav := event.Navigation()
			if nav == nil {
				continue
			}
			var target *platform.Page
			for _, definition := range definitions {
				if definition.Ref == nav.Page {
					target = definition.Page
					break
				}
			}
			if nav.Page == (platform.AssetRef{App: owner, Kind: platform.AssetPage, Name: p.Name}) {
				target = &p
			}
			if target == nil {
				return fmt.Errorf("page %s navigation target %s is unavailable", p.Name, nav.Page)
			}
			if err := platform.CheckPageNavigation(p, *target, *nav); err != nil {
				return err
			}
		}
	}
	return nil
}

func ExplorationAssetAvailable(definitions []platform.Definition, binding platform.AssetBinding) bool {
	for _, d := range definitions {
		if d.Ref != binding.Ref {
			continue
		}
		switch d.Ref.Kind {
		case platform.AssetLinkType:
			selected := d.LinkVersion(binding.SourceVersion)
			return selected != nil && selected.LinkType != nil
		case platform.AssetQuery:
			selected := d.QueryVersion(binding.SourceVersion)
			return selected != nil && selected.Query != nil
		case platform.AssetPropertyType:
			selected := d.PropertyVersion(binding.SourceVersion)
			return selected != nil && selected.PropertyType != nil
		default:
			return d.Version == binding.SourceVersion
		}
	}
	return false
}
func FilterExplorationAssets(definitions []platform.Definition) []platform.Definition {
	for i, d := range definitions {
		if d.Page == nil {
			continue
		}
		page, err := d.Page.Safety()
		if err != nil {
			continue
		}
		page.DataBindings = slices.Clone(page.DataBindings)
		removed := map[string]map[string]bool{}
		for j, binding := range page.DataBindings {
			binding.Resources = slices.DeleteFunc(slices.Clone(binding.Resources), func(resource platform.PageResourceBinding) bool {
				if resource.Kind != "asset" || resource.Asset == nil || ExplorationAssetAvailable(definitions, *resource.Asset) {
					return false
				}
				if removed[binding.ID] == nil {
					removed[binding.ID] = map[string]bool{}
				}
				removed[binding.ID][resource.ID] = true
				return true
			})
			page.DataBindings[j] = binding
		}
		if len(removed) == 0 {
			continue
		}
		page.ActionTriggers = slices.Clone(page.ActionTriggers)
		for j, trigger := range page.ActionTriggers {
			trigger.Events = slices.DeleteFunc(slices.Clone(trigger.Events), func(e platform.PageEventBinding) bool { return removed[e.Source][e.Control] })
			page.ActionTriggers[j] = trigger
		}
		page = page.VisibleSafety()
		page = d.Page.WithSafetyProjection(page)
		definitions[i].Page = &page
	}
	return definitions
}
