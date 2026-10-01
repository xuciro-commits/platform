package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Overlays own disjoint presentation roots. Their state is page-local; they do
// not introduce an execution path or a second business record model.
type PageOverlay struct {
	Root         string `json:"root"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	OpenVariable string `json:"openVariable"`
}

// A finite presentation event can only write a typed scalar state literal.
type PageEventBinding struct {
	Source string          `json:"source"`
	Event  string          `json:"event"`
	Target string          `json:"target"`
	Value  json.RawMessage `json:"value"`
}

func (d *PageDocument) checkEvents(sections []Section) error {
	if len(d.Overlays) > 16 || len(d.Events) > 256 {
		return fmt.Errorf("page overlay or event limit exceeded")
	}
	if (len(d.Overlays) > 0 || len(d.Events) > 0) && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.4") {
		return fmt.Errorf("page overlays and events require UI profile v2.4")
	}
	open := map[string]bool{}
	for id, overlay := range d.Overlays {
		variable := d.Variables[overlay.OpenVariable]
		var initial bool
		if !pageNodeID.MatchString(id) || !pageNodeID.MatchString(overlay.Root) || overlay.Title == "" || len(overlay.Title) > 1024 ||
			(overlay.Kind != "modal" && overlay.Kind != "drawer") || variable.Mode != "state" || variable.Type != "boolean" ||
			json.Unmarshal(variable.Initial, &initial) != nil || initial || open[overlay.OpenVariable] {
			return fmt.Errorf("page overlay %s needs a separate root, title, supported kind and unique boolean state initialized false", id)
		}
		open[overlay.OpenVariable] = true
	}
	bound := map[string]bool{}
	for _, event := range d.Events {
		found := false
		for _, section := range sections {
			if section.ID == event.Source && section.Widget == "button" {
				found = true
			}
		}
		variable := d.Variables[event.Target]
		if !found || event.Event != "click" || bound[event.Source] || variable.Mode != "state" || pageLiteralType(event.Value) != variable.Type {
			return fmt.Errorf("page event %s needs one button click and a matching state value", event.Source)
		}
		for _, node := range d.Nodes {
			if node.Kind == "tabs" && node.ActiveVariable == event.Target {
				var child string
				if json.Unmarshal(event.Value, &child) != nil || !slices.Contains(node.Children, child) {
					return fmt.Errorf("page event %s tab value must name a child", event.Source)
				}
			}
		}
		bound[event.Source] = true
	}
	for _, section := range sections {
		if section.Widget == "button" && !bound[section.ID] {
			return fmt.Errorf("page button %s needs a click binding", section.ID)
		}
	}
	return nil
}
