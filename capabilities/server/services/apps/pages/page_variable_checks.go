package pages

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"sort"
	"strings"
)

func CheckVariables(d platform.PageInputSchema, engines ...platform.RuleEngine) error {
	contract := platform.PageWidgets.Runtime
	if len(d.Variables) > contract.MaxVariables {
		return fmt.Errorf("page variable limit exceeded")
	}
	ids := make([]string, 0, len(d.Variables))
	for id := range d.Variables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len((platform.Page{Format: 4, InputSchema: d}).ComputeResources()) > platform.PageWidgets.Runtime.ComputeResource.MaxResources {
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
		if !platform.PageNodeID.MatchString(id) || (v.Scope != contract.Scope && v.Scope != contract.Loop.Scope && v.Scope != contract.Overlay.Scope && v.Scope != contract.Application.Scope) || ((v.Scope == contract.Scope || v.Scope == contract.Application.Scope) && v.Owner != "") || ((v.Scope == contract.Loop.Scope || v.Scope == contract.Overlay.Scope) && !platform.PageNodeID.MatchString(v.Owner)) || !slices.Contains(contract.ValueTypes, v.Type) || len(v.Title) > 1024 {
			return fail("unsupported identity, scope or type")
		}
		if v.Type == "record-set" && !((platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.32") && v.Mode == "resource" && v.Source != nil && v.Source.Kind == "records" && slices.Contains([]string{"page", "overlay"}, v.Scope)) || (platform.PageUIProfileSupports(d.ProfileVersion(), platform.PageWidgets.Runtime.RecordSelection.SharedUIProfile) && v.Scope == "application" && (v.Mode == "shared" || v.Mode == "resource"))) {
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
		if v.Type == "statistics" && (!platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.50") || v.Mode != "aggregate" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("statistics needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Compute != nil && (v.Mode != "resource" || v.Source.Kind != "compute") {
			return fail("compute needs its original source kind")
		}
		if v.Type == "number" && (!platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.49") || !(slices.Contains([]string{"aggregate", "constant", "derived"}, v.Mode) || v.Mode == "resource" && v.Source != nil && v.Source.Kind == "compute" && platform.PageUIProfileSupports(d.ProfileVersion(), platform.PageWidgets.Runtime.ComputeResource.RequiredUIProfile)) || v.Scope == "application") {
			return fail("number needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Port != "" && (!platform.PageUIProfileSupports(d.ProfileVersion(), platform.PageWidgets.Runtime.Exploration.RequiredUIProfile) || !platform.PageNodeID.MatchString(v.Source.Port) || v.Type != "record" || v.Mode != "resource" || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
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
			if v.Source == nil || !((v.Type == "decimal" && v.Source.Kind == contract.Aggregate.Source && v.Source.Measure == "") || (v.Type == "number" && v.Source.Kind == "aggregate") || (v.Type == "statistics" && v.Source.Kind == "statistics" && platform.PageNodeID.MatchString(v.Source.Measure))) || !platform.PageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Variable != "" || len(v.Initial) > 0 {
				return fail("aggregate needs only a count query source")
			}
			if v.Source.Kind == "aggregate" {
				if _, _, ok := stringsCutMeasure(v.Source.Measure); !ok {
					return fail("aggregate needs a supported numeric measure")
				}
			}
		case "property":
			if v.Source == nil || v.Source.Kind != "property" || !platform.PageNodeID.MatchString(v.Source.Variable) || !platform.PageNodeID.MatchString(v.Source.Field) || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != platform.AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Source.Fields) > 0 || len(v.Initial) > 0 || !slices.Contains([]string{"string", "boolean", "decimal"}, v.Type) {
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
			if (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != platform.AssetObject) {
				return fail("shared record needs an object requirement")
			}
			if v.Type == "object-set" && (v.Writable || v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != platform.AssetObject) {
				return fail("shared window needs a read-only object requirement")
			}
			if v.Scope != "application" || v.Source == nil || v.Source.Kind != "application" || !platform.PageNodeID.MatchString(v.Source.Variable) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Initial) != 0 {
				return fail("shared binding needs only an application variable source")
			}
		case "input":
			if v.Type == "object-set" && !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.85") {
				return fail("collection input needs v2.85")
			}
			if v.Scope != "page" || v.Source != nil || !slices.Contains(contract.Interface.ValueTypes, v.Type) || (v.Type == "record" || v.Type == "object-set") && len(v.Initial) != 0 || len(v.Initial) != 0 && literalType(v.Initial) != v.Type {
				return fail("input needs a typed page value without a source")
			}
		case "resource":
			if v.Source != nil && v.Source.Kind == "compute" {
				if err := checkComputeResource(d, v); err != nil {
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
				if v.Type != v.Source.Kind || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != platform.AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" {
					return fail("application record needs only an object source")
				}
				if v.Type == "filter" {
					if len(v.Source.Fields) == 0 || len(v.Source.Fields) > contract.Application.MaxFilterFields {
						return fail("application filter needs bounded fields")
					}
					seen := map[string]bool{}
					for _, field := range v.Source.Fields {
						if !platform.PageNodeID.MatchString(field) || seen[field] {
							return fail("invalid filter field")
						}
						seen[field] = true
					}
				}
				break
			}
			if v.Source.Kind == "plan" {
				if (v.Scope != "page" && v.Scope != "overlay" && v.Scope != "application" && v.Scope != "loop-item") || v.Type != "object-set" || !platform.PageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" {
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
			if v.Scope == "application" || !platform.PageNodeID.MatchString(v.Source.Section) || v.Source.Node != "" {
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
			if v.Type == "string-set" && (!platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.30") || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
				return fail("string-set state requires v2.30 page or overlay scope")
			}
			if literalType(v.Initial) != v.Type {
				return fail("initial value type mismatch")
			}
		case "derived":
			if len(v.Initial) != 0 || v.Rule == nil {
				return fail("derived variable needs only an expression")
			}
			schema := pageRuleSchema(d)
			var walk func(platform.Expr) error
			walk = func(e platform.Expr) error {
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
			if !pageRuleFits(v.Type, v.Rule.Kind) {
				return fail("rule output type differs")
			}
			if _, err := platform.RuleEngineOf(engines).CompileExpr(*v.Rule, schema); err != nil {
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

func checkComputeResource(d platform.PageInputSchema, v platform.PageVariable) error {
	c := v.Source.Compute
	if !platform.PageUIProfileSupports(d.ProfileVersion(), platform.PageWidgets.Runtime.ComputeResource.RequiredUIProfile) || !slices.Contains([]string{"number", "decimal"}, v.Type) || v.Mode != "resource" || v.Writable || !slices.Contains([]string{"page", "overlay"}, v.Scope) || c == nil || !platform.PageNodeID.MatchString(c.RecordVariable) || c.Operation.Ref.Kind != platform.AssetCompute || c.Operation.Ref.Check() != nil || c.Operation.SourceVersion == "" || len(c.Inputs) == 0 || len(c.Inputs) > 64 || c.OutputField != "" && !platform.PageNodeID.MatchString(c.OutputField) {
		return fmt.Errorf("compute resource needs its finite typed operation and original record")
	}
	parent := d.Variables[c.RecordVariable]
	if parent.Type != "record" || !(sharedContextRecord(d, c.RecordVariable) || parent.Mode == "resource" && parent.Source != nil && parent.Source.Kind == "record" && parent.Scope == v.Scope && parent.Owner == v.Owner) {
		return fmt.Errorf("compute input needs its confirmed record in the original owner")
	}
	subject := false
	for name, b := range c.Inputs {
		if !platform.PageNodeID.MatchString(name) || b.Check() != nil || b.Source != "literal" && b.Source != "subject" || b.Source == "subject" && (len(b.Path) < 1 || len(b.Path) > 16 || slices.ContainsFunc(b.Path, func(part string) bool {
			return !platform.PageNodeID.MatchString(part)
		})) {
			return fmt.Errorf("compute input needs a scalar source field or typed literal")
		}
		subject = subject || b.Source == "subject"
	}
	if !subject {
		return fmt.Errorf("record computation needs an original protected source field")
	}
	s := v.Source
	if s.Section != "" || s.Node != "" || s.Query != "" || s.Variable != "" || s.Object != nil || s.Field != "" || len(s.Fields) > 0 || s.Measure != "" || s.Port != "" {
		return fmt.Errorf("compute resource cannot carry another source")
	}
	return nil
}

func sharedContextRecord(d platform.PageInputSchema, id string) bool {
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == platform.AssetObject && v.Source.Object.Check() == nil
}

func checkAggregateScalar(v platform.PageVariable, info platform.EntityInfo) error {
	if v.Mode != "aggregate" || v.Source == nil || v.Source.Kind != "aggregate" && v.Source.Kind != "statistics" {
		return nil
	}
	_, field, ok := stringsCutMeasure(v.Source.Measure)
	if v.Source.Kind == "statistics" {
		field = v.Source.Measure
		ok = platform.PageNodeID.MatchString(field)
	}
	f, found := info.Field(field)
	if !ok || !found || (f.Type != "integer" && f.Type != "decimal") {
		return fmt.Errorf("aggregate scalar needs an original visible numeric field")
	}
	return nil
}

func stringsCutMeasure(measure string) (string, string, bool) {
	op, field, ok := strings.Cut(measure, ":")
	return op, field, ok && (op == "sum" || op == "avg" || op == "min" || op == "max") && platform.PageNodeID.MatchString(field)
}

func pageRuleKind(kind string) string {
	switch kind {
	case "string":
		return "text"
	case "record", "filter", "object-set", "record-set":
		return "reference"
	case "string-set":
		return "set"
	default:
		return kind
	}
}

func pageRuleSchema(d platform.PageInputSchema) platform.ExprSchema {
	schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}, Sets: map[string]platform.ExprSetType{}}
	for name, v := range d.Variables {
		t := platform.ExprType{Kind: pageRuleKind(v.Type)}
		if t.Kind == "reference" {
			t.Resource = v.Type
		}
		if t.Kind == "set" {
			t.Element = &platform.ExprType{Kind: "text"}
		}
		if v.RuleType != nil {
			if v.Mode == "property" {
				t.Kind = v.RuleType.Kind
			}
			t.Fields = v.RuleType.Fields
		}
		if v.Rule != nil {
			t.Kind = v.Rule.Kind
		}
		schema.Refs[name] = t
		if v.Type == "object-set" && v.RuleType != nil {
			schema.Sets[name] = platform.ExprSetType{Element: *v.RuleType, Policy: "visible"}
		}
	}
	return schema
}

func pageRuleFits(carrier, kind string) bool {
	if pageRuleKind(carrier) == kind {
		return true
	}
	if carrier == "decimal" && kind == "integer" {
		return true
	}
	if carrier == "string" {
		return kind == "date" || kind == "datetime" || kind == "choice" || kind == "reference"
	}
	return false
}

func CheckApplicationVariables(a platform.Application) error {
	if a.UIProfile != "" && !platform.SupportsPageUIProfile(a.UIProfile) {
		return fmt.Errorf("unsupported application UI profile %s", a.UIProfile)
	}
	if len(a.Variables) == 0 && len(a.Queries) == 0 {
		return nil
	}
	if !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.8") {
		return fmt.Errorf("application variables require UI profile v2.8")
	}
	if (len(a.Queries) > 0 || applicationHasResources(a)) && !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.12") {
		return fmt.Errorf("application query resources require v2.12")
	}
	for id, v := range a.Variables {
		if v.Mode == "property" && !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.17") {
			return fmt.Errorf("application property %s requires v2.17", id)
		}
		if v.Type == "decimal" && !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.16") {
			return fmt.Errorf("application decimal %s requires v2.16", id)
		}
		if v.Type == "filter" && !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.15") {
			return fmt.Errorf("application filter %s requires v2.15", id)
		}
		if v.Type == "record-set" && !platform.PageUIProfileSupports(a.UIProfile, platform.PageWidgets.Runtime.RecordSelection.SharedUIProfile) {
			return fmt.Errorf("application record-set %s requires its shared profile", id)
		}
		if v.Type == "record" && !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.14") {
			return fmt.Errorf("application record %s requires v2.14", id)
		}
		if v.Scope != "application" || !slices.Contains(platform.PageWidgets.Runtime.Application.Modes, v.Mode) {
			return fmt.Errorf("application variable %s needs its own pure scalar declaration", id)
		}
	}
	d := a.QueryPage().InputSchema
	if err := checkQueries(platform.Page{Format: 4, InputSchema: d}, "application"); err != nil {
		return err
	}
	return CheckVariables(d)
}

func applicationHasResources(a platform.Application) bool {
	for _, v := range a.Variables {
		if v.Mode == "resource" {
			return true
		}
	}
	return false
}
