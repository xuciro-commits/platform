package platform

import (
	"fmt"
)

func (d *PageDocument) checkSummary(s Section) error {
	if s.Widget != "summary-stats" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	set, stats := d.Variables[s.CollectionVariable], d.Variables[s.StatisticsVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Summary.RequiredUIProfile) || s.SummaryField == "" || stats.Source.Query != set.Source.Query || stats.Source.Measure != s.SummaryField {
		return fmt.Errorf("summary needs matching original statistics and query inputs")
	}
	return nil
}
func (s Section) CheckSummary(info EntityInfo) error {
	if s.Widget != "summary-stats" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}
