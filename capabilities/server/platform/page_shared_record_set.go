package platform

import "fmt"

func (d *PageDocument) checkSharedRecordSetOutput(s Section) error {
	v := d.Variables[s.SelectionSetVariable]
	if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordSelection.SharedUIProfile) || v.Type != "record-set" || v.Scope != "application" || v.Mode != "shared" || !v.Writable || v.Source == nil || v.Source.Kind != "application" || v.Source.Object == nil {
		return fmt.Errorf("shared record-set output needs a writable original application resource")
	}
	overlays, loops := d.overlayOwners(), d.loopOwners()
	for id, n := range d.Nodes {
		if n.Section == s.ID && (overlays[id] != "" || loops[id] != "") {
			return fmt.Errorf("shared record-set output needs a main-page table")
		}
	}
	return nil
}
