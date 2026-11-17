package pages

import (
	_ "embed"
	"encoding/json"

	"platformserver/platform"
)

//go:embed page_compat.json
var pageCompatibilityJSON []byte

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
	Resources           []platform.PageResourceBinding `json:"resources,omitempty"`
	FieldRoles          []pageWidgetFieldRole          `json:"fieldRoles,omitempty"`
	PortGroups          []pageWidgetPortGroup          `json:"portGroups,omitempty"`
	ModelPorts          []pageModelPort                `json:"modelPorts,omitempty"`
	ConfigurationFields []string                       `json:"configurationFields,omitempty"`
	ComponentID         string                         `json:"componentID"`
	ConfigVersion       int                            `json:"configVersion"`
	RequiredUIProfile   string                         `json:"requiredUIProfile"`
	SelectionMode       string                         `json:"selectionMode"`
	BindingKinds        []string                       `json:"bindingKinds"`
	InputPorts          []pageWidgetPort               `json:"inputPorts"`
	OutputPorts         []pageWidgetPort               `json:"outputPorts"`
	Events              []pageWidgetEvent              `json:"events"`
	Slots               []pageWidgetSlot               `json:"slots"`
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
	if err := json.Unmarshal(pageCompatibilityJSON, &frozen); err != nil {
		panic(err)
	}
	return frozen.RetainedContract
}()

func pageWidget(id string) *pageWidgetContract {
	for i := range pageWidgets.Widgets {
		if pageWidgets.Widgets[i].ComponentID == id {
			return &pageWidgets.Widgets[i]
		}
	}
	return nil
}

type pageRuntimeContract struct {
	Composite struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxRecords        int    `json:"maxRecords"`
		MaxCells          int    `json:"maxCells"`
		MaxBytes          int    `json:"maxBytes"`
	} `json:"composite"`
	ModelBinding struct {
		RequiredUIProfile string `json:"requiredUIProfile"`

		MaxRecords int `json:"maxRecords"`

		MaxBytes   int      `json:"maxBytes"`
		FieldSlots []string `json:"fieldSlots"`
	} `json:"modelBinding"`

	ComputeResource struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxResources      int    `json:"maxResources"`
	} `json:"computeResource"`

	Embedding struct {
		MaxDepth     int `json:"maxDepth"`
		MaxInstances int `json:"maxInstances"`
		MaxQueries   int `json:"maxQueries"`
		MaxRecords   int `json:"maxRecords"`
		MaxSections  int `json:"maxSections"`
	} `json:"embedding"`
	Exploration struct {
		RequiredUIProfile     string `json:"requiredUIProfile"`
		MaxResourceWindow     int    `json:"maxResourceWindow"`
		SharedVertexUIProfile string `json:"sharedVertexUIProfile"`
	} `json:"exploration"`
	Telemetry struct {
		SharedUIProfile string `json:"sharedUIProfile"`
	} `json:"telemetry"`
	ContextViews struct {
		SharedUIProfile   string `json:"sharedUIProfile"`
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxAvatarWindow   int    `json:"maxAvatarWindow"`

		MaxURLBytes int `json:"maxURLBytes"`
	} `json:"contextViews"`

	DateInput struct {
		RequiredUIProfile         string `json:"requiredUIProfile"`
		DateTimeRequiredUIProfile string `json:"dateTimeRequiredUIProfile"`
	} `json:"dateInput"`

	RecordSelection struct {
		MaxRecords      int    `json:"maxRecords"`
		SharedUIProfile string `json:"sharedUIProfile"`
	} `json:"recordSelection"`
	TableEditing struct {
		MaxRows int `json:"maxRows"`

		FieldTypes []string `json:"fieldTypes"`
	} `json:"tableEditing"`
	Scope   string `json:"scope"`
	Decimal struct {
		MaxBytes int `json:"maxBytes"`
	} `json:"decimal"`
	Application struct {
		Scope           string   `json:"scope"`
		ValueTypes      []string `json:"valueTypes"`
		Modes           []string `json:"modes"`
		BindingMode     string   `json:"bindingMode"`
		MaxFilterFields int      `json:"maxFilterFields"`
	} `json:"application"`
	Overlay struct {
		Scope      string   `json:"scope"`
		ValueTypes []string `json:"valueTypes"`
		Modes      []string `json:"modes"`
	} `json:"overlay"`
	ValueTypes     []string       `json:"valueTypes"`
	MaxVariables   int            `json:"maxVariables"`
	MaxStringBytes int            `json:"maxStringBytes"`
	Operators      []pageOperator `json:"operators"`
	Resources      []struct {
		Kind    string   `json:"kind"`
		Type    string   `json:"type"`
		Widget  string   `json:"widget"`
		Widgets []string `json:"widgets,omitempty"`
	} `json:"resources"`
	Loop struct {
		MaxDepth int    `json:"maxDepth"`
		Scope    string `json:"scope"`
		Source   string `json:"source"`

		MaxItems int `json:"maxItems"`
	} `json:"loop"`
	Aggregate struct {
		Source           string `json:"source"`
		MaxVariables     int    `json:"maxVariables"`
		MaxExpandedReads int    `json:"maxExpandedReads"`
	} `json:"aggregate"`
	Query struct {
		Set struct {
			Operations    []string `json:"operations"`
			MaxConditions int      `json:"maxConditions"`

			MaxNodes int `json:"maxNodes"`
			MaxDepth int `json:"maxDepth"`
			MaxBytes int `json:"maxBytes"`
		} `json:"set"`
		Source                string   `json:"source"`
		InventoryUIProfile    string   `json:"inventoryUIProfile"`
		MaxDeclaredPlans      int      `json:"maxDeclaredPlans"`
		InterfaceUIProfile    string   `json:"interfaceUIProfile"`
		MaxDeclaredTotalLimit int      `json:"maxDeclaredTotalLimit"`
		MaxPlans              int      `json:"maxPlans"`
		MaxConditions         int      `json:"maxConditions"`
		MaxSort               int      `json:"maxSort"`
		MaxLimit              int      `json:"maxLimit"`
		MaxTotalLimit         int      `json:"maxTotalLimit"`
		MaxOffset             int      `json:"maxOffset"`
		Operators             []string `json:"operators"`
	} `json:"query"`
	Interface struct {
		MaxPorts   int      `json:"maxPorts"`
		MaxVersion int      `json:"maxVersion"`
		ValueTypes []string `json:"valueTypes"`
	} `json:"interface"`
}

type pageOperator struct {
	ID      string `json:"id"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	MinArgs int    `json:"minArgs"`
	MaxArgs int    `json:"maxArgs"`
}
