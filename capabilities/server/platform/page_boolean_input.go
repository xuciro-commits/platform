package platform

import "fmt"

func (d *PageDocument) checkBooleanInput(s Section) error {
	if s.Widget != "boolean-input" {
		if s.BooleanVariable != "" || s.BooleanLabel != nil {
			return fmt.Errorf("boolean input bindings need their widget")
		}
		return nil
	}
	v := d.Variables[s.BooleanVariable]
	if s.BooleanVariable == "" || v.Type != "boolean" || v.Mode != "state" || v.Scope != "page" && v.Scope != "overlay" || s.BooleanLabel != nil && len(*s.BooleanLabel) > 1024 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("boolean input needs an original page or overlay boolean state and bounded label")
	}
	return nil
}
