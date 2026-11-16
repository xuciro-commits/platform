package pages

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

func embeddingBindings(p platform.Page) []platform.PageEmbedding {
	safe, err := Safety(p)
	if err != nil {
		return nil
	}
	result := []platform.PageEmbedding{}
	for _, binding := range safe.DataBindings {
		for _, resource := range binding.Resources {
			if resource.Kind == "page" && resource.Asset != nil {
				result = append(result, platform.PageEmbedding{ReadOnly: resource.ReadOnly, Kind: resource.Source, Page: *resource.Asset, ContentVersion: resource.ContentVersion, InterfaceVersion: resource.InterfaceVersion, Inputs: resource.Inputs, Results: resource.Outputs})
			}
		}
	}
	return result
}

func checkPageEmbedding(parent platform.Page, child platform.Page, e platform.PageEmbedding) error {
	digest, err := PageContentVersion(child)
	if err != nil || digest != e.ContentVersion || child.Name != e.Page.Ref.Name {
		return fmt.Errorf("embedded content identity differs")
	}
	return CheckPageNavigation(parent, child, platform.PageNavigation{Page: e.Page.Ref, InterfaceVersion: e.InterfaceVersion, Inputs: e.Inputs, Results: e.Results})
}

func CheckPageEmbeddingGraph(root platform.AssetRef, lookup map[platform.AssetRef]platform.ReleaseAsset) error {
	active := map[platform.AssetRef]bool{}
	instances, queries, records, sections := 0, 0, 0, 0
	var visit func(platform.AssetRef, int) error
	visit = func(ref platform.AssetRef, depth int) error {
		if depth > platform.PageWidgets.Runtime.Embedding.MaxDepth || active[ref] {
			return fmt.Errorf("embedded page cycle or depth budget exceeded")
		}
		asset, ok := lookup[ref]
		var p platform.Page
		if !ok || json.Unmarshal(asset.Body, &p) != nil {
			return fmt.Errorf("embedded page is unavailable")
		}
		instances++
		safe, err := Safety(p)
		if err != nil {
			return err
		}
		for _, binding := range safe.DataBindings {
			for _, resource := range binding.Resources {
				if resource.Kind == "external-document" {
					instances++
				}
			}
		}
		sections += len(safe.DataBindings)
		if instances > platform.PageWidgets.Runtime.Embedding.MaxInstances || sections > platform.PageWidgets.Runtime.Embedding.MaxSections {
			return fmt.Errorf("embedded page instance budget exceeded")
		}
		if len(safe.InputSchema.Queries) > 0 {
			plans := safe.ActiveQueryPlans()
			queries += len(plans)
			for id, q := range safe.InputSchema.Queries {
				if !plans[id] {
					continue
				}
				factor := 1
				if q.ItemOwner != "" {
					var err error
					factor, err = safe.LoopFactor(q.ItemOwner)
					if err != nil {
						return err
					}
				}
				records += q.Limit * factor
			}
		}
		if queries > platform.PageWidgets.Runtime.Embedding.MaxQueries || records > platform.PageWidgets.Runtime.Embedding.MaxRecords {
			return fmt.Errorf("embedded page read budget exceeded")
		}
		active[ref] = true
		defer delete(active, ref)
		for _, e := range embeddingBindings(p) {
			child, ok := lookup[e.Page.Ref]
			var target platform.Page
			if !ok || child.SourceVersion != e.Page.SourceVersion || json.Unmarshal(child.Body, &target) != nil {
				return fmt.Errorf("embedded original page version is unavailable")
			}
			if err := checkPageEmbedding(p, target, e); err != nil {
				return err
			}
			if err := visit(e.Page.Ref, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, 0)
}
