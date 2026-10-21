package platform

import (
	"fmt"
	"slices"
)

func (d *PageDocument) checkBooleanInput(s Section) error {
	if s.Widget != "boolean-input" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if s.BooleanVariable == "" || s.BooleanLabel != nil && len(*s.BooleanLabel) > 1024 {
		return fmt.Errorf("boolean input needs an original page or overlay boolean state and bounded label")
	}
	if s.BooleanVariant != "" && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.BooleanInput.RequiredUIProfile) || !slices.Contains(pageWidgets.Runtime.BooleanInput.Variants, s.BooleanVariant)) {
		return fmt.Errorf("boolean presentation needs its supported variant profile")
	}
	return nil
}
