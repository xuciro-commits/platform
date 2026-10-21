package platform

import (
	"fmt"
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
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if err := d.checkSharedRecordOutput(s); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordScatter.RequiredUIProfile) || s.Scatter == nil {
		return fmt.Errorf("scatter needs its profile, fields and bounded ordered original plan window")
	}
	return nil
}

func (s Section) CheckScatter(info EntityInfo) error {
	if s.Widget != "record-scatter" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}
