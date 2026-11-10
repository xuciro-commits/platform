package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type PageVariable struct {
	RuleType *ExprType           `json:"ruleType,omitempty"`
	Rule     *Expr               `json:"rule,omitempty"`
	Writable bool                `json:"writable,omitempty"`
	Title    string              `json:"title,omitempty"`
	Scope    string              `json:"scope"`
	Owner    string              `json:"owner,omitempty"`
	Type     string              `json:"type"`
	Mode     string              `json:"mode"`
	Initial  json.RawMessage     `json:"initial,omitempty"`
	Source   *PageResourceSource `json:"source,omitempty"`
}

type PageResourceSource struct {
	Compute *PageComputeResource `json:"compute,omitempty"`
	Port    string               `json:"port,omitempty"`
	Measure string               `json:"measure,omitempty"`
	Field   string               `json:"field,omitempty"`
	Fields  []string             `json:"fields,omitempty"`

	Object   *AssetRef `json:"object,omitempty"`
	Query    string    `json:"query,omitempty"`
	Variable string    `json:"variable,omitempty"`
	Kind     string    `json:"kind"`
	Section  string    `json:"section,omitempty"`
	Node     string    `json:"node,omitempty"`
}

type PageValue struct {
	Variable string          `json:"variable,omitempty"`
	Literal  json.RawMessage `json:"literal,omitempty"`
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

func pageLiteralType(raw json.RawMessage) string {
	if _, ok := NumberLiteral(raw); ok {
		return "number"
	}
	if _, ok := DecimalLiteral(raw); ok {
		return "decimal"
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	if m, ok := value.(map[string]any); ok && len(m) == 2 && m["kind"] == "string-set" {
		if values, ok := m["values"].([]any); ok && len(values) <= 64 {
			seen := map[string]bool{}
			for _, item := range values {
				v, ok := item.(string)
				if !ok || len(v) > 4096 || seen[v] {
					return ""
				}
				seen[v] = true
			}
			return "string-set"
		}
	}
	switch v := value.(type) {
	case bool:
		return "boolean"
	case string:
		if len(v) <= pageWidgets.Runtime.MaxStringBytes {
			return "string"
		}
	}
	return ""
}

func (d PageInputSchema) CheckVariables(engines ...RuleEngine) error {
	contract := pageWidgets.Runtime
	if len(d.Variables) > contract.MaxVariables {
		return fmt.Errorf("page variable limit exceeded")
	}
	ids := make([]string, 0, len(d.Variables))
	for id := range d.Variables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len((Page{Format: 4, InputSchema: d}).ComputeResources()) > pageWidgets.Runtime.ComputeResource.MaxResources {
		return fmt.Errorf("page compute resources exceed their bound")
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) (string, error)
	visit = func(id string) (string, error) {
		v, ok := d.Variables[id]
		fail := func(reason string) (string, error) {
			return "", fmt.Errorf("page variable %s: %s", id, reason)
		}
		if !ok {
			return fail("missing dependency")
		}
		if visiting[id] {
			return fail("cyclic dependency")
		}
		if visited[id] {
			return v.Type, nil
		}
		if !pageNodeID.MatchString(id) || (v.Scope != contract.Scope && v.Scope != contract.Loop.Scope && v.Scope != contract.Overlay.Scope && v.Scope != contract.Application.Scope) || ((v.Scope == contract.Scope || v.Scope == contract.Application.Scope) && v.Owner != "") || ((v.Scope == contract.Loop.Scope || v.Scope == contract.Overlay.Scope) && !pageNodeID.MatchString(v.Owner)) || !slices.Contains(contract.ValueTypes, v.Type) || len(v.Title) > 1024 {
			return fail("unsupported identity, scope or type")
		}
		if v.Type == "record-set" && !((PageUIProfileSupports(d.profileVersion(), "platform.page.v2.32") && v.Mode == "resource" && v.Source != nil && v.Source.Kind == "records" && slices.Contains([]string{"page", "overlay"}, v.Scope)) || (PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.RecordSelection.SharedUIProfile) && v.Scope == "application" && (v.Mode == "shared" || v.Mode == "resource"))) {
			return fail("record-set needs a v2.32 local table resource")
		}
		if v.Scope == contract.Overlay.Scope && (!slices.Contains(contract.Overlay.ValueTypes, v.Type) || !slices.Contains(contract.Overlay.Modes, v.Mode)) {
			return fail("overlay variable needs a supported local value or resource")
		}
		visiting[id] = true
		if v.Scope == contract.Application.Scope && !slices.Contains(contract.Application.ValueTypes, v.Type) {
			return fail("application variable needs a supported scalar or resource")
		}
		if v.Writable && v.Mode != contract.Application.BindingMode {
			return fail("only shared bindings declare writable")
		}
		if v.Type == "statistics" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.50") || v.Mode != "aggregate" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("statistics needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Compute != nil && (v.Mode != "resource" || v.Source.Kind != "compute") {
			return fail("compute needs its original source kind")
		}
		if v.Type == "number" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.49") || !(slices.Contains([]string{"aggregate", "constant", "derived"}, v.Mode) || v.Mode == "resource" && v.Source != nil && v.Source.Kind == "compute" && PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.ComputeResource.RequiredUIProfile)) || v.Scope == "application") {
			return fail("number needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Port != "" && (!PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.Exploration.RequiredUIProfile) || !pageNodeID.MatchString(v.Source.Port) || v.Type != "record" || v.Mode != "resource" || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("record output port needs its scoped finite graph profile")
		}
		if v.Source != nil && v.Source.Measure != "" && (v.Mode != "aggregate" || v.Source.Kind != "aggregate" && v.Source.Kind != "statistics") {
			return fail("measure needs an aggregate scalar")
		}
		if v.Mode != "resource" && v.Mode != "property" && v.Mode != "aggregate" && v.Mode != contract.Application.BindingMode && v.Source != nil {
			return fail("only resource or shared variables may declare a source")
		}
		if v.Source != nil && v.Source.Object != nil && !((v.Mode == "shared" && (v.Type == "object-set" || v.Type == "record" || v.Type == "filter" || v.Type == "record-set")) || (v.Mode == "resource" && v.Scope == "application" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set")) || v.Mode == "property") {
			return fail("only shared windows declare an object requirement")
		}
		if v.Source != nil && len(v.Source.Fields) > 0 && !(v.Scope == "application" && v.Mode == "resource" && v.Type == "filter") {
			return fail("only an application filter declares fields")
		}
		if v.Source != nil && v.Source.Field != "" && v.Mode != "property" {
			return fail("only a property source declares a field")
		}
		if v.Rule != nil && v.Mode != "derived" {
			return fail("only derived variables carry rules")
		}
		switch v.Mode {
		case "aggregate":
			if v.Source == nil || !((v.Type == "decimal" && v.Source.Kind == contract.Aggregate.Source && v.Source.Measure == "") || (v.Type == "number" && v.Source.Kind == "aggregate") || (v.Type == "statistics" && v.Source.Kind == "statistics" && pageNodeID.MatchString(v.Source.Measure))) || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Variable != "" || len(v.Initial) > 0 {
				return fail("aggregate needs only a count query source")
			}
			if v.Source.Kind == "aggregate" {
				if _, _, ok := stringsCutMeasure(v.Source.Measure); !ok {
					return fail("aggregate needs a supported numeric measure")
				}
			}
		case "property":
			if v.Source == nil || v.Source.Kind != "property" || !pageNodeID.MatchString(v.Source.Variable) || !pageNodeID.MatchString(v.Source.Field) || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Source.Fields) > 0 || len(v.Initial) > 0 || !slices.Contains([]string{"string", "boolean", "decimal"}, v.Type) {
				return fail("property needs a typed record and field source")
			}
			parent := d.Variables[v.Source.Variable]
			if parent.Scope == "loop-item" && (v.Scope != "loop-item" || parent.Owner != v.Owner) || parent.Scope == "overlay" && (v.Scope != "overlay" || parent.Owner != v.Owner) {
				return fail("property source escapes its scope")
			}
			typ, err := visit(v.Source.Variable)
			if err != nil {
				return "", err
			}
			if typ != "record" {
				return fail("property source must be a record")
			}
		case "shared":
			if (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared record needs an object requirement")
			}
			if v.Type == "object-set" && (v.Writable || v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared window needs a read-only object requirement")
			}
			if v.Scope != "application" || v.Source == nil || v.Source.Kind != "application" || !pageNodeID.MatchString(v.Source.Variable) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Initial) != 0 {
				return fail("shared binding needs only an application variable source")
			}
		case "input":
			if v.Type == "object-set" && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.85") {
				return fail("collection input needs v2.85")
			}
			if v.Scope != "page" || v.Source != nil || !slices.Contains(contract.Interface.ValueTypes, v.Type) || (v.Type == "record" || v.Type == "object-set") && len(v.Initial) != 0 || len(v.Initial) != 0 && pageLiteralType(v.Initial) != v.Type {
				return fail("input needs a typed page value without a source")
			}
		case "resource":
			if v.Source != nil && v.Source.Kind == "compute" {
				if err := d.checkComputeResource(v); err != nil {
					return fail(err.Error())
				}
				if _, err := visit(v.Source.Compute.RecordVariable); err != nil {
					return "", err
				}
				break
			}
			if v.Source == nil || v.Source.Variable != "" || len(v.Initial) != 0 {
				return fail("resource variable needs only a typed source")
			}
			if (v.Source.Kind == "record" || v.Source.Kind == "filter" || v.Source.Kind == "record-set") && v.Scope == "application" {
				if v.Type != v.Source.Kind || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" {
					return fail("application record needs only an object source")
				}
				if v.Type == "filter" {
					if len(v.Source.Fields) == 0 || len(v.Source.Fields) > contract.Application.MaxFilterFields {
						return fail("application filter needs bounded fields")
					}
					seen := map[string]bool{}
					for _, field := range v.Source.Fields {
						if !pageNodeID.MatchString(field) || seen[field] {
							return fail("invalid filter field")
						}
						seen[field] = true
					}
				}
				break
			}
			if v.Source.Kind == "plan" {
				if (v.Scope != "page" && v.Scope != "overlay" && v.Scope != "application" && v.Scope != "loop-item") || v.Type != "object-set" || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" {
					return fail("plan source needs a scoped query window")
				}
				break
			}
			if v.Source.Query != "" {
				return fail("only a plan source may declare a query")
			}
			if v.Scope == contract.Loop.Scope {
				if v.Type != "record" || v.Source.Kind != contract.Loop.Source || v.Source.Node != v.Owner || v.Source.Section != "" {
					return fail("item source needs its loop owner")
				}
				break
			}
			if v.Scope == "application" || !pageNodeID.MatchString(v.Source.Section) || v.Source.Node != "" {
				return fail("resource source needs a section")
			}
			found := false
			for _, resource := range contract.Resources {
				if resource.Kind == v.Source.Kind && resource.Type == v.Type {
					found = true
				}
			}
			if !found {
				return fail("resource source type mismatch")
			}
		case "constant", "state":
			if v.Type == "string-set" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.30") || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
				return fail("string-set state requires v2.30 page or overlay scope")
			}
			if pageLiteralType(v.Initial) != v.Type {
				return fail("initial value type mismatch")
			}
		case "derived":
			if len(v.Initial) != 0 || v.Rule == nil {
				return fail("derived variable needs only an expression")
			}
			schema := d.PageRuleSchema()
			var walk func(Expr) error
			walk = func(e Expr) error {
				if e.Agg != nil {
					if _, err := visit(e.Agg.Source); err != nil {
						return err
					}
				}
				if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") {
					dependency := d.Variables[e.Ref]
					if dependency.Scope == contract.Loop.Scope && (v.Scope != contract.Loop.Scope || v.Owner != dependency.Owner) || dependency.Scope == contract.Overlay.Scope && (v.Scope != contract.Overlay.Scope || v.Owner != dependency.Owner) {
						return fmt.Errorf("rule dependency escapes its scope")
					}
					if _, err := visit(e.Ref); err != nil {
						return err
					}
				}
				for _, arg := range e.Args {
					if err := walk(arg); err != nil {
						return err
					}
				}
				if e.In != nil {
					return walk(*e.In)
				}
				return nil
			}
			if err := walk(*v.Rule); err != nil {
				return fail(err.Error())
			}
			if !PageRuleFits(v.Type, v.Rule.Kind) {
				return fail("rule output type differs")
			}
			if _, err := ruleEngineOf(engines).CompileExpr(*v.Rule, schema); err != nil {
				return fail(err.Error())
			}
		default:
			return fail("unsupported variable mode")
		}
		visiting[id] = false
		visited[id] = true
		return v.Type, nil
	}
	for _, id := range ids {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (d PageInputSchema) checkProfile() error {
	for id, v := range d.Variables {
		if err := d.checkVariableProfile(id, v); err != nil {
			return err
		}
	}
	return nil
}

func (d PageInputSchema) checkVariableProfile(id string, v PageVariable) error {
	supports := func(feature string) bool {
		return PageUIProfileSupports(d.profileVersion(), pageWidgets.Features[feature])
	}
	switch {
	case v.Mode == "property" && !supports("propertyVariables"):
		return fmt.Errorf("property variable %s requires v2.17", id)
	case v.Type == "decimal" && !supports("decimalVariables"):
		return fmt.Errorf("decimal variable %s requires v2.16", id)
	case v.Scope == "application" && v.Type == "filter" && !supports("applicationFilters"):
		return fmt.Errorf("shared filter %s requires v2.15", id)
	case v.Scope == "application" && v.Type == "record" && !supports("applicationRecords"):
		return fmt.Errorf("shared record %s requires v2.14", id)
	case v.Scope == "application" && v.Type == "object-set" && !supports("applicationCollections"):
		return fmt.Errorf("shared window %s requires v2.12", id)
	case v.Scope == "application" && (!supports("applicationBindings") || v.Mode != "shared"):
		return fmt.Errorf("page variable %s needs v2.8 and a shared application binding", id)
	case v.Scope == "overlay" && v.Type == "filter" && !supports("overlayFilters"):
		return fmt.Errorf("overlay filter %s requires v2.13", id)
	case v.Scope == "overlay" && v.Mode == "resource" && !supports("overlayResources"):
		return fmt.Errorf("overlay resource %s requires UI profile v2.11", id)
	case v.Scope == "overlay" && !supports("overlayVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.7", id)
	case v.Scope == pageWidgets.Runtime.Loop.Scope && !supports("loopVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.5", id)
	case v.Mode == "resource" && !supports("resourceVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.3", id)
	}
	return nil
}

func (d PageInputSchema) profileVersion() string {
	if d.profile != "" {
		return d.profile
	}
	return "platform.page.v3"
}
