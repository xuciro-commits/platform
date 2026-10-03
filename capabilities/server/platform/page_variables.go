package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

// PageVariable is presentation state, never a business record or authorization.
// Additional types/scopes require a versioned runtime contract.
type PageVariable struct {
	Writable   bool                `json:"writable,omitempty"`
	Title      string              `json:"title,omitempty"`
	Scope      string              `json:"scope"`
	Owner      string              `json:"owner,omitempty"`
	Type       string              `json:"type"`
	Mode       string              `json:"mode"`
	Initial    json.RawMessage     `json:"initial,omitempty"`
	Expression *PageExpression     `json:"expression,omitempty"`
	Source     *PageResourceSource `json:"source,omitempty"`
}

// PageResourceSource names a typed widget output, loop item or application
// presentation port. Widget sources retain their original read boundary.
type PageResourceSource struct {
	Measure  string    `json:"measure,omitempty"`
	Field    string    `json:"field,omitempty"`
	Fields   []string  `json:"fields,omitempty"`
	Object   *AssetRef `json:"object,omitempty"` // object requirement of a shared window
	Query    string    `json:"query,omitempty"`
	Variable string    `json:"variable,omitempty"`
	Kind     string    `json:"kind"`
	Section  string    `json:"section,omitempty"`
	Node     string    `json:"node,omitempty"`
}
type PageExpression struct {
	Op   string      `json:"op"`
	Args []PageValue `json:"args"`
}
type PageValue struct {
	Variable string          `json:"variable,omitempty"`
	Literal  json.RawMessage `json:"literal,omitempty"`
}
type pageRuntimeContract struct {
	Histogram struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxBins           int    `json:"maxBins"`
	} `json:"histogram"`
	Terms struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxGroups         int    `json:"maxGroups"`
	} `json:"terms"`
	Input struct {
		SearchRequiredUIProfile string `json:"searchRequiredUIProfile"`
	} `json:"input"`
	Spacer struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
	} `json:"spacer"`
	Separator struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxLabelBytes     int    `json:"maxLabelBytes"`
	} `json:"separator"`
	Notice struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxTitleBytes     int      `json:"maxTitleBytes"`
		MaxMessageBytes   int      `json:"maxMessageBytes"`
		Tones             []string `json:"tones"`
	} `json:"notice"`
	AlertBanner struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxMessageBytes   int      `json:"maxMessageBytes"`
		Tones             []string `json:"tones"`
	} `json:"alertBanner"`
	RecordPicker struct {
		RequiredUIProfile      string `json:"requiredUIProfile"`
		MaxCandidates          int    `json:"maxCandidates"`
		ValueRequiredUIProfile string `json:"valueRequiredUIProfile"`
	} `json:"recordPicker"`
	DateInput struct {
		RequiredUIProfile         string `json:"requiredUIProfile"`
		DateTimeRequiredUIProfile string `json:"dateTimeRequiredUIProfile"`
	} `json:"dateInput"`
	ChoiceInput struct {
		RequiredUIProfile         string   `json:"requiredUIProfile"`
		Variants                  []string `json:"variants"`
		MaxOptions                int      `json:"maxOptions"`
		MaxOptionBytes            int      `json:"maxOptionBytes"`
		MultipleRequiredUIProfile string   `json:"multipleRequiredUIProfile"`
		ClearRequiredUIProfile    string   `json:"clearRequiredUIProfile"`
		MaxSelected               int      `json:"maxSelected"`
	} `json:"choiceInput"`
	BooleanInput struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		Variants          []string `json:"variants"`
	} `json:"booleanInput"`
	RangeInput struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxTicks          int    `json:"maxTicks"`
	} `json:"rangeInput"`
	Leaderboard struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxRanks          int    `json:"maxRanks"`
	} `json:"leaderboard"`
	Summary struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
	} `json:"summary"`
	Gauge struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
	} `json:"gauge"`
	Progress struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
	} `json:"progress"`
	RecordGantt struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxRows           int      `json:"maxRows"`
		MaxTones          int      `json:"maxTones"`
		Tones             []string `json:"tones"`
	} `json:"recordGantt"`
	RecordCalendar struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxRecords        int    `json:"maxRecords"`
	} `json:"recordCalendar"`
	RecordEvents struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxEvents         int      `json:"maxEvents"`
		MaxTones          int      `json:"maxTones"`
		Tones             []string `json:"tones"`
	} `json:"recordEvents"`
	ChartPresentation struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		Variants          []string `json:"variants"`
	} `json:"chartPresentation"`
	Sparkline struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxPoints         int    `json:"maxPoints"`
	} `json:"sparkline"`
	Treemap struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
	} `json:"treemap"`
	Heatmap struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxAxis           int    `json:"maxAxis"`
		MaxCells          int    `json:"maxCells"`
	} `json:"heatmap"`
	RecordScatter struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxPoints         int    `json:"maxPoints"`
	} `json:"recordScatter"`
	RecordChart struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		Marks             []string `json:"marks"`
		MaxPoints         int      `json:"maxPoints"`
	} `json:"recordChart"`
	RecordList struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		Layouts           []string `json:"layouts"`
		MaxFields         int      `json:"maxFields"`
	} `json:"recordList"`
	Titles struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxTextBytes      int      `json:"maxTextBytes"`
		HeadingLevels     []string `json:"headingLevels"`
	} `json:"titles"`
	MetricPresentation struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxUnitBytes      int      `json:"maxUnitBytes"`
		Formatters        []string `json:"formatters"`
		Variants          []string `json:"variants"`
		Tones             []string `json:"tones"`
	} `json:"metricPresentation"`
	StatusTracker struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxStages         int    `json:"maxStages"`
	} `json:"statusTracker"`
	RecordLinks struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxGroups         int    `json:"maxGroups"`
		MaxTitleBytes     int    `json:"maxTitleBytes"`
	} `json:"recordLinks"`
	ButtonGroup struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		MaxButtons        int      `json:"maxButtons"`
		MaxTitleBytes     int      `json:"maxTitleBytes"`
		Variants          []string `json:"variants"`
		Icons             []string `json:"icons"`
	} `json:"buttonGroup"`
	RecordView struct {
		RequiredUIProfile string   `json:"requiredUIProfile"`
		Tabs              []string `json:"tabs"`
	} `json:"recordView"`
	DetailPresentation struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxColumns        int    `json:"maxColumns"`
	} `json:"detailPresentation"`
	TablePresentation struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxColumns        int    `json:"maxColumns"`
		MaxTitleBytes     int    `json:"maxTitleBytes"`
		MinWidth          int    `json:"minWidth"`
		MaxWidth          int    `json:"maxWidth"`
		Formatters        []struct {
			ID         string   `json:"id"`
			FieldTypes []string `json:"fieldTypes"`
		} `json:"formatters"`
	} `json:"tablePresentation"`
	RecordSelection struct {
		MaxRecords int `json:"maxRecords"`
	} `json:"recordSelection"`
	TableEditing struct {
		MaxRows    int      `json:"maxRows"`
		MaxFields  int      `json:"maxFields"`
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
		MaxDepth            int      `json:"maxDepth"`
		Scope               string   `json:"scope"`
		Source              string   `json:"source"`
		MaxContainers       int      `json:"maxContainers"`
		MaxItems            int      `json:"maxItems"`
		MaxTotalItems       int      `json:"maxTotalItems"`
		RecordWidgets       []string `json:"recordWidgets"`
		PresentationWidgets []string `json:"presentationWidgets"`
	} `json:"loop"`
	Aggregate struct {
		Source           string `json:"source"`
		MaxVariables     int    `json:"maxVariables"`
		MaxExpandedReads int    `json:"maxExpandedReads"`
	} `json:"aggregate"`
	Query struct {
		Set struct {
			Operations     []string `json:"operations"`
			MaxConditions  int      `json:"maxConditions"`
			MaxSearchBytes int      `json:"maxSearchBytes"`
			MaxNodes       int      `json:"maxNodes"`
			MaxDepth       int      `json:"maxDepth"`
			MaxBytes       int      `json:"maxBytes"`
		} `json:"set"`
		Source        string   `json:"source"`
		MaxPlans      int      `json:"maxPlans"`
		MaxConditions int      `json:"maxConditions"`
		MaxSort       int      `json:"maxSort"`
		MaxLimit      int      `json:"maxLimit"`
		MaxTotalLimit int      `json:"maxTotalLimit"`
		MaxOffset     int      `json:"maxOffset"`
		Operators     []string `json:"operators"`
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

// CheckVariables checks the same finite type/operator graph consumed by the
// browser. It evaluates no source text and accesses no records or capabilities.
func (d *PageDocument) CheckVariables() error {
	contract := pageWidgets.Runtime
	if len(d.Variables) > contract.MaxVariables {
		return fmt.Errorf("page variable limit exceeded")
	}
	ids := make([]string, 0, len(d.Variables))
	for id := range d.Variables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) (string, error)
	visit = func(id string) (string, error) {
		v, ok := d.Variables[id]
		fail := func(reason string) (string, error) { return "", fmt.Errorf("page variable %s: %s", id, reason) }
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
		if v.Type == "record-set" && (!PageUIProfileSupports(d.UIProfile, "platform.page.v2.32") || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "records" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
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
		if v.Type == "statistics" && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Summary.RequiredUIProfile) || v.Mode != "aggregate" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("statistics needs its read-only scoped profile")
		}
		if v.Type == "number" && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Gauge.RequiredUIProfile) || !slices.Contains([]string{"aggregate", "constant", "derived"}, v.Mode) || v.Scope == "application") {
			return fail("number needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Measure != "" && (v.Mode != "aggregate" || v.Source.Kind != "aggregate" && v.Source.Kind != "statistics") {
			return fail("measure needs an aggregate scalar")
		}
		if v.Mode != "resource" && v.Mode != "property" && v.Mode != "aggregate" && v.Mode != contract.Application.BindingMode && v.Source != nil {
			return fail("only resource or shared variables may declare a source")
		}
		if v.Source != nil && v.Source.Object != nil && !((v.Mode == "shared" && (v.Type == "object-set" || v.Type == "record" || v.Type == "filter")) || (v.Mode == "resource" && v.Scope == "application" && (v.Type == "record" || v.Type == "filter")) || v.Mode == "property") {
			return fail("only shared windows declare an object requirement")
		}
		if v.Source != nil && len(v.Source.Fields) > 0 && !(v.Scope == "application" && v.Mode == "resource" && v.Type == "filter") {
			return fail("only an application filter declares fields")
		}
		if v.Source != nil && v.Source.Field != "" && v.Mode != "property" {
			return fail("only a property source declares a field")
		}
		switch v.Mode {
		case "aggregate":
			if v.Source == nil || !((v.Type == "decimal" && v.Source.Kind == contract.Aggregate.Source && v.Source.Measure == "") || (v.Type == "number" && v.Source.Kind == "aggregate") || (v.Type == "statistics" && v.Source.Kind == "statistics" && pageNodeID.MatchString(v.Source.Measure))) || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Variable != "" || v.Expression != nil || len(v.Initial) > 0 {
				return fail("aggregate needs only a count query source")
			}
			if v.Source.Kind == "aggregate" {
				if _, _, ok := stringsCutMeasure(v.Source.Measure); !ok {
					return fail("aggregate needs a supported numeric measure")
				}
			}
		case "property":
			if v.Source == nil || v.Source.Kind != "property" || !pageNodeID.MatchString(v.Source.Variable) || !pageNodeID.MatchString(v.Source.Field) || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Source.Fields) > 0 || len(v.Initial) > 0 || v.Expression != nil || !slices.Contains([]string{"string", "boolean", "decimal"}, v.Type) {
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
			if (v.Type == "record" || v.Type == "filter") && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared record needs an object requirement")
			}
			if v.Type == "object-set" && (v.Writable || v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared window needs a read-only object requirement")
			}
			if v.Scope != "application" || v.Source == nil || v.Source.Kind != "application" || !pageNodeID.MatchString(v.Source.Variable) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("shared binding needs only an application variable source")
			}
		case "input":
			if v.Scope != "page" || v.Source != nil || v.Expression != nil || !slices.Contains(contract.Interface.ValueTypes, v.Type) || v.Type == "record" && len(v.Initial) != 0 || len(v.Initial) != 0 && pageLiteralType(v.Initial) != v.Type {
				return fail("input needs a typed page value without a source")
			}
		case "resource":
			if v.Source == nil || v.Source.Variable != "" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("resource variable needs only a typed source")
			}
			if (v.Source.Kind == "record" || v.Source.Kind == "filter") && v.Scope == "application" {
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
			if v.Type == "string-set" && (!PageUIProfileSupports(d.UIProfile, "platform.page.v2.30") || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
				return fail("string-set state requires v2.30 page or overlay scope")
			}
			if v.Expression != nil || pageLiteralType(v.Initial) != v.Type {
				return fail("initial value type mismatch")
			}
		case "derived":
			if len(v.Initial) != 0 || v.Expression == nil {
				return fail("derived variable needs only an expression")
			}
			expr := v.Expression
			if expr.Op == "parse-number" && !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Gauge.RequiredUIProfile) {
				return fail("parse-number requires its profile")
			}
			if expr != nil && expr.Op == "parse-decimal" && !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Progress.RequiredUIProfile) {
				return fail("parse-decimal requires its profile")
			}
			at := slices.IndexFunc(contract.Operators, func(op pageOperator) bool { return op.ID == expr.Op })
			if at < 0 {
				return fail("unsupported operator")
			}
			op := contract.Operators[at]
			if len(expr.Args) < op.MinArgs || len(expr.Args) > op.MaxArgs || v.Type != op.Output {
				return fail("operator arity or output type mismatch")
			}
			first := ""
			for _, arg := range expr.Args {
				if (arg.Variable != "") == (len(arg.Literal) != 0) {
					return fail("argument needs exactly one variable or literal")
				}
				typ := pageLiteralType(arg.Literal)
				if arg.Variable != "" {
					dependency := d.Variables[arg.Variable]
					if dependency.Scope == contract.Loop.Scope && (v.Scope != contract.Loop.Scope || v.Owner != dependency.Owner) {
						return fail("item dependency escapes its loop scope")
					}
					if dependency.Scope == contract.Overlay.Scope && (v.Scope != contract.Overlay.Scope || v.Owner != dependency.Owner) {
						return fail("overlay dependency escapes its owner scope")
					}
					var err error
					typ, err = visit(arg.Variable)
					if err != nil {
						return "", err
					}
				}
				if first == "" {
					first = typ
				}
				resource := typ == "record" || typ == "filter" || typ == "object-set"
				if typ == "statistics" || typ == "" || (op.Input == "same" && (typ != first || resource)) || (op.Input == "resource" && !resource) || (op.Input != "same" && op.Input != "resource" && typ != op.Input) {
					return fail("argument type mismatch")
				}
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
