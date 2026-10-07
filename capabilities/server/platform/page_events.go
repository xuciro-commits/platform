package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Overlays own disjoint presentation roots. Their state is page-local; they do
// not introduce an execution path or a second business record model.
type PageOverlayPresentation struct {
	Side            string `json:"side"`
	Size            string `json:"size"`
	CustomWidth     *int   `json:"customWidth,omitempty"`
	Backdrop        bool   `json:"backdrop"`
	CloseOnBackdrop bool   `json:"closeOnBackdrop"`
	CloseOnEsc      bool   `json:"closeOnEsc"`
}
type PageOverlay struct {
	Presentation *PageOverlayPresentation `json:"presentation,omitempty"`
	Root         string                   `json:"root"`
	Kind         string                   `json:"kind"`
	Title        string                   `json:"title"`
	OpenVariable string                   `json:"openVariable"`
}

// A presentation event runs an ordered chain of effects (ADR-0053 §11). Each
// effect is exactly one of: a typed scalar state write, an ontology action
// taken on a record variable (or a creating action), a page navigation, or a
// return to the caller. Navigation and return end the chain.
type PageEventBinding struct {
	Control string       `json:"control,omitempty"`
	Source  string       `json:"source"`
	Event   string       `json:"event"`
	Effects []PageEffect `json:"effects"`
}

type PageEffect struct {
	Kind   string          `json:"kind"`
	Target string          `json:"target,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	// From names the variable a set effect copies instead of a literal Value
	// (ADR-0053 §7 assignment; v2.108): same type, readable where the event is.
	From     string            `json:"from,omitempty"`
	Action   *PageActionEffect `json:"action,omitempty"`
	Navigate *PageNavigation   `json:"navigate,omitempty"`
}

type PageActionEffect struct {
	Ref            AssetRef `json:"ref"`
	RecordVariable string   `json:"recordVariable,omitempty"`
	// Inputs prefill the action's parameters from page variables or literals
	// (ADR-0053 §7; v2.108). The form still opens when the action has one; a
	// member may change what was prefilled before it commits.
	Inputs map[string]PageValue `json:"inputs,omitempty"`
}

const maxPageEffects = 8

// Navigation returns the chain's navigation, when it has one.
func (b PageEventBinding) Navigation() *PageNavigation {
	for _, effect := range b.Effects {
		if effect.Kind == "navigate" {
			return effect.Navigate
		}
	}
	return nil
}

// Returns reports whether the chain ends by returning to the caller.
func (b PageEventBinding) Returns() bool {
	return len(b.Effects) > 0 && b.Effects[len(b.Effects)-1].Kind == "return"
}

// Only reports whether the chain is exactly one effect of the kind.
func (b PageEventBinding) Only(kind string) bool {
	return len(b.Effects) == 1 && b.Effects[0].Kind == kind
}

// Variables lists the state the chain writes.
func (b PageEventBinding) Variables() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			ids = append(ids, effect.Target)
		case "navigate":
			if effect.Navigate != nil {
				for _, id := range effect.Navigate.Results {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

// Reads lists the variables the chain reads.
func (b PageEventBinding) Reads() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			if effect.From != "" {
				ids = append(ids, effect.From)
			}
		case "action":
			if effect.Action != nil && effect.Action.RecordVariable != "" {
				ids = append(ids, effect.Action.RecordVariable)
			}
			if effect.Action != nil {
				for _, arg := range effect.Action.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		case "navigate":
			if effect.Navigate != nil {
				for _, arg := range effect.Navigate.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		}
	}
	return ids
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
		if p := overlay.Presentation; p != nil {
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.91") || !slices.Contains([]string{"left", "right"}, p.Side) || !slices.Contains([]string{"small", "medium", "large", "custom"}, p.Size) || (p.Size == "custom") != (p.CustomWidth != nil) || p.CustomWidth != nil && (*p.CustomWidth < 240 || *p.CustomWidth > 1200) {
				return fmt.Errorf("overlay presentation needs supported dimensions and its renderer profile")
			}
		}
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
		group := false
		breadcrumb := false
		directory := false
		for _, section := range sections {
			if descriptor := widgetEvent(section.Widget, event.Event); section.ID == event.Source && descriptor != nil {
				found = descriptor.RequiredUIProfile == "" || PageUIProfileSupports(d.UIProfile, descriptor.RequiredUIProfile)
				group = section.Widget == "button-group"
				breadcrumb = section.Widget == "breadcrumb"
				directory = section.Widget == "asset-directory"
				navigation := event.Navigation()
				if group {
					found = found && slices.ContainsFunc(section.Buttons, func(b PageButton) bool { return b.ID == event.Control })
				} else if directory {
					found = found && event.Only("navigate") && navigation != nil && len(navigation.Inputs) == 0 && len(navigation.Results) == 0 && section.AssetDirectory != nil && slices.ContainsFunc(section.AssetDirectory.Items, func(i PageAssetDirectoryItem) bool {
						return i.ID == event.Control && i.Asset.Ref.Kind == AssetPage && i.Asset.Ref == navigation.Page
					})
				} else if breadcrumb {
					found = found && event.Control == "home" && event.Only("navigate") && navigation != nil && len(navigation.Inputs) == 0 && len(navigation.Results) == 0
				} else if event.Control != "" {
					found = false
				}
			}
		}
		key := event.Source + "/" + event.Control
		if !found || bound[key] || len(event.Effects) == 0 || len(event.Effects) > maxPageEffects {
			return fmt.Errorf("page event %s needs one button click or registered selection binding and one to %d effects", event.Source, maxPageEffects)
		}
		bound[key] = true
		if (len(event.Effects) > 1 || slices.ContainsFunc(event.Effects, func(e PageEffect) bool { return e.Kind == "action" })) && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.106") {
			return fmt.Errorf("page effect chains require UI profile v2.106")
		}
		for at, effect := range event.Effects {
			last := at == len(event.Effects)-1
			switch effect.Kind {
			case "set":
				if effect.Action != nil || effect.Navigate != nil {
					return fmt.Errorf("page effect %s is not one effect", event.Source)
				}
				variable := d.Variables[effect.Target]
				if event.Event == "select" && (!(variable.Mode == "state" && slices.Contains([]string{"page", "overlay"}, variable.Scope) || PageUIProfileSupports(d.UIProfile, "platform.page.v2.89") && variable.Mode == "shared" && variable.Scope == "application" && variable.Writable && d.rootPresentationSource(event.Source)) || !slices.Contains([]string{"string", "boolean"}, variable.Type) || open[effect.Target]) {
					return fmt.Errorf("selection event needs writable presentation scalar state without navigation or overlay control")
				}
				if effect.From != "" {
					source := d.Variables[effect.From]
					if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.108") || len(effect.Value) != 0 || effect.From == effect.Target || source.Type == "" || source.Type != variable.Type || !variable.IsWritable() {
						return fmt.Errorf("page event %s needs a readable source variable of the target's type", event.Source)
					}
				} else if !variable.IsWritable() || pageLiteralType(effect.Value) != variable.Type {
					return fmt.Errorf("page event %s needs a matching state value", event.Source)
				}
				for id, node := range d.Nodes {
					if node.Kind == "tabs" && node.ActiveVariable == effect.Target {
						var child string
						if json.Unmarshal(effect.Value, &child) != nil || !slices.Contains(d.ownedChildren(id), child) {
							return fmt.Errorf("page event %s tab value must name a child", event.Source)
						}
					}
				}
			case "action":
				a := effect.Action
				if event.Event == "select" || effect.Target != "" || len(effect.Value) != 0 || effect.Navigate != nil || a == nil || a.Ref.Check() != nil || a.Ref.Kind != AssetAction {
					return fmt.Errorf("page action effect %s needs one original action on a click", event.Source)
				}
				if v := d.Variables[a.RecordVariable]; a.RecordVariable != "" && v.Type != "record" {
					return fmt.Errorf("page action effect %s needs a record variable", event.Source)
				}
				if len(a.Inputs) > 0 && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.108") || len(a.Inputs) > pageWidgets.Runtime.Interface.MaxPorts {
					return fmt.Errorf("page action effect %s inputs need UI profile v2.108 and a bounded count", event.Source)
				}
				for parameter, arg := range a.Inputs {
					if !pageNodeID.MatchString(parameter) || (arg.Variable != "") == (len(arg.Literal) != 0) || arg.Variable != "" && d.Variables[arg.Variable].Type == "" || arg.Variable == "" && pageLiteralType(arg.Literal) == "" {
						return fmt.Errorf("page action effect %s input %s is invalid", event.Source, parameter)
					}
				}
			case "navigate", "return":
				if event.Event == "select" || group || !last {
					return fmt.Errorf("page event %s navigation must end a click chain", event.Source)
				}
				if err := d.checkNavigationEffect(effect); err != nil {
					return err
				}
			default:
				return fmt.Errorf("page effect %s has an unknown kind", event.Source)
			}
		}
	}
	for _, section := range sections {
		if section.Widget == "button-group" {
			for _, button := range section.Buttons {
				if !bound[section.ID+"/"+button.ID] {
					return fmt.Errorf("button group control needs a click binding")
				}
			}
		}
		if event := widgetEvent(section.Widget, "click"); event != nil && event.Required && section.Widget != "button-group" && section.Widget != "breadcrumb" && !bound[section.ID+"/"] {
			return fmt.Errorf("page button %s needs a click binding", section.ID)
		}
	}
	return nil
}

// Application presentation updates are owned by an actual non-loop page root.
// Overlay and item templates keep their existing local select-event boundary.
func (d *PageDocument) rootPresentationSource(section string) bool {
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		n := d.Nodes[id]
		if n.Kind == "loop" {
			return false
		}
		if n.Kind == "widget" && n.Section == section {
			return true
		}
		for _, child := range n.Children {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(d.Root)
}
