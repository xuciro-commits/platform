package platform

import (
	_ "embed"
	"encoding/json"
)

//go:embed page_compat.json
var pageWidgetJSON []byte

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

var PageWidgets = func() pageUIContract {
	var frozen struct {
		RetainedContract pageUIContract `json:"retainedContract"`
	}
	if err := json.Unmarshal(pageWidgetJSON, &frozen); err != nil {
		panic(err)
	}
	return frozen.RetainedContract
}()

func PageWidget(id string) *pageWidgetContract {
	for i := range PageWidgets.Widgets {
		if PageWidgets.Widgets[i].ComponentID == id {
			return &PageWidgets.Widgets[i]
		}
	}
	return nil
}
