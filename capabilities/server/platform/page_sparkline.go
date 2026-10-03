package platform

import (
	"fmt"
	"slices"
)

type PageSparkline struct {
	Field  string  `json:"field,omitempty"`
	Label  *string `json:"label,omitempty"`
	Suffix string  `json:"suffix,omitempty"`
}

func (d *PageDocument) checkSparkline(s Section) error {
	if s.Widget != "sparkline-kpi" {
		if s.Sparkline != nil || s.SparklineDecimalVariable != "" || s.SparklineNumberVariable != "" {
			return fmt.Errorf("sparkline bindings need their widget")
		}
		return nil
	}
	c := s.Sparkline
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Sparkline.RequiredUIProfile) || c == nil || c.Label != nil && len(*c.Label) > 1024 || len(c.Suffix) > 64 || (s.SparklineDecimalVariable == "") == (s.SparklineNumberVariable == "") || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("sparkline needs one original scalar and bounded presentation")
	}
	id, typ := s.SparklineDecimalVariable, "decimal"
	if s.SparklineNumberVariable != "" {
		id, typ = s.SparklineNumberVariable, "number"
	}
	v := d.Variables[id]
	if v.Type != typ || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
		return fmt.Errorf("sparkline scalar needs its original type and owner")
	}
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
	if s.CollectionVariable == "" {
		return nil
	}
	f, ok := info.Field(s.Sparkline.Field)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
		return fmt.Errorf("sparkline series needs a visible original numeric field")
	}
	return nil
}
