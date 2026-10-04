package platform

import (
	"fmt"
	"slices"
)

// PageRecordScatter presents original records, never aggregate points.
type PageRecordScatter struct {
	XField     string `json:"xField"`
	YField     string `json:"yField"`
	ColorField string `json:"colorField"`
	LabelField string `json:"labelField"`
}

func (d *PageDocument) checkScatter(s Section) error {
	if s.Widget != "record-scatter" {
		if s.Scatter != nil {
			return fmt.Errorf("scatter configuration needs its widget")
		}
		return nil
	}
	if err := d.checkSharedRecordOutput(s); err != nil {
		return err
	}
	v := d.Variables[s.CollectionVariable]
	var q PageQuery
	if v.Source != nil {
		q = d.Queries[v.Source.Query]
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordScatter.RequiredUIProfile) || s.Scatter == nil || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(q.Sort) == 0 || q.Limit < 1 || q.Limit > pageWidgets.Runtime.RecordScatter.MaxPoints || len(s.Fields) > 0 || len(s.Actions) > 0 || s.RecordVariable != "" || s.ParentSelection != "" || s.Relation != "" {
		return fmt.Errorf("scatter needs its profile, fields and bounded ordered original plan window")
	}
	return nil
}

func (s Section) CheckScatter(info EntityInfo) error {
	if s.Widget != "record-scatter" {
		return nil
	}
	if s.Scatter == nil {
		return fmt.Errorf("scatter configuration is missing")
	}
	for _, name := range []string{s.Scatter.XField, s.Scatter.YField} {
		field, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"integer", "decimal"}, field.Type) {
			return fmt.Errorf("scatter coordinates need visible original numeric fields")
		}
	}
	color, ok := info.Field(s.Scatter.ColorField)
	if !ok || !slices.Contains([]string{"text", "choice"}, color.Type) {
		return fmt.Errorf("scatter color needs a visible text or choice field")
	}
	label, ok := info.Field(s.Scatter.LabelField)
	if s.Scatter.LabelField != "id" && (!ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, label.Type)) {
		return fmt.Errorf("scatter label needs a visible original title field or ID")
	}
	return nil
}
