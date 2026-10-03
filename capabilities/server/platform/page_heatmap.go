package platform

import (
	"fmt"
	"slices"
)

func (d *PageDocument) checkHeatmap(s Section) error {
	ids := []string{s.RowValueVariable, s.RowSetVariable, s.ColumnValueVariable, s.ColumnSetVariable}
	if s.Widget != "heatmap" {
		for _, id := range ids {
			if id != "" {
				return fmt.Errorf("heatmap filter bindings need their widget")
			}
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Heatmap.RequiredUIProfile) || s.Measure != "count" || s.Group == "" || s.ColumnGroup == "" || s.Group == s.ColumnGroup || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.RowValueVariable != "" && s.RowSetVariable != "" || s.ColumnValueVariable != "" && s.ColumnSetVariable != "" || len(s.Fields) > 0 || len(s.Actions) > 0 {
		return fmt.Errorf("heatmap needs two original axes, complete count and exclusive scoped filter types")
	}
	seen := map[string]bool{}
	for index, id := range ids {
		if id == "" {
			continue
		}
		state := d.Variables[id]
		typ := "string"
		if index%2 == 1 {
			typ = "string-set"
		}
		if seen[id] || state.Mode != "state" || state.Type != typ || state.Scope != v.Scope || state.Owner != v.Owner {
			return fmt.Errorf("heatmap outputs need distinct original state variables in its collection owner")
		}
		seen[id] = true
	}
	return nil
}

func (s Section) CheckHeatmap(info EntityInfo) error {
	if s.Widget != "heatmap" {
		return nil
	}
	if s.Measure != "count" || s.Group == s.ColumnGroup {
		return fmt.Errorf("heatmap needs complete count and distinct fields")
	}
	for _, name := range []string{s.Group, s.ColumnGroup} {
		f, ok := info.Field(name)
		if !ok || name == "count" || !slices.Contains([]string{"text", "choice"}, f.Type) {
			return fmt.Errorf("heatmap axes need visible original text or choice fields")
		}
	}
	return nil
}
