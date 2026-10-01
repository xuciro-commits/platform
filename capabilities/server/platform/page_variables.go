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
	Query    string `json:"query,omitempty"`
	Variable string `json:"variable,omitempty"`
	Kind     string `json:"kind"`
	Section  string `json:"section,omitempty"`
	Node     string `json:"node,omitempty"`
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
	Scope       string `json:"scope"`
	Application struct {
		Scope       string   `json:"scope"`
		ValueTypes  []string `json:"valueTypes"`
		Modes       []string `json:"modes"`
		BindingMode string   `json:"bindingMode"`
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
		Kind   string `json:"kind"`
		Type   string `json:"type"`
		Widget string `json:"widget"`
	} `json:"resources"`
	Loop struct {
		Scope               string   `json:"scope"`
		Source              string   `json:"source"`
		MaxContainers       int      `json:"maxContainers"`
		MaxItems            int      `json:"maxItems"`
		MaxTotalItems       int      `json:"maxTotalItems"`
		RecordWidgets       []string `json:"recordWidgets"`
		PresentationWidgets []string `json:"presentationWidgets"`
	} `json:"loop"`
	Query struct {
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
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
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
		if v.Scope == contract.Overlay.Scope && (!slices.Contains(contract.Overlay.ValueTypes, v.Type) || !slices.Contains(contract.Overlay.Modes, v.Mode)) {
			return fail("overlay variable needs a scalar state, constant or expression")
		}
		visiting[id] = true
		if v.Scope == contract.Application.Scope && !slices.Contains(contract.Application.ValueTypes, v.Type) {
			return fail("application variable must be scalar")
		}
		if v.Writable && v.Mode != contract.Application.BindingMode {
			return fail("only shared bindings declare writable")
		}
		if v.Mode != "resource" && v.Mode != contract.Application.BindingMode && v.Source != nil {
			return fail("only resource or shared variables may declare a source")
		}
		switch v.Mode {
		case "shared":
			if v.Scope != "application" || v.Source == nil || v.Source.Kind != "application" || !pageNodeID.MatchString(v.Source.Variable) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("shared binding needs only an application variable source")
			}
		case "input":
			if v.Scope != "page" || v.Source != nil || v.Expression != nil || !slices.Contains(contract.Interface.ValueTypes, v.Type) || v.Type == "record" && len(v.Initial) != 0 || len(v.Initial) != 0 && pageLiteralType(v.Initial) != v.Type {
				return fail("input needs a typed page value without a source")
			}
		case "resource":
			if v.Source == nil || v.Source.Variable != "" || v.Scope == "application" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("resource variable needs only a typed source")
			}
			if v.Source.Kind == "plan" {
				if v.Scope != "page" || v.Type != "object-set" || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" {
					return fail("plan source needs a page query window")
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
			if !pageNodeID.MatchString(v.Source.Section) || v.Source.Node != "" {
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
			if v.Expression != nil || pageLiteralType(v.Initial) != v.Type {
				return fail("initial value type mismatch")
			}
		case "derived":
			if len(v.Initial) != 0 || v.Expression == nil {
				return fail("derived variable needs only an expression")
			}
			expr := v.Expression
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
				if typ == "" || (op.Input == "same" && (typ != first || resource)) || (op.Input == "resource" && !resource) || (op.Input != "same" && op.Input != "resource" && typ != op.Input) {
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
