package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// The application API owns this build-time UI contract (ADR-0046). The same
// bytes generate the browser registry; they describe presentation, not grants
// or new business execution capabilities.
//
//go:embed pageui/widgets.json
var pageWidgetJSON []byte

type pageWidgetContract struct {
	ComponentID       string            `json:"componentID"`
	ConfigVersion     int               `json:"configVersion"`
	RequiredUIProfile string            `json:"requiredUIProfile"`
	SelectionMode     string            `json:"selectionMode"`
	PropsSchema       ValueSchema       `json:"propsSchema"`
	Defaults          map[string]any    `json:"defaults"`
	InputPorts        []pageWidgetPort  `json:"inputPorts"`
	OutputPorts       []pageWidgetPort  `json:"outputPorts"`
	Events            []pageWidgetEvent `json:"events"`
	LayoutPreferences struct {
		Frame string `json:"frame"`
	} `json:"layoutPreferences"`
	LifecyclePolicy *struct {
		StateOwner string   `json:"stateOwner"`
		ClearOn    []string `json:"clearOn"`
		Hidden     string   `json:"hidden"`
	} `json:"lifecyclePolicy"`
}

type pageWidgetPort struct {
	ID                string `json:"id"`
	BindingField      string `json:"bindingField"`
	Type              string `json:"type"`
	RequiredUIProfile string `json:"requiredUIProfile"`
	Writable          bool   `json:"writable"`
}
type pageWidgetEvent struct {
	ID          string `json:"id"`
	Payload     string `json:"payload"`
	Required    bool   `json:"required"`
	MaxBindings int    `json:"maxBindings"`
}

type pageUIContract struct {
	Layout struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		UnusedProfile     string `json:"unusedProfile"`
		MaxUnused         int    `json:"maxUnused"`
		MinSize           int    `json:"minSize"`
		MaxSize           int    `json:"maxSize"`
		MaxWeight         int    `json:"maxWeight"`
		MaxGap            int    `json:"maxGap"`
		StackBelow        int    `json:"stackBelow"`
	} `json:"layout"`
	UIProfile         string               `json:"uiProfile"`
	SupportedProfiles []string             `json:"supportedProfiles"`
	Widgets           []pageWidgetContract `json:"widgets"`
	Runtime           pageRuntimeContract  `json:"runtime"`
}

var pageWidgets = func() pageUIContract {
	var manifest pageUIContract
	if err := json.Unmarshal(pageWidgetJSON, &manifest); err != nil {
		panic(err)
	}
	seen := map[string]bool{}
	for _, widget := range manifest.Widgets {
		if widget.ComponentID == "" || seen[widget.ComponentID] || widget.ConfigVersion < 1 || !slices.Contains(manifest.SupportedProfiles, widget.RequiredUIProfile) {
			panic("invalid page widget contract identity")
		}
		seen[widget.ComponentID] = true
		fields := map[string]bool{}
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.ID == "" || fields[port.BindingField] || !slices.Contains([]string{"recordVariable", "collectionVariable", "selectionVariable", "filterVariable", "selectionSetVariable", "enabledWhen"}, port.BindingField) || !slices.Contains([]string{"record", "record-set", "object-set", "filter", "boolean"}, port.Type) || !slices.Contains(manifest.SupportedProfiles, port.RequiredUIProfile) {
				panic("invalid page widget port")
			}
			fields[port.BindingField] = true
		}
		for _, event := range widget.Events {
			if event.ID != "click" || event.Payload != "void" || event.MaxBindings != 1 {
				panic("unsupported page widget event")
			}
		}
		if widget.LayoutPreferences.Frame != "card" && widget.LayoutPreferences.Frame != "inline" {
			panic("invalid widget frame")
		}
		if policy := widget.LifecyclePolicy; policy != nil {
			session := policy.StateOwner == "page-session" && policy.Hidden == "retain" && slices.Equal(policy.ClearOn, []string{"scope-change", "binding-change", "close"})
			instance := policy.StateOwner == "widget-instance" && policy.Hidden == "unmount" && slices.Equal(policy.ClearOn, []string{"record", "member", "definition", "scope-close"})
			if !session && !instance {
				panic("unsupported widget lifecycle policy")
			}
		}
		if err := widget.PropsSchema.Check(); err != nil {
			panic(err)
		}
		defaults, _ := json.Marshal(widget.Defaults)
		if err := widget.PropsSchema.Validate(defaults, 1<<20); err != nil {
			panic(err)
		}
	}
	return manifest
}()

// PageUIManifest returns the exact descriptor used by api-types and Catalog.
func PageUIManifest() string { return string(pageWidgetJSON) }
func PageUIProfile() string  { return pageWidgets.UIProfile }
func WidgetWritesSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && w.SelectionMode == "write"
}
func WidgetReadsSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && (w.SelectionMode == "read" || w.SelectionMode == "write")
}

func SupportsPageUIProfile(profile string) bool {
	return slices.Contains(pageWidgets.SupportedProfiles, profile)
}

func pageWidget(id string) *pageWidgetContract {
	for i := range pageWidgets.Widgets {
		if pageWidgets.Widgets[i].ComponentID == id {
			return &pageWidgets.Widgets[i]
		}
	}
	return nil
}
func widgetPort(id, field string) *pageWidgetPort {
	w := pageWidget(id)
	if w == nil {
		return nil
	}
	for _, ports := range [][]pageWidgetPort{w.InputPorts, w.OutputPorts} {
		for i := range ports {
			if ports[i].BindingField == field {
				return &ports[i]
			}
		}
	}
	return nil
}
func widgetEvent(id, event string) *pageWidgetEvent {
	w := pageWidget(id)
	if w == nil {
		return nil
	}
	for i := range w.Events {
		if w.Events[i].ID == event {
			return &w.Events[i]
		}
	}
	return nil
}
func (d *PageDocument) checkWidgetPorts(s Section) error {
	if s.Mark != "" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.24") {
		return fmt.Errorf("chart mark requires UI profile v2.24")
	}
	if w := pageWidget(s.Widget); w != nil && !PageUIProfileSupports(d.UIProfile, w.RequiredUIProfile) {
		return fmt.Errorf("widget %s requires UI profile %s", s.Widget, w.RequiredUIProfile)
	}
	for field, id := range map[string]string{"recordVariable": s.RecordVariable, "collectionVariable": s.CollectionVariable, "selectionVariable": s.SelectionVariable, "filterVariable": s.FilterVariable, "selectionSetVariable": s.SelectionSetVariable} {
		if id == "" {
			continue
		}
		if field == "selectionSetVariable" {
			v := d.Variables[id]
			if v.Mode != "resource" || v.Source == nil || v.Source.Kind != "records" || v.Source.Section != s.ID {
				return fmt.Errorf("table selection set needs its own record-set resource")
			}
		}
		port := widgetPort(s.Widget, field)
		v, ok := d.Variables[id]
		if port == nil || !ok || !PageUIProfileSupports(d.UIProfile, port.RequiredUIProfile) || v.Type != port.Type || port.Writable && field != "selectionSetVariable" && !v.Writable {
			return fmt.Errorf("widget %s has an invalid %s port", s.Widget, field)
		}
	}
	return nil
}

func PageUIProfileSupports(profile, required string) bool {
	current, minimum := slices.Index(pageWidgets.SupportedProfiles, profile), slices.Index(pageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

// Widgets remains the original public discovery list, derived from the one
// descriptor rather than a separately maintained enumeration.
var Widgets = func() []string {
	ids := make([]string, 0, len(pageWidgets.Widgets))
	for _, widget := range pageWidgets.Widgets {
		ids = append(ids, widget.ComponentID)
	}
	return ids
}()

func checkPageWidget(section Section) error {
	for _, widget := range pageWidgets.Widgets {
		if widget.ComponentID != section.Widget {
			continue
		}
		if section.ConfigVersion != widget.ConfigVersion {
			return fmt.Errorf("widget %s config version %d is unsupported", section.Widget, section.ConfigVersion)
		}
		// Only presentation properties are validated here. Asset references,
		// selections, queries and actions retain their original host owners.
		values := map[string]string{"title": section.Title, "width": section.Width, "text": section.Text, "group": section.Group, "mark": section.Mark, "columnGroup": section.ColumnGroup, "measure": section.Measure, "timeStart": section.TimeStart, "timeEnd": section.TimeEnd, "timeLabel": section.TimeLabel, "timeGroup": section.TimeGroup, "cardLabel": section.CardLabel}
		props := map[string]string{}
		for key, value := range values {
			if value != "" {
				if _, declared := widget.PropsSchema.Properties[key]; !declared {
					return fmt.Errorf("widget %s does not declare property %s", section.Widget, key)
				}
				props[key] = value
			}
		}
		raw, _ := json.Marshal(props)
		return widget.PropsSchema.Validate(raw, 1<<20)
	}
	return fmt.Errorf("widget %q is unavailable", section.Widget)
}
