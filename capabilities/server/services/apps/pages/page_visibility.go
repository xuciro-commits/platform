package pages

import (
	"maps"
	"strings"

	"platformserver/platform"
)

func withSafetyProjection(p platform.Page, projected platform.Page) platform.Page {
	result := projected
	result.WidgetSpec = p.WidgetSpec
	result.RetainedVersion = p.RetainedVersion
	return result
}

func visibleSafety(p platform.Page) platform.Page {
	result := p
	result.DataBindings = append([]platform.PageDataBinding{}, p.DataBindings...)
	result.InputSchema.Variables = maps.Clone(p.InputSchema.Variables)
	result.InputSchema.Queries = maps.Clone(p.InputSchema.Queries)
	scopes := map[string]platform.PageScope{}
	for _, s := range p.ScopeVisibility.Scopes {
		scopes[s.ID] = s
	}
	for changed := true; changed; {
		changed = false
		allowed := map[string]bool{}
		for _, b := range result.DataBindings {
			allowed[b.ID] = true
		}
		for id, v := range result.InputSchema.Variables {
			missing := false
			if src := v.Source; src != nil {
				missing = src.Section != "" && !allowed[src.Section]
				if src.Variable != "" && src.Kind != "application" {
					_, ok := result.InputSchema.Variables[src.Variable]
					missing = missing || !ok
				}
				if src.Query != "" {
					_, ok := result.InputSchema.Queries[src.Query]
					missing = missing || !ok
				}
				if src.Compute != nil {
					_, ok := result.InputSchema.Variables[src.Compute.RecordVariable]
					missing = missing || !ok
				}
			}
			if v.Scope == "loop-item" {
				scope, ok := scopes[v.Owner]
				_, input := result.InputSchema.Variables[scope.Bindings["collection"]]
				missing = missing || !ok || !input
			}
			var reads func(platform.Expr)
			reads = func(e platform.Expr) {
				if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") {
					_, ok := result.InputSchema.Variables[e.Ref]
					missing = missing || !ok
				}
				if e.Agg != nil {
					_, ok := result.InputSchema.Variables[e.Agg.Source]
					missing = missing || !ok
				}
				for _, a := range e.Args {
					reads(a)
				}
				if e.In != nil {
					reads(*e.In)
				}
			}
			if v.Rule != nil {
				reads(*v.Rule)
			}
			if missing {
				delete(result.InputSchema.Variables, id)
				changed = true
			}
		}
		for id, q := range result.InputSchema.Queries {
			missing := q.MissingSetInput(result.InputSchema.Queries)
			for _, v := range q.Variables() {
				_, ok := result.InputSchema.Variables[v]
				missing = missing || !ok
			}
			if missing {
				delete(result.InputSchema.Queries, id)
				changed = true
			}
		}
		bindings := result.DataBindings[:0]
		for _, b := range result.DataBindings {
			valid := true
			for key, id := range b.Ports {
				if strings.HasSuffix(key, "Variable") && id != "" {
					_, ok := result.InputSchema.Variables[id]
					valid = valid && ok
				}
			}
			if valid {
				bindings = append(bindings, b)
			} else {
				changed = true
			}
		}
		result.DataBindings = bindings
	}
	allowed := map[string]bool{}
	for _, b := range result.DataBindings {
		allowed[b.ID] = true
	}
	kept := map[string]bool{p.ScopeVisibility.Root: true}
	for _, s := range p.ScopeVisibility.Scopes {
		valid := s.Binding == "" || allowed[s.Binding]
		for _, key := range []string{"valueVariable", "activeVariable", "visibleWhen", "enabledWhen", "collection", "itemVariable", "openVariable"} {
			if id := s.Bindings[key]; id != "" {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
		}
		if valid {
			kept[s.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range kept {
			if parent := scopes[id].Parent; parent != "" && !kept[parent] {
				delete(kept, id)
				changed = true
			}
		}
	}
	result.ScopeVisibility.Scopes = nil
	result.ScopeVisibility.Owners = map[string]string{}
	for _, s := range p.ScopeVisibility.Scopes {
		if kept[s.ID] {
			result.ScopeVisibility.Scopes = append(result.ScopeVisibility.Scopes, s)
			if s.Binding != "" {
				result.ScopeVisibility.Owners[s.Binding] = s.ID
			}
		}
	}
	result.ActionTriggers = nil
	for _, trigger := range p.ActionTriggers {
		if trigger.Binding != "page" && !allowed[trigger.Binding] && !kept[trigger.Binding] {
			continue
		}
		copy := trigger
		copy.Events = nil
		for _, event := range trigger.Events {
			valid := true
			for _, id := range append(event.Variables(), event.Reads()...) {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
			if valid {
				copy.Events = append(copy.Events, event)
			}
		}
		result.ActionTriggers = append(result.ActionTriggers, copy)
	}
	if iface := p.InputSchema.Interface; iface != nil {
		copy := *iface
		copy.Inputs = maps.Clone(iface.Inputs)
		copy.Outputs = maps.Clone(iface.Outputs)
		for _, ports := range []map[string]platform.PagePort{copy.Inputs, copy.Outputs} {
			for id, port := range ports {
				if _, ok := result.InputSchema.Variables[port.Variable]; !ok {
					delete(ports, id)
				}
			}
		}
		result.InputSchema.Interface = &copy
	}
	return withSafetyProjection(p, result)
}
