package platform

import (
	"fmt"
	"slices"
)

// A slot is an ordinary owned layout edge, not another component store.
func (d *PageDocument) checkWidgetSlots(id string, node PageLayoutNode, sections []Section) error {
	var widget string
	for _, s := range sections {
		if s.ID == node.Section {
			widget = s.Widget
			break
		}
	}
	used := map[string]bool{}
	for _, child := range node.Children {
		root, ok := d.Nodes[child]
		slot := widgetSlot(widget, root.Slot)
		if !ok || slot == nil || used[root.Slot] || !PageUIProfileSupports(d.UIProfile, slot.RequiredUIProfile) || !slices.Contains(slot.AllowedLayouts, root.Kind) {
			return fmt.Errorf("page widget %s needs unique declared slot roots and their UI profile", id)
		}
		used[root.Slot] = true
	}
	return nil
}

func (d *PageDocument) checkSlotParent(id string, node PageLayoutNode) error {
	if node.Slot == "" {
		return nil
	}
	for _, parent := range d.Nodes {
		if parent.Kind == "widget" && slices.Contains(parent.Children, id) {
			return nil
		}
	}
	return fmt.Errorf("page slot %s needs its widget parent", id)
}
