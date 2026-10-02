package platform

import (
	"fmt"
	"slices"
)

func (d *PageDocument) checkFacets(s Section) error {
	if len(s.Facets) == 0 && s.FilterSearchVariable == "" {
		return nil
	}
	if s.Widget != "filter" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.30") || len(s.Facets) > 16 || s.FilterVariable != "" || s.CollectionVariable == "" {
		return fmt.Errorf("facets require v2.30 filter and an exclusive source plan")
	}
	owners := d.overlayOwners()
	owner := ""
	for id, n := range d.Nodes {
		if n.Section == s.ID {
			owner = owners[id]
		}
	}
	check := func(id, typ string) bool {
		v, ok := d.Variables[id]
		return ok && v.Type == typ && v.Mode == "state" && (v.Scope == "page" || v.Scope == "overlay" && v.Owner == owner && owner != "")
	}
	seen := map[string]bool{}
	for _, f := range s.Facets {
		typ := "string-set"
		if f.Kind == "search" || f.Kind == "number" {
			typ = "string"
		}
		if !pageNodeID.MatchString(f.Field) || seen[f.Field] || !slices.Contains([]string{"checkbox", "histogram", "search", "number"}, f.Kind) || !check(f.Variable, typ) {
			return fmt.Errorf("facet needs a unique field and compatible state binding")
		}
		seen[f.Field] = true
	}
	if s.FilterSearchVariable != "" && !check(s.FilterSearchVariable, "string") {
		return fmt.Errorf("facet search needs text state")
	}
	return nil
}

func (s Section) CheckFacetSchema(info EntityInfo) error {
	for _, facet := range s.Facets {
		f, ok := info.Field(facet.Field)
		if !ok || facet.Kind == "number" && !slices.Contains([]string{"integer", "decimal"}, f.Type) || facet.Kind != "number" && !slices.Contains([]string{"text", "choice"}, f.Type) {
			return fmt.Errorf("facet field type is unavailable")
		}
	}
	return nil
}
