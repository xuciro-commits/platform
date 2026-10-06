package platform

import "fmt"

// Detail layout does not change fields, reads or record actions.
type PageDetailPresentation struct {
	Columns  int  `json:"columns"`
	HideNull bool `json:"hideNull,omitempty"`
	// Barcode names a text field also drawn as a Code 128 barcode, and Print
	// offers the detail as a printable label (ADR-0057 D1).
	Barcode string `json:"barcode,omitempty"`
	Print   bool   `json:"print,omitempty"`
}

func (d *PageDocument) checkDetailPresentation(s Section) error {
	if s.DetailPresentation == nil {
		return nil
	}
	limits := pageWidgets.Runtime.DetailPresentation
	if s.Widget != "detail" || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || s.DetailPresentation.Columns < 1 || s.DetailPresentation.Columns > limits.MaxColumns {
		return fmt.Errorf("detail presentation needs its supported profile and bounded columns")
	}
	if (s.DetailPresentation.Barcode != "" || s.DetailPresentation.Print) && !PageUIProfileSupports(d.UIProfile, limits.BarcodeRequiredUIProfile) {
		return fmt.Errorf("barcode and print presentation need their profile")
	}
	return nil
}

// CheckDetailBarcode refuses a barcode over a field the record has not, or
// one that is not text: a barcode encodes a code, not a number or a date.
func (s Section) CheckDetailBarcode(info EntityInfo) error {
	if s.Widget != "detail" || s.DetailPresentation == nil || s.DetailPresentation.Barcode == "" {
		return nil
	}
	if s.DetailPresentation.Barcode != "id" {
		if f, ok := info.Field(s.DetailPresentation.Barcode); !ok || f.Type != "text" {
			return fmt.Errorf("barcode needs an original text field")
		}
	}
	return nil
}
