package platform

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

func (d PageInputSchema) ProfileVersion() string {
	if d.profile != "" {
		return d.profile
	}
	return "platform.page.v3"
}
