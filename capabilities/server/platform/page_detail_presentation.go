package platform

import "fmt"

// Detail layout does not change fields, reads or record actions.
type PageDetailPresentation struct {
	Columns  int  `json:"columns"`
	HideNull bool `json:"hideNull,omitempty"`
}

func (d *PageDocument) checkDetailPresentation(s Section) error {
	if s.DetailPresentation == nil {
		return nil
	}
	limits := pageWidgets.Runtime.DetailPresentation
	if s.Widget != "detail" || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || s.DetailPresentation.Columns < 1 || s.DetailPresentation.Columns > limits.MaxColumns {
		return fmt.Errorf("detail presentation needs its supported profile and bounded columns")
	}
	return nil
}
