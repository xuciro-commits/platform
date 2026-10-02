package platform

import (
	"fmt"
	"slices"
)

type PageRecordChart struct {
	Mark   string `json:"mark"`
	XField string `json:"xField"`
	YField string `json:"yField"`
}

func (d *PageDocument) checkRecordChart(s Section) error {
	if s.Widget != "record-chart" {
		if s.RecordChart != nil {
			return fmt.Errorf("record chart configuration needs its widget")
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordChart.RequiredUIProfile) || s.RecordChart == nil || !slices.Contains(pageWidgets.Runtime.RecordChart.Marks, s.RecordChart.Mark) || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(d.Queries[v.Source.Query].Sort) == 0 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("record chart needs its profile, axes and ordered original plan window")
	}
	return nil
}

func (s Section) CheckRecordChart(info EntityInfo) error {
	if s.Widget != "record-chart" {
		return nil
	}
	if s.RecordChart == nil {
		return fmt.Errorf("record chart configuration is missing")
	}
	x, xOK := info.Field(s.RecordChart.XField)
	y, yOK := info.Field(s.RecordChart.YField)
	if s.RecordChart.XField != "id" && (!xOK || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "boolean", "date", "datetime"}, x.Type)) || !yOK || !slices.Contains([]string{"integer", "decimal"}, y.Type) {
		return fmt.Errorf("record chart needs visible scalar labels and a numeric value field")
	}
	return nil
}
