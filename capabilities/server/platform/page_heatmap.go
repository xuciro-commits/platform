package platform

import (
	"fmt"
)

func (d *PageDocument) checkHeatmap(s Section) error {
	if s.Widget != "heatmap" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Heatmap.RequiredUIProfile) || s.Measure != "count" || s.Group == "" || s.ColumnGroup == "" || s.Group == s.ColumnGroup || len(s.Fields) > 0 || len(s.Actions) > 0 {
		return fmt.Errorf("heatmap needs two original axes, complete count and exclusive scoped filter types")
	}

	return nil
}

func (s Section) CheckHeatmap(info EntityInfo) error {
	if s.Widget != "heatmap" {
		return nil
	}
	if s.Measure != "count" || s.Group == s.ColumnGroup || s.Group == "count" || s.ColumnGroup == "count" {
		return fmt.Errorf("heatmap needs complete count and distinct original fields")
	}
	return s.checkWidgetFieldRoles(info)
}
