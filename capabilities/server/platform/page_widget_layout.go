package platform

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// page_spacer.go

type PageSpacer struct {
	Size *float64 `json:"size,omitempty"`
}

func (d *PageDocument) checkSpacer(s Section) error {
	if s.Widget != "spacer" {
		return nil
	}
	c := s.Spacer
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Spacer.RequiredUIProfile) || c == nil || c.Size == nil || math.IsNaN(*c.Size) || math.IsInf(*c.Size, 0) || *c.Size < 0 || *c.Size > float64(pageWidgets.Layout.MaxSize) {
		return fmt.Errorf("spacer needs its profile and finite nonnegative size within the layout budget without business bindings")
	}
	return nil
}

// page_separator.go

type PageSeparator struct {
	Label *string `json:"label,omitempty"`
}

func (d *PageDocument) checkSeparator(s Section) error {
	if s.Widget != "separator" {
		return nil
	}
	c := s.Separator
	limits := pageWidgets.Runtime.Separator
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.Label != nil && len(*c.Label) > limits.MaxLabelBytes {
		return fmt.Errorf("separator needs its profile and bounded plain label without business bindings")
	}
	return nil
}

// page_button_group.go

type PageButton struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Variant string `json:"variant,omitempty"`
	Icon    string `json:"icon,omitempty"`
}

func (d *PageDocument) checkButtonGroup(s Section) error {
	if s.Widget != "button-group" {
		return nil
	}
	limits := pageWidgets.Runtime.ButtonGroup
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.Buttons) == 0 || len(s.Buttons) > limits.MaxButtons {
		return fmt.Errorf("button group needs its profile and bounded controls")
	}
	seen := map[string]bool{}
	for _, b := range s.Buttons {
		if !pageNodeID.MatchString(b.ID) || seen[b.ID] || b.Title == "" || len(b.Title) > limits.MaxTitleBytes || b.Variant != "" && !slices.Contains(limits.Variants, b.Variant) || b.Icon != "" && !slices.Contains(limits.Icons, b.Icon) {
			return fmt.Errorf("button needs a stable identity, title and supported presentation")
		}
		seen[b.ID] = true
	}
	return nil
}

// page_layout_size.go

func (d *PageDocument) checkLayoutSize(id string, node PageLayoutNode, parent string, parentHeight bool, dormant bool) (bool, error) {
	fail := func(reason string) (bool, error) { return false, fmt.Errorf("page node %s layout: %s", id, reason) }
	limits := pageWidgets.Layout
	if (node.Size != nil || node.Gap != nil) && !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) {
		return fail("sizes require " + limits.RequiredUIProfile)
	}
	if node.Gap != nil && (node.Kind != "rows" && node.Kind != "columns" || *node.Gap < 0 || *node.Gap > limits.MaxGap) {
		return fail("gap needs a bounded Rows or Columns container")
	}
	s := node.Size
	bounded := parentHeight && parent == "columns"
	if s == nil {
		return bounded, nil
	}
	for _, value := range []*int{s.Width, s.Height, s.MinWidth, s.MaxWidth, s.MinHeight, s.MaxHeight} {
		if value != nil && (*value < limits.MinSize || *value > limits.MaxSize) {
			return fail("dimension is outside the size budget")
		}
	}
	for _, axis := range [][3]*int{{s.Width, s.MinWidth, s.MaxWidth}, {s.Height, s.MinHeight, s.MaxHeight}} {
		fixed, min, max := axis[0], axis[1], axis[2]
		if min != nil && max != nil && *min > *max || fixed != nil && (min != nil && *fixed < *min || max != nil && *fixed > *max) {
			return fail("fixed, minimum and maximum dimensions disagree")
		}
	}
	bounded = s.Height != nil || bounded
	if s.Weight != nil {
		if *s.Weight < 1 || *s.Weight > limits.MaxWeight || !dormant && parent != "rows" && parent != "columns" {
			return fail("weight needs a Rows or Columns parent and a bounded positive value")
		}
		if !dormant && (parent == "columns" && s.Width != nil || parent == "rows" && s.Height != nil) {
			return fail("weight conflicts with a fixed main-axis dimension")
		}
		if parent == "rows" {
			if !parentHeight && !dormant {
				return fail("row weight needs a parent with a definite height")
			}
			bounded = true
		}
	}
	if s.Scroll != "" && s.Scroll != "visible" && s.Scroll != "auto" {
		return fail("scroll must be visible or auto")
	}
	if s.Scroll == "auto" && !dormant && !bounded && s.MaxHeight == nil {
		return fail("scroll needs a definite or maximum height")
	}
	return bounded, nil
}

// page_titles.go

func (d *PageDocument) checkTitles(s Section) error {
	if s.Widget == "heading" {
		c := pageWidgets.Runtime.Titles
		if !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || !slices.Contains(c.HeadingLevels, s.HeadingLevel) || strings.TrimSpace(s.Text) == "" || len(s.Text) > c.MaxTextBytes {
			return fmt.Errorf("heading needs its profile, plain text and supported level")
		}
	}
	if s.Widget != "collection-title" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	set, count := d.Variables[s.CollectionVariable], d.Variables[s.CountVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Titles.RequiredUIProfile) || count.Source.Query != set.Source.Query || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Measure != "" {
		return fmt.Errorf("collection title needs a matching scoped original count and query collection")
	}
	return nil
}

// page_slots.go

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
