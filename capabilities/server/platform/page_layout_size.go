package platform

import "fmt"

func (d *PageDocument) checkLayoutSize(id string, node PageLayoutNode, parent string, parentHeight bool) (bool, error) {
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
		if *s.Weight < 1 || *s.Weight > limits.MaxWeight || parent != "rows" && parent != "columns" {
			return fail("weight needs a Rows or Columns parent and a bounded positive value")
		}
		if parent == "columns" && s.Width != nil || parent == "rows" && s.Height != nil {
			return fail("weight conflicts with a fixed main-axis dimension")
		}
		if parent == "rows" {
			if !parentHeight {
				return fail("row weight needs a parent with a definite height")
			}
			bounded = true
		}
	}
	if s.Scroll != "" && s.Scroll != "visible" && s.Scroll != "auto" {
		return fail("scroll must be visible or auto")
	}
	if s.Scroll == "auto" && !bounded && s.MaxHeight == nil {
		return fail("scroll needs a definite or maximum height")
	}
	return bounded, nil
}
