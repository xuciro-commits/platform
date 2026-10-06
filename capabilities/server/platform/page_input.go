package platform

import "fmt"

func (d *PageDocument) checkInputPresentation(s Section) error {
	if s.InputKind == "" {
		return nil
	}
	// A scan input is a search input read by a barcode scanner or camera
	// (ADR-0057 D1): the same text state drives the same query search.
	required := pageWidgets.Runtime.Input.SearchRequiredUIProfile
	if s.InputKind == "scan" {
		required = pageWidgets.Runtime.Input.ScanRequiredUIProfile
	}
	if s.Widget != "input" || s.InputKind != "search" && s.InputKind != "scan" || !PageUIProfileSupports(d.UIProfile, required) {
		return fmt.Errorf("search presentation needs its original input and profile")
	}
	id := ""
	for _, node := range d.Nodes {
		if node.Section == s.ID {
			id = node.ValueVariable
		}
	}
	v := d.Variables[id]
	if id == "" || v.Type != "string" || v.Mode != "state" || v.Scope != "page" && v.Scope != "overlay" {
		return fmt.Errorf("search input needs original scoped text state")
	}
	for _, q := range d.Queries {
		if q.Search != nil && q.Search.Variable == id && q.Owner == v.Owner && q.ItemOwner == "" {
			return nil
		}
	}
	return fmt.Errorf("search input needs a same-owner original query search")
}
