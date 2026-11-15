package publication

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func checkObjectView(object platform.AssetRef, view platform.ObjectView, assets map[platform.AssetRef]platform.ReleaseAsset) error {
	if err := view.Check(); err != nil {
		return err
	}
	var app platform.Application
	if asset, ok := assets[view.Application]; !ok || json.Unmarshal(asset.Body, &app) != nil || !slices.Contains(app.Pages, view.Page.Name) {
		return fmt.Errorf("Object view page must belong to its saved module")
	}
	var page platform.Page
	if asset, ok := assets[view.Page]; !ok || json.Unmarshal(asset.Body, &page) != nil || page.Object != object {
		return fmt.Errorf("Object view page must read its bound object type")
	}
	schema := page.InputSchema
	if schema.Interface == nil {
		return fmt.Errorf("Object view page needs a required currentObject record input for its bound object type")
	}
	input, ok := schema.Interface.Inputs["currentObject"]
	if !ok || input.Type != "record" || !input.Required || input.Object == nil || *input.Object != object {
		return fmt.Errorf("Object view page needs a required currentObject record input for its bound object type")
	}
	for name, port := range schema.Interface.Inputs {
		if name != "currentObject" && port.Required {
			return fmt.Errorf("Object view page cannot require inputs other than currentObject")
		}
	}
	return nil
}

func pageNavigationEdge(asset platform.ReleaseAsset, target platform.AssetRef) bool {
	if asset.Ref.Kind != platform.AssetPage || target.Kind != platform.AssetPage {
		return false
	}
	var p platform.Page
	if json.Unmarshal(asset.Body, &p) != nil {
		return false
	}
	for _, ref := range p.NavigationTargets() {
		if ref == target {
			return true
		}
	}
	return false
}

func frozenPageInterfaceQuery(q platform.PageQuery, lookup map[platform.AssetRef]platform.ReleaseAsset) (*platform.Definition, error) {
	if q.Query == nil {
		return nil, fmt.Errorf("interface page query needs a published binding")
	}
	asset, ok := lookup[q.Query.Ref]
	var named platform.NamedQuery
	if !ok || asset.SourceVersion != q.Query.SourceVersion || json.Unmarshal(asset.Body, &named) != nil || named.InterfaceShape == nil || named.Check() != nil {
		return nil, fmt.Errorf("frozen interface query version or shape is unavailable")
	}
	for _, ref := range named.Dependencies() {
		implementation, exists := lookup[ref]
		info, err := queryObjectDescriptor(implementation.Body)
		if !exists || err != nil || info.Type != ref.Name || info.App != ref.App || !slices.Contains(info.Implements, named.Interface) || named.InterfaceShape.Implements(info) != nil {
			return nil, fmt.Errorf("frozen interface query implementation %s is unavailable", ref.Name)
		}
	}
	return &platform.Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &named}, nil
}
