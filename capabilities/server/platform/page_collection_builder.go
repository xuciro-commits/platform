package platform

import (
	"fmt"
	"slices"
)

// PageCollectionBuilder constrains the original fields offered by the editor.
// Runtime applied conditions are transient inputs, never saved record arrays.
type PageCollectionBuilder struct {
	Fields []string `json:"fields"`
}

func (d *PageDocument) collectionBuilderInput(id, owner string, object AssetRef, sections []Section) bool {
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.88") {
		return false
	}
	v := d.Variables[id]
	scope := "page"
	if owner != "" {
		scope = "overlay"
	}
	if v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "query" || v.Owner != owner || v.Scope != scope || v.Writable || v.Source.Query != "" || v.Source.Variable != "" || v.Source.Node != "" || v.Source.Object != nil || len(v.Source.Fields) > 0 || v.Source.Measure != "" || v.Source.Field != "" || v.Source.Port != "" {
		return false
	}
	for _, s := range sections {
		if s.ID != v.Source.Section || s.Widget != "collection-builder" || s.CollectionOutputVariable != id {
			continue
		}
		input := d.Variables[s.CollectionVariable]
		if input.Source == nil || input.Source.Kind != "plan" {
			return false
		}
		q, ok := d.Queries[input.Source.Query]
		return ok && q.Owner == owner && q.Object == object
	}
	return false
}
func (d *PageDocument) checkCollectionBuilder(s Section, sections []Section) error {
	if s.Widget != "collection-builder" {
		if s.CollectionBuilder != nil || s.CollectionOutputVariable != "" {
			return fmt.Errorf("collection builder configuration needs its widget")
		}
		return nil
	}
	if s.CollectionBuilder == nil || len(s.CollectionBuilder.Fields) < 1 || len(s.CollectionBuilder.Fields) > 32 || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.88") {
		return fmt.Errorf("collection builder needs bounded field configuration and its renderer profile")
	}
	names := map[string]bool{}
	for _, field := range s.CollectionBuilder.Fields {
		if !pageNodeID.MatchString(field) || names[field] {
			return fmt.Errorf("collection builder fields are invalid")
		}
		names[field] = true
	}
	v := d.Variables[s.CollectionVariable]
	if v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || v.Type != "object-set" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
		return fmt.Errorf("collection builder needs its original plan")
	}
	q, ok := d.Queries[v.Source.Query]
	if !ok || s.CollectionVariable == s.CollectionOutputVariable || !d.collectionBuilderInput(s.CollectionOutputVariable, q.Owner, q.Object, sections) || v.Owner != q.Owner || v.Scope != d.Variables[s.CollectionOutputVariable].Scope || len(s.Fields) > 0 || len(s.Actions) > 0 || s.RecordVariable != "" || s.Selection != "" || s.SelectionVariable != "" || s.FilterVariable != "" || s.Query != (AssetRef{}) || s.ParentSelection != "" || s.Relation != "" {
		return fmt.Errorf("collection builder needs a distinct same-owner original output")
	}
	return nil
}
func (s Section) CheckCollectionBuilderFields(info EntityInfo) error {
	if s.Widget != "collection-builder" {
		return nil
	}
	if s.CollectionBuilder == nil {
		return fmt.Errorf("collection builder configuration is missing")
	}
	for _, name := range s.CollectionBuilder.Fields {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"text", "choice", "integer", "decimal"}, f.Type) {
			return fmt.Errorf("collection builder field is unavailable: %s", name)
		}
	}
	return nil
}

// Query input edges follow the actual producer plan. Set and builder edges
// share the original finite budget and cannot feed back into their own source.
func (d *PageDocument) checkCollectionBuilderGraph(sections []Section) error {
	c := pageWidgets.Runtime.Query
	for root := range d.Queries {
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if !ok || path[id] || depth > c.Set.MaxDepth || nodes > c.Set.MaxNodes {
				return fmt.Errorf("collection sources are cyclic or exceed their budget")
			}
			path[id] = true
			defer delete(path, id)
			if q.Set != nil {
				for _, input := range q.Set.Inputs {
					if err := visit(input, depth+1, path); err != nil {
						return err
					}
				}
			}
			v := d.Variables[q.Input]
			if q.Input != "" && v.Mode != "input" {
				if !d.collectionBuilderInput(q.Input, q.Owner, q.Object, sections) {
					return fmt.Errorf("collection input producer is unavailable")
				}
				for _, s := range sections {
					if s.ID == v.Source.Section {
						if err := visit(d.Variables[s.CollectionVariable].Source.Query, depth+1, path); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	for _, s := range sections {
		v := d.Variables[s.CollectionVariable]
		if v.Source != nil && v.Source.Kind == "query" {
			for _, producer := range sections {
				if producer.ID == v.Source.Section && producer.Widget == "collection-builder" {
					return fmt.Errorf("builder output needs an independent consumer plan")
				}
			}
		}
	}
	return nil
}
