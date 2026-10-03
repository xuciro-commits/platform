package platform

import (
	"fmt"
	"slices"
)

func (d *PageDocument) checkSummary(s Section) error {
	if s.Widget != "summary-stats" {
		if s.SummaryField != "" || s.StatisticsVariable != "" {
			return fmt.Errorf("summary bindings need their widget")
		}
		return nil
	}
	set, stats := d.Variables[s.CollectionVariable], d.Variables[s.StatisticsVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Summary.RequiredUIProfile) || s.SummaryField == "" || s.StatisticsVariable == "" || set.Mode != "resource" || set.Source == nil || set.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, set.Scope) || stats.Type != "statistics" || stats.Mode != "aggregate" || stats.Source == nil || stats.Source.Kind != "statistics" || stats.Source.Query != set.Source.Query || stats.Source.Measure != s.SummaryField || stats.Scope != set.Scope || stats.Owner != set.Owner || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("summary needs matching original statistics and query inputs")
	}
	return nil
}
func (s Section) CheckSummary(info EntityInfo) error {
	if s.Widget != "summary-stats" {
		return nil
	}
	f, ok := info.Field(s.SummaryField)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
		return fmt.Errorf("summary needs an original visible numeric field")
	}
	return nil
}
