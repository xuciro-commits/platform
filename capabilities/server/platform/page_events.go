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
	Source   string          `json:"source"`
	Event    string          `json:"event"`
	Target   string          `json:"target"`
	Value    json.RawMessage `json:"value,omitempty"`
	Navigate *PageNavigation `json:"navigate,omitempty"`
	Return   bool            `json:"return,omitempty"`
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
			if descriptor := widgetEvent(section.Widget, event.Event); section.ID == event.Source && descriptor != nil {
				found = descriptor.RequiredUIProfile == "" || PageUIProfileSupports(d.UIProfile, descriptor.RequiredUIProfile)
			}
		}
		variable := d.Variables[event.Target]
		if !found || bound[event.Source] {
			return fmt.Errorf("page event %s needs one button click or registered selection binding and a matching state value", event.Source)
		}
		if event.Event == "select" && (event.Navigate != nil || event.Return || variable.Mode != "state" || !slices.Contains([]string{"page", "overlay"}, variable.Scope) || !slices.Contains([]string{"string", "boolean"}, variable.Type) || open[event.Target]) {
			return fmt.Errorf("selection event needs local scalar state without navigation or overlay control")
		}
		if event.Navigate != nil || event.Return {
			if err := d.checkNavigationEvent(event); err != nil {
				return err
			}
			bound[event.Source] = true
			continue
		}
		if !variable.IsWritable() || pageLiteralType(event.Value) != variable.Type {
			return fmt.Errorf("page event %s needs a matching state value", event.Source)
		}
		for id, node := range d.Nodes {
			if node.Kind == "tabs" && node.ActiveVariable == event.Target {
				var child string
				if json.Unmarshal(event.Value, &child) != nil || !slices.Contains(d.ownedChildren(id), child) {
					return fmt.Errorf("page event %s tab value must name a child", event.Source)
				}
			}
		}
		bound[event.Source] = true
	}
	for _, section := range sections {
		if event := widgetEvent(section.Widget, "click"); event != nil && event.Required && !bound[section.ID] {
			return fmt.Errorf("page button %s needs a click binding", section.ID)
		}
	}
	return nil
}
