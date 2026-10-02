package platform

import (
	"fmt"
	"slices"
)

type PageRecordView struct {
	Tabs []string `json:"tabs"`
}

func (d *PageDocument) checkRecordView(s Section) error {
	if s.Widget != "record-view" {
		if s.RecordView != nil {
			return fmt.Errorf("record view configuration needs its widget")
		}
		return nil
	}
	limits := pageWidgets.Runtime.RecordView
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) {
		return fmt.Errorf("record view needs its supported profile")
	}
	if s.RecordView == nil {
		return nil
	}
	if len(s.RecordView.Tabs) == 0 || len(s.RecordView.Tabs) > len(limits.Tabs) {
		return fmt.Errorf("record view needs bounded tabs")
	}
	seen := map[string]bool{}
	for _, tab := range s.RecordView.Tabs {
		if seen[tab] || !slices.Contains(limits.Tabs, tab) {
			return fmt.Errorf("record view has an invalid tab")
		}
		seen[tab] = true
	}
	return nil
}
