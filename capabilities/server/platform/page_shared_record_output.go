package platform

import "fmt"

// Shared producers keep application ownership; the reference is not an access grant.
func (d *PageDocument) checkSharedRecordOutput(s Section) error {
	if s.SelectionVariable == "" {
		return nil
	}
	if s.Widget != "record-scatter" && s.Widget != "record-leaderboard" && s.Widget != "record-list" && s.Widget != "record-map" {
		return nil
	}
	v := d.Variables[s.SelectionVariable]
	if s.Selection != "" || s.Widget == "record-list" && s.RecordList != nil && s.RecordList.Layout == "tiles" || v.Type != "record" || v.Scope != "application" || v.Mode != "shared" || !v.Writable || v.Source == nil || v.Source.Kind != "application" || v.Source.Object == nil {
		return fmt.Errorf("shared record output needs its writable original application record")
	}
	overlays, loops := d.overlayOwners(), d.loopOwners()
	for id, n := range d.Nodes {
		if n.Section == s.ID && (overlays[id] != "" || loops[id] != "") {
			return fmt.Errorf("shared record output needs a main-page producer")
		}
	}
	return nil
}
