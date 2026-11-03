package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed page_compat.json
var pageWidgetJSON []byte

func PageContractJSON() []byte {
	return pageWidgetJSON
}

type pageModelRole struct {
	Kinds    []string `json:"kinds"`
	Required bool     `json:"required,omitempty"`
	Target   string   `json:"target,omitempty"`
}

type pageModelPort struct {
	ID       string                   `json:"id"`
	Required bool                     `json:"required,omitempty"`
	Roles    map[string]pageModelRole `json:"roles"`
}

type pageWidgetContract struct {
	Resources           []PageResourceBinding `json:"resources,omitempty"`
	FieldRoles          []pageWidgetFieldRole `json:"fieldRoles,omitempty"`
	PortGroups          []pageWidgetPortGroup `json:"portGroups,omitempty"`
	ModelPorts          []pageModelPort       `json:"modelPorts,omitempty"`
	ConfigurationFields []string              `json:"configurationFields,omitempty"`
	ComponentID         string                `json:"componentID"`
	ConfigVersion       int                   `json:"configVersion"`
	RequiredUIProfile   string                `json:"requiredUIProfile"`
	SelectionMode       string                `json:"selectionMode"`
	BindingKinds        []string              `json:"bindingKinds"`
	InputPorts          []pageWidgetPort      `json:"inputPorts"`
	OutputPorts         []pageWidgetPort      `json:"outputPorts"`
	Events              []pageWidgetEvent     `json:"events"`
	Slots               []pageWidgetSlot      `json:"slots"`
}

type pageWidgetSlot struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Placement         string   `json:"placement"`
	RequiredUIProfile string   `json:"requiredUIProfile"`
	AllowedLayouts    []string `json:"allowedLayouts"`
}

type pageWidgetPort struct {
	Required          bool            `json:"required,omitempty"`
	Modes             []string        `json:"modes,omitempty"`
	Scopes            []string        `json:"scopes,omitempty"`
	Source            string          `json:"source,omitempty"`
	SameOwner         string          `json:"sameOwner,omitempty"`
	ScopeRule         string          `json:"scopeRule,omitempty"`
	Plan              *pageWidgetPlan `json:"plan,omitempty"`
	ID                string          `json:"id"`
	BindingField      string          `json:"bindingField"`
	Type              string          `json:"type"`
	RequiredUIProfile string          `json:"requiredUIProfile"`
	Writable          bool            `json:"writable"`
	StateWritable     bool            `json:"stateWritable,omitempty"`
}

type pageWidgetPlan struct {
	Ordered  bool `json:"ordered,omitempty"`
	MaxLimit int  `json:"maxLimit,omitempty"`
}

type pageWidgetPortGroup struct {
	Fields   []string `json:"fields"`
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	Distinct bool     `json:"distinct,omitempty"`
}

type pageWidgetFieldRole struct {
	Path     string   `json:"path"`
	Kinds    []string `json:"kinds"`
	Optional bool     `json:"optional,omitempty"`
	AllowID  bool     `json:"allowID,omitempty"`
	When     string   `json:"when,omitempty"`
	Distinct bool     `json:"distinct,omitempty"`
}

type pageWidgetEvent struct {
	RequiredUIProfile string `json:"requiredUIProfile,omitempty"`
	ID                string `json:"id"`
	Payload           string `json:"payload"`
	Required          bool   `json:"required"`
	MaxBindings       int    `json:"maxBindings"`
}

type pageUIContract struct {
	Documentless struct {
		Widgets []string `json:"widgets"`
		Fields  []string `json:"fields"`
	} `json:"documentless"`
	Features map[string]string `json:"features"`
	Layout   struct {
		PresentationProfile string `json:"presentationProfile"`
		MaxPadding          int    `json:"maxPadding"`
		RequiredUIProfile   string `json:"requiredUIProfile"`
		UnusedProfile       string `json:"unusedProfile"`
		MaxUnused           int    `json:"maxUnused"`
		MinSize             int    `json:"minSize"`
		MaxSize             int    `json:"maxSize"`
		MaxWeight           int    `json:"maxWeight"`
		MaxGap              int    `json:"maxGap"`
		StackBelow          int    `json:"stackBelow"`
	} `json:"layout"`
	UIProfile         string               `json:"uiProfile"`
	SupportedProfiles []string             `json:"supportedProfiles"`
	Widgets           []pageWidgetContract `json:"widgets"`
	Runtime           pageRuntimeContract  `json:"runtime"`
}

var pageWidgets = func() pageUIContract {
	var frozen struct {
		RetainedContract pageUIContract `json:"retainedContract"`
	}
	if err := json.Unmarshal(pageWidgetJSON, &frozen); err != nil {
		panic(err)
	}
	return frozen.RetainedContract
}()

func PageUIProfile() string {
	return pageWidgets.UIProfile
}

func WidgetReadsSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && (w.SelectionMode == "read" || w.SelectionMode == "write")
}

func SupportsPageUIProfile(profile string) bool {
	if profile == "platform.page.v3" {
		return true
	}
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

func (p Page) checkWidgetPorts(binding PageDataBinding, owner string) error {
	widget := binding.Ports["widget"]
	if pageWidget(widget) == nil {
		return nil
	}
	loop, overlay := p.scopeOwner(owner, "loop"), p.scopeOwner(owner, "overlay")
	for field, id := range binding.Ports {
		port := widgetPort(widget, field)
		if port != nil && port.ScopeRule != "" && field != "enabledWhen" {
			v, ok := p.InputSchema.Variables[id]
			reachable := ok && (v.Scope == "page" || v.Scope == "application" || v.Scope == "loop-item" && loop != "" && v.Owner == loop || v.Scope == "overlay" && overlay != "" && v.Owner == overlay)
			if !reachable || port.ScopeRule == "local" && loop != "" || port.ScopeRule == "exact" && (loop != "" || v.Scope == "page" && overlay != "" || v.Scope == "overlay" && v.Owner != overlay) {
				return fmt.Errorf("widget %s %s escapes its presentation owner", widget, field)
			}
		}
		if port == nil {
			if pageVariablePortFields[field] {
				return fmt.Errorf("widget %s has an invalid %s port", widget, field)
			}
			continue
		}
		v, ok := p.InputSchema.Variables[id]
		if ok && v.Scope == "application" && v.Type == "record" && !PageUIProfileSupports(p.InputSchema.profileVersion(), sharedRecordProfile(widget)) {
			return fmt.Errorf("widget %s shares an application record only from its profile", widget)
		}
		readonlyChoice := field == "choiceVariable" && v.Mode == "constant"
		if !ok || v.Type != port.Type || port.Writable && field != "selectionSetVariable" && !readonlyChoice && !(v.Writable || port.StateWritable && v.Mode == "state") {
			return fmt.Errorf("widget %s has an invalid %s port", widget, field)
		}
	}
	object, known := binding.ObjectRef(p).Name, binding.ObjectRef(p).Name != ""
	if id := binding.Ports["selectionSetVariable"]; id != "" && known {
		if v := p.InputSchema.Variables[id]; v.Mode == "shared" && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object) {
			return fmt.Errorf("shared record-set object does not match its table")
		}
	}
	if id := binding.Ports["collectionVariable"]; id != "" && known && p.WindowVariableObject(id) != object {
		return fmt.Errorf("table window object does not match its plan")
	}
	local := p.scopeOwner(owner, "loop") != "" || p.scopeOwner(owner, "overlay") != ""
	if id := binding.Ports["filterVariable"]; id != "" {
		v := p.InputSchema.Variables[id]
		if p.scopeOwner(owner, "loop") != "" || binding.Ports["collectionVariable"] != "" || v.Type != "filter" || v.Mode != "shared" || widget == "filter" && (!v.Writable || p.scopeOwner(owner, "overlay") != "") {
			return fmt.Errorf("page section %s has an invalid shared filter port", binding.ID)
		}
		if known && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object) {
			return fmt.Errorf("shared filter object does not match its widget")
		}
	}
	if id := binding.Ports["selectionVariable"]; id != "" {
		v := p.InputSchema.Variables[id]
		if local || binding.Ports["selection"] != "" || v.Type != "record" || v.Mode != "shared" || !v.Writable {
			return fmt.Errorf("page section %s needs a writable shared record output on a root table", binding.ID)
		}
		if known && object != p.RecordVariableObject(id) {
			return fmt.Errorf("shared selection object does not match its table")
		}
	}
	return nil
}

func sharedRecordProfile(widget string) string {
	switch widget {
	case "scene-3d", "observation":
		return pageWidgets.Runtime.Telemetry.SharedUIProfile
	case "vertex-graph":
		return pageWidgets.Runtime.Exploration.SharedVertexUIProfile
	case "graph-explorer", "breadcrumb", "avatar-stack":
		return pageWidgets.Runtime.ContextViews.SharedUIProfile
	}
	return "platform.page.v2.14"
}

var pageVariablePortFields = func() map[string]bool {
	fields := map[string]bool{}
	for _, widget := range pageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField != "enabledWhen" {
				fields[port.BindingField] = true
			}
		}
	}
	return fields
}()

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

func (s Section) variablePorts() map[string]string {
	values := map[string]any{}
	_ = json.Unmarshal(Raw(s), &values)
	ports := map[string]string{}
	for _, widget := range pageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField == "enabledWhen" {
				continue
			}
			for _, value := range legacyPagePathValues(values, strings.Split(port.BindingField, ".")) {
				if text, ok := value.(string); ok && text != "" {
					ports[port.BindingField] = text
				}
			}
		}
	}
	return ports
}

func PageUIProfileSupports(profile, required string) bool {
	if profile == "platform.page.v3" {
		return true
	}
	current, minimum := slices.Index(pageWidgets.SupportedProfiles, profile), slices.Index(pageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

var Widgets = func() []string {
	ids := make([]string, 0, len(pageWidgets.Widgets))
	for _, widget := range pageWidgets.Widgets {
		ids = append(ids, widget.ComponentID)
	}
	return ids
}()
