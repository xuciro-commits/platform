package platform

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

func (p Page) Safety() (Page, error) {
	if p.Format != 4 {
		return Page{}, fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.retainedVersion == "" {
		version, err := PageContentVersion(p)
		if err != nil {
			return Page{}, err
		}
		p.retainedVersion = version
	}
	return p, nil
}

func (p Page) WithSafetyProjection(projected Page) Page {
	result := projected
	result.WidgetSpec = p.WidgetSpec
	result.retainedVersion = p.retainedVersion
	return result
}

func (p Page) VisibleSafety() Page {
	result := p
	result.DataBindings = append([]PageDataBinding{}, p.DataBindings...)
	result.InputSchema.Variables = maps.Clone(p.InputSchema.Variables)
	result.InputSchema.Queries = maps.Clone(p.InputSchema.Queries)
	scopes := map[string]PageScope{}
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
			var reads func(Expr)
			reads = func(e Expr) {
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
		for _, ports := range []map[string]PagePort{copy.Inputs, copy.Outputs} {
			for id, port := range ports {
				if _, ok := result.InputSchema.Variables[port.Variable]; !ok {
					delete(ports, id)
				}
			}
		}
		result.InputSchema.Interface = &copy
	}
	return p.WithSafetyProjection(result)
}

func (p Page) CheckSafety() error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, binding := range safe.DataBindings {
		if !pageNodeID.MatchString(binding.ID) || ids[binding.ID] {
			return fmt.Errorf("page data bindings need unique stable IDs")
		}
		ids[binding.ID] = true
	}
	scopes := map[string]bool{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if !slices.Contains([]string{"page", "application", "overlay", "loop"}, scope.Kind) {
			return fmt.Errorf("unknown page scope kind")
		}
		if !pageNodeID.MatchString(scope.ID) || scopes[scope.ID] {
			return fmt.Errorf("page scopes need unique stable IDs")
		}
		scopes[scope.ID] = true
		if scope.Binding != "" && !ids[scope.Binding] {
			return fmt.Errorf("page scope binding is unavailable")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if scope.Parent != "" && !scopes[scope.Parent] {
			return fmt.Errorf("page scope parent is unavailable")
		}
		if scope.ID == safe.ScopeVisibility.Root && scope.Parent != "" {
			return fmt.Errorf("the page root scope has no parent")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		seen := map[string]bool{}
		owner := scope.ID
		for owner != "" {
			if seen[owner] {
				return fmt.Errorf("page scope graph contains a cycle")
			}
			seen[owner] = true
			next := ""
			for _, parent := range safe.ScopeVisibility.Scopes {
				if parent.ID == owner {
					next = parent.Parent
					break
				}
			}
			owner = next
		}
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range pageBindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && safe.scopeOwner(owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && safe.scopeOwner(owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := safe.checkWidgetPorts(binding, owner); err != nil {
			return err
		}
	}
	triggers := map[string]bool{}
	for _, trigger := range safe.ActionTriggers {
		if trigger.ID == "" || triggers[trigger.ID] || trigger.Binding != "page" && !ids[trigger.Binding] && !scopes[trigger.Binding] {
			return fmt.Errorf("page trigger needs a unique ID and original owner")
		}
		triggers[trigger.ID] = true
	}
	if err := safe.checkEnvelope(); err != nil {
		return err
	}
	if err := safe.InputSchema.checkProfile(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := safe.checkQueries("page"); err != nil {
		return err
	}
	if err := safe.InputSchema.checkInterface(); err != nil {
		return err
	}
	if safe.Object.Name != "" {
		if err := safe.CheckRecordPorts(); err != nil {
			return err
		}
	}
	return safe.checkEvents()
}
