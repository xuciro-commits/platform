package platformserver

import (
	"platformserver/platform"
	"slices"
)

func explorationAssetAvailable(definitions []platform.Definition, binding platform.AssetBinding) bool {
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
func filterExplorationAssets(definitions []platform.Definition) []platform.Definition {
	for i, d := range definitions {
		if d.Page == nil || d.Page.Document == nil {
			continue
		}
		page := *d.Page
		page.Sections = slices.Clone(page.Sections)
		changed := false
		for j, s := range page.Sections {
			if s.AssetDirectory == nil {
				continue
			}
			directory := *s.AssetDirectory
			directory.Items = slices.DeleteFunc(slices.Clone(directory.Items), func(item platform.PageAssetDirectoryItem) bool {
				return !explorationAssetAvailable(definitions, item.Asset)
			})
			if len(directory.Items) != len(s.AssetDirectory.Items) {
				changed = true
			}
			page.Sections[j].AssetDirectory = &directory
		}
		if !changed {
			continue
		}
		page.Sections = slices.DeleteFunc(page.Sections, func(s platform.Section) bool { return s.AssetDirectory != nil && len(s.AssetDirectory.Items) == 0 })
		doc := *page.Document
		doc.Events = slices.DeleteFunc(slices.Clone(doc.Events), func(e platform.PageEventBinding) bool {
			for _, s := range page.Sections {
				if s.ID == e.Source && s.AssetDirectory != nil {
					return !slices.ContainsFunc(s.AssetDirectory.Items, func(item platform.PageAssetDirectoryItem) bool { return item.ID == e.Control })
				}
			}
			return false
		})
		page.Document = doc.Visible(page.Sections)
		definitions[i].Page = &page
	}
	return definitions
}
