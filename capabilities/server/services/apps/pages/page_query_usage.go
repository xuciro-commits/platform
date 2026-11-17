package pages

import (
	"encoding/json"
	ruleexpr "platformserver/expr"
	"platformserver/platform"
	"slices"
	"strings"
)

func queryValues(q platform.PageQuery) []platform.PageValue {
	values := []platform.PageValue{}
	if q.Search != nil {
		values = append(values, *q.Search)
	}
	if q.For != nil {
		values = append(values, *q.For)
	}
	return values
}

func QueryVariables(q platform.PageQuery) []string {
	ids := []string{}
	if q.Input != "" {
		ids = append(ids, q.Input)
	}
	for _, v := range queryValues(q) {
		if v.Variable != "" {
			ids = append(ids, v.Variable)
		}
	}
	if q.Filter != nil {
		for _, ref := range ruleexpr.Refs(*q.Filter) {
			if name, ok := strings.CutPrefix(ref, "root."); ok {
				name, _, _ = strings.Cut(name, "#")
				if name != "" && !slices.Contains(ids, name) {
					ids = append(ids, name)
				}
			}
		}
	}
	return ids
}

func variableDependsOnQuery(p platform.Page, variable, target string) bool {
	d := p.InputSchema
	sections := p.DataBindings
	seenVariables, seenQueries := map[string]bool{}, map[string]bool{}
	var variableDepends func(string) bool
	var queryDepends func(string) bool
	queryDepends = func(id string) bool {
		if id == target {
			return true
		}
		if seenQueries[id] {
			return false
		}
		seenQueries[id] = true
		q := d.Queries[id]
		for _, source := range QueryVariables(q) {
			if variableDepends(source) {
				return true
			}
		}
		if q.Set != nil {
			for _, input := range q.Set.Inputs {
				if queryDepends(input) {
					return true
				}
			}
		}
		return false
	}
	variableDepends = func(id string) bool {
		if id == "" || seenVariables[id] {
			return false
		}
		seenVariables[id] = true
		v := d.Variables[id]
		if v.Rule != nil {
			for _, ref := range ruleexpr.Refs(*v.Rule) {
				if variableDepends(ref) {
					return true
				}
			}
		}
		if v.Source == nil {
			return false
		}
		if v.Source.Kind == "property" && variableDepends(v.Source.Variable) {
			return true
		}
		if (v.Source.Kind == "plan" || v.Mode == "aggregate") && queryDepends(v.Source.Query) {
			return true
		}
		if slices.Contains([]string{"record", "records", "query", "filter"}, v.Source.Kind) {
			for _, s := range sections {
				if s.ID != v.Source.Section {
					continue
				}
				for _, input := range bindingVariables(s) {
					if variableDepends(input) {
						return true
					}
				}
			}
		}
		return false
	}
	return variableDepends(variable)
}

func activeQueryPlans(p platform.Page) map[string]bool {
	active := map[string]bool{}
	if p.ScopeVisibility.Root == "" {
		for id := range p.InputSchema.Queries {
			active[id] = true
		}
		return active
	}
	variables := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if variables[id] {
			return
		}
		variables[id] = true
		if q, ok := p.InputSchema.Queries[id]; ok {
			active[id] = true
			for _, source := range QueryVariables(q) {
				visit(source)
			}
			if q.Set != nil {
				for _, source := range q.Set.Inputs {
					visit(source)
				}
			}
		}
		if v, ok := p.InputSchema.Variables[id]; ok {
			if v.Source != nil {
				visit(v.Source.Query)
				if v.Source.Kind != "application" {
					visit(v.Source.Variable)
				}
				if v.Source.Compute != nil {
					visit(v.Source.Compute.RecordVariable)
				}
			}
			if v.Rule != nil {
				var scan func(platform.Expr)
				scan = func(e platform.Expr) {
					visit(e.Ref)
					if e.Agg != nil {
						visit(e.Agg.Source)
					}
					for _, a := range e.Args {
						scan(a)
					}
					if e.In != nil {
						scan(*e.In)
					}
				}
				scan(*v.Rule)
			}
		}
	}
	dormant := map[string]bool{}
	for _, scope := range p.ScopeVisibility.Scopes {
		if scope.Inactive {
			dormant[scope.Binding] = true
		}
	}
	for _, binding := range p.DataBindings {
		if dormant[binding.ID] {
			continue
		}
		for _, id := range binding.Ports {
			visit(id)
		}
		for _, resource := range binding.Resources {
			for _, input := range resource.Inputs {
				visit(input.Variable)
			}
			for _, id := range resource.Outputs {
				visit(id)
			}
		}
	}
	for _, scope := range p.ScopeVisibility.Scopes {
		if !scope.Inactive {
			for _, id := range scope.Bindings {
				visit(id)
			}
		}
	}
	for _, trigger := range p.ActionTriggers {
		for _, event := range trigger.Events {
			for _, id := range append(eventReads(event), eventVariables(event)...) {
				visit(id)
			}
		}
	}
	if p.InputSchema.Interface != nil {
		for _, ports := range []map[string]platform.PagePort{p.InputSchema.Interface.Inputs, p.InputSchema.Interface.Outputs} {
			for _, port := range ports {
				visit(port.Variable)
			}
		}
	}
	return active
}

func eventReads(b platform.PageEventBinding) []string {
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

func eventVariables(b platform.PageEventBinding) []string {
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

func bindingVariables(b platform.PageDataBinding) []string {
	out := []string{}
	for key, id := range b.Ports {
		if strings.HasSuffix(key, "Variable") {
			out = append(out, id)
		}
	}
	for _, r := range b.Resources {
		for _, v := range r.Inputs {
			out = append(out, v.Variable)
		}
		for _, id := range r.Outputs {
			out = append(out, id)
		}
	}
	return out
}

func literalType(raw json.RawMessage) string {
	if _, ok := ruleexpr.NumberLiteral(raw); ok {
		return "number"
	}
	if _, ok := ruleexpr.DecimalLiteral(raw); ok {
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
