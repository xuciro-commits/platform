package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var pageNodeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,79}$`)

type PageEmbedding struct {
	ReadOnly         bool                 `json:"readOnly,omitempty"`
	Kind             string               `json:"kind"`
	Page             AssetBinding         `json:"page"`
	ContentVersion   string               `json:"contentVersion"`
	InterfaceVersion int                  `json:"interfaceVersion"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Results          map[string]string    `json:"results,omitempty"`
}

func (e PageEmbedding) Navigation() PageNavigation {
	return PageNavigation{Page: e.Page.Ref, InterfaceVersion: e.InterfaceVersion, Inputs: e.Inputs, Results: e.Results}
}

func (p Page) EmbeddingBindings() []PageEmbedding {
	safe, err := p.Safety()
	if err != nil {
		return nil
	}
	result := []PageEmbedding{}
	for _, binding := range safe.DataBindings {
		for _, resource := range binding.Resources {
			if resource.Kind == "page" && resource.Asset != nil {
				result = append(result, PageEmbedding{ReadOnly: resource.ReadOnly, Kind: resource.Source, Page: *resource.Asset, ContentVersion: resource.ContentVersion, InterfaceVersion: resource.InterfaceVersion, Inputs: resource.Inputs, Results: resource.Outputs})
			}
		}
	}
	return result
}

func CheckPageEmbedding(parent Page, child Page, e PageEmbedding) error {
	digest, err := PageContentVersion(child)
	if err != nil || digest != e.ContentVersion || child.Name != e.Page.Ref.Name {
		return fmt.Errorf("embedded content identity differs")
	}
	return CheckPageNavigation(parent, child, e.Navigation())
}

func CheckPageEmbeddingGraph(root AssetRef, lookup map[AssetRef]ReleaseAsset) error {
	active := map[AssetRef]bool{}
	instances, queries, records, sections := 0, 0, 0, 0
	var visit func(AssetRef, int) error
	visit = func(ref AssetRef, depth int) error {
		if depth > pageWidgets.Runtime.Embedding.MaxDepth || active[ref] {
			return fmt.Errorf("embedded page cycle or depth budget exceeded")
		}
		asset, ok := lookup[ref]
		var p Page
		if !ok || json.Unmarshal(asset.Body, &p) != nil {
			return fmt.Errorf("embedded page is unavailable")
		}
		instances++
		safe, err := p.Safety()
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
		if instances > pageWidgets.Runtime.Embedding.MaxInstances || sections > pageWidgets.Runtime.Embedding.MaxSections {
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
		if queries > pageWidgets.Runtime.Embedding.MaxQueries || records > pageWidgets.Runtime.Embedding.MaxRecords {
			return fmt.Errorf("embedded page read budget exceeded")
		}
		active[ref] = true
		defer delete(active, ref)
		for _, e := range p.EmbeddingBindings() {
			child, ok := lookup[e.Page.Ref]
			var target Page
			if !ok || child.SourceVersion != e.Page.SourceVersion || json.Unmarshal(child.Body, &target) != nil {
				return fmt.Errorf("embedded original page version is unavailable")
			}
			if err := CheckPageEmbedding(p, target, e); err != nil {
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
