package platform

import (
	"fmt"
	"slices"
	"strings"
)

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
