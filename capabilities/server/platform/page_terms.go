package platform

import (
	"fmt"
	"slices"
	"strings"
)

func (d *PageDocument) checkTerms(s Section) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" {
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Terms.RequiredUIProfile) || s.Group == "" || s.Group == "count" || strings.Contains(s.Group, ":") || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("term counts need original scoped plan and an unambiguous text group")
	}
	return nil
}
func (s Section) CheckTerms(info EntityInfo) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" {
		return nil
	}
	f, ok := info.Field(s.Group)
	if !ok || s.Group == "count" || strings.Contains(s.Group, ":") || !slices.Contains([]string{"text", "choice"}, f.Type) {
		return fmt.Errorf("term counts need an original visible text or choice field")
	}
	return nil
}
