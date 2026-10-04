package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

type PageEmbedding struct {
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
func (s Section) EmbeddingVariables() []string {
	var ids []string
	if s.Embedding != nil {
		for _, v := range s.Embedding.Inputs {
			if v.Variable != "" {
				ids = append(ids, v.Variable)
			}
		}
		for _, id := range s.Embedding.Results {
			ids = append(ids, id)
		}
	}
	return ids
}
func (p Page) EmbeddingBindings() []PageEmbedding {
	var result []PageEmbedding
	for _, s := range p.Sections {
		if s.Embedding != nil {
			result = append(result, *s.Embedding)
		}
	}
	return result
}
func (d *PageDocument) checkEmbedding(s Section) error {
	if s.Widget != "embedded-page" {
		if s.Embedding != nil {
			return fmt.Errorf("page embedding needs its widget")
		}
		return nil
	}
	e := s.Embedding
	if e == nil || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.83") || !slices.Contains([]string{"module", "custom", "dashboard"}, e.Kind) || e.Page.Ref.Check() != nil || e.Page.Ref.Kind != AssetPage || e.Page.SourceVersion == "" || CheckPageContentVersion(e.ContentVersion) != nil || e.InterfaceVersion < 0 || len(e.Inputs) > 16 || len(e.Results) > 16 || s.Object != (AssetRef{}) || s.RecordVariable != "" || s.CollectionVariable != "" || s.Query != (AssetRef{}) || s.Operation != nil || s.Function != nil || len(s.Fields) > 0 || len(s.Actions) > 0 || len(s.Inputs) > 0 || s.Selection != "" || s.FilterVariable != "" || s.ParentSelection != "" || s.Relation != "" {
		return fmt.Errorf("embedding needs exact original page content and finite typed bindings")
	}
	for id, v := range e.Inputs {
		if !pageNodeID.MatchString(id) || (v.Variable != "") == (len(v.Literal) > 0) {
			return fmt.Errorf("embedding input needs one original variable or literal")
		}
		if v.Variable != "" {
			if _, ok := d.Variables[v.Variable]; !ok {
				return fmt.Errorf("embedding input variable is unavailable")
			}
		} else if len(v.Literal) > 4096 || !slices.Contains([]string{"string", "boolean", "decimal"}, pageLiteralType(v.Literal)) {
			return fmt.Errorf("embedding literal is incompatible")
		}
	}
	seen := map[string]bool{}
	for id, value := range e.Results {
		v, ok := d.Variables[value]
		if !pageNodeID.MatchString(id) || !ok || !v.IsWritable() || v.Type == "record" || seen[value] {
			return fmt.Errorf("embedding result needs a unique writable original scalar")
		}
		seen[value] = true
	}
	return nil
}
func CheckPageEmbedding(parent Page, child Page, e PageEmbedding) error {
	digest, err := PageContentVersion(child)
	if err != nil || digest != e.ContentVersion || child.Name != e.Page.Ref.Name {
		return fmt.Errorf("embedded content identity differs")
	}
	return CheckPageNavigation(parent, child, e.Navigation())
}

// Embedding expands instances, not merely unique assets. Every root is bounded
// and cycles are rejected independently of navigation's mutual-link exception.
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
		sections += len(p.Sections)
		if instances > pageWidgets.Runtime.Embedding.MaxInstances || sections > pageWidgets.Runtime.Embedding.MaxSections {
			return fmt.Errorf("embedded page instance budget exceeded")
		}
		if p.Document != nil {
			queries += len(p.Document.Queries)
			for _, q := range p.Document.Queries {
				factor := 1
				if q.ItemOwner != "" {
					var err error
					factor, err = p.Document.loopFactor(q.ItemOwner)
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
