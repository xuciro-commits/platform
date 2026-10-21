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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordChart.RequiredUIProfile) || s.RecordChart == nil || !slices.Contains(pageWidgets.Runtime.RecordChart.Marks, s.RecordChart.Mark) {
		return fmt.Errorf("record chart needs its profile, axes and ordered original plan window")
	}
	return nil
}

func (s Section) CheckRecordChart(info EntityInfo) error {
	if s.Widget != "record-chart" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}
