package platform

import (
	"fmt"
	"slices"
	"strings"
)

func (d *PageDocument) checkTitles(s Section) error {
	if s.HeadingLevel != "" && s.Widget != "heading" {
		return fmt.Errorf("heading level needs its widget")
	}
	if s.Widget == "heading" {
		c := pageWidgets.Runtime.Titles
		if !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || !slices.Contains(c.HeadingLevels, s.HeadingLevel) || strings.TrimSpace(s.Text) == "" || len(s.Text) > c.MaxTextBytes {
			return fmt.Errorf("heading needs its profile, plain text and supported level")
		}
	}
	if s.Widget != "collection-title" {
		return nil
	}
	set, count := d.Variables[s.CollectionVariable], d.Variables[s.CountVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Titles.RequiredUIProfile) || s.CollectionVariable == "" || s.CountVariable == "" || set.Mode != "resource" || set.Source == nil || set.Source.Kind != "plan" || count.Mode != "aggregate" || count.Source == nil || count.Source.Kind != "count" || count.Source.Query != set.Source.Query || count.Scope != set.Scope || count.Owner != set.Owner || !slices.Contains([]string{"page", "overlay"}, set.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Measure != "" {
		return fmt.Errorf("collection title needs a matching scoped original count and query collection")
	}
	return nil
}
