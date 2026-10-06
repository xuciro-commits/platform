package platformserver

import (
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
)

// Discovery uses the same member-filtered assets. Hidden input sources and
// target pages remove their dependent buttons without exposing placeholders.
func filterPageNavigation(definitions []platform.Definition) []platform.Definition {
	visible := map[platform.AssetRef]bool{}
	for _, d := range definitions {
		visible[d.Ref] = true
	}
	for i, d := range definitions {
		if d.Page == nil || d.Page.Document == nil {
			continue
		}
		page := *d.Page
		doc := *page.Document
		doc.Variables = maps.Clone(doc.Variables)
		if doc.Interface != nil {
			for _, port := range doc.Interface.Inputs {
				if port.Object != nil && !visible[*port.Object] {
					delete(doc.Variables, port.Variable)
				}
			}
		}
		removed := map[string]bool{}
		directoryControls := map[string]map[string]bool{}
		doc.Events = slices.DeleteFunc(slices.Clone(doc.Events), func(e platform.PageEventBinding) bool {
			if n := e.Navigation(); n != nil && !visible[n.Page] {
				directory := slices.ContainsFunc(page.Sections, func(s platform.Section) bool {
					return s.ID == e.Source && s.Widget == "asset-directory" && s.AssetDirectory != nil
				})
				if directory {
					if directoryControls[e.Source] == nil {
						directoryControls[e.Source] = map[string]bool{}
					}
					directoryControls[e.Source][e.Control] = true
				} else {
					removed[e.Source] = true
				}
				return true
			}
			return false
		})
		page.Sections = slices.Clone(page.Sections)
		for j, s := range page.Sections {
			controls := directoryControls[s.ID]
			if len(controls) == 0 || s.AssetDirectory == nil {
				continue
			}
			directory := *s.AssetDirectory
			directory.Items = slices.DeleteFunc(slices.Clone(directory.Items), func(item platform.PageAssetDirectoryItem) bool { return controls[item.ID] })
			page.Sections[j].AssetDirectory = &directory
		}
		page.Sections = slices.DeleteFunc(page.Sections, func(s platform.Section) bool {
			_, has := doc.Variables[s.RecordVariable]
			return s.Embedding != nil && !visible[s.Embedding.Page.Ref] || removed[s.ID] || s.RecordVariable != "" && !has || s.AssetDirectory != nil && len(s.AssetDirectory.Items) == 0
		})
		page.Document = doc.Visible(page.Sections)
		for {
			nodes := map[string]bool{}
			for _, node := range page.Document.Nodes {
				if node.Kind == "widget" {
					nodes[node.Section] = true
				}
			}
			before := len(page.Sections)
			page.Sections = slices.DeleteFunc(page.Sections, func(s platform.Section) bool { return !nodes[s.ID] })
			if before == len(page.Sections) {
				break
			}
			page.Document = page.Document.Visible(page.Sections)
		}
		definitions[i].Page = &page
	}
	return definitions
}

func (t *Tenant) checkPageNavigation(p platform.Page, owner string) error {
	if p.Document == nil {
		return nil
	}
	for _, event := range p.Document.Events {
		nav := event.Navigation()
		if nav == nil {
			continue
		}
		var target *platform.Page
		for _, definition := range t.definitions {
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
	return nil
}
