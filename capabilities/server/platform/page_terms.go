package platform

import (
	"fmt"
	"strings"
)

func (d *PageDocument) checkTerms(s Section) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Terms.RequiredUIProfile) || s.Group == "" || s.Group == "count" || strings.Contains(s.Group, ":") || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("term counts need original scoped plan and an unambiguous text group")
	}
	return nil
}
func (s Section) CheckTerms(info EntityInfo) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	if s.Group == "count" || strings.Contains(s.Group, ":") {
		return fmt.Errorf("term counts need an unambiguous original group")
	}
	return s.checkWidgetFieldRoles(info)
}
