package platform

import (
	"fmt"
)

type PageSparkline struct {
	Field  string  `json:"field,omitempty"`
	Label  *string `json:"label,omitempty"`
	Suffix string  `json:"suffix,omitempty"`
}

func (d *PageDocument) checkSparkline(s Section) error {
	if s.Widget != "sparkline-kpi" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	c := s.Sparkline
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Sparkline.RequiredUIProfile) || c == nil || c.Label != nil && len(*c.Label) > 1024 || len(c.Suffix) > 64 {
		return fmt.Errorf("sparkline needs one original scalar and bounded presentation")
	}
	id := s.SparklineDecimalVariable
	if s.SparklineNumberVariable != "" {
		id = s.SparklineNumberVariable
	}
	v := d.Variables[id]
	if s.CollectionVariable == "" {
		if c.Field != "" {
			return fmt.Errorf("sparkline field needs its original series window")
		}
		return nil
	}
	set := d.Variables[s.CollectionVariable]
	var q PageQuery
	if set.Source != nil {
		q = d.Queries[set.Source.Query]
	}
	if c.Field == "" || set.Mode != "resource" || set.Source == nil || set.Source.Kind != "plan" || set.Scope != v.Scope || set.Owner != v.Owner || len(q.Sort) == 0 || q.Limit < 1 || q.Limit > pageWidgets.Runtime.Sparkline.MaxPoints {
		return fmt.Errorf("sparkline series needs a same-owner ordered original 30-record window")
	}
	return nil
}
func (s Section) CheckSparkline(info EntityInfo) error {
	if s.Widget != "sparkline-kpi" {
		return nil
	}
	if s.Sparkline == nil {
		return fmt.Errorf("sparkline configuration is missing")
	}
	return s.checkWidgetFieldRoles(info)
}
