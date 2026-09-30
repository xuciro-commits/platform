package build

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

// checkProcessScopes checks one graph, one binding grammar and explicit scope
// boundaries. Layout/React Flow edge JSON is never an execution definition.
func checkProcessScopes(p Process) error {
	nodes := map[string]ProcessStep{}
	for _, node := range p.Steps {
		nodes[node.Name] = node
	}
	scope := map[string]string{}
	visited := map[string]bool{}
	var assign func(string, string) error
	assign = func(name, current string) error {
		if name == "" {
			return nil
		}
		node := nodes[name]
		if prior, ok := scope[name]; ok && prior != current {
			if (node.Kind == "end" || node.Kind == "join") && node.Value == nil && len(node.Inputs) == 0 {
				return nil
			}
			return fmt.Errorf("Node %s: paths cross distinct iteration/parallel scopes; finish that scope before joining", name)
		}
		scope[name] = current
		key := current + ":" + name
		if visited[key] {
			return nil
		}
		visited[key] = true
		if node.Kind == "foreach" || node.Kind == "while" {
			if err := assign(node.Body, current+"/loop:"+name); err != nil {
				return err
			}
		}
		if node.Kind == "fork" {
			for _, branch := range node.Branches {
				if err := assign(branch, current+"/fork:"+name+":"+branch); err != nil {
					return err
				}
			}
		}
		for _, next := range append([]string{node.Next, node.Error}, slices.Collect(maps.Values(node.Cases))...) {
			if err := assign(next, current); err != nil {
				return err
			}
		}
		return nil
	}
	if err := assign(p.Steps[0].Name, ""); err != nil {
		return err
	}
	// Dominators are based on control reachability, not visual position or
	// declaration order. Only values produced on every incoming route exist.
	predecessors := map[string][]string{}
	for _, node := range p.Steps {
		for _, to := range processPaths(node) {
			if to != "" {
				predecessors[to] = append(predecessors[to], node.Name)
			}
		}
	}
	dominators := map[string]map[string]bool{}
	for _, node := range p.Steps {
		set := map[string]bool{}
		if node.Name == p.Steps[0].Name {
			set[node.Name] = true
		} else {
			for name := range nodes {
				set[name] = true
			}
		}
		dominators[node.Name] = set
	}
	for changed := true; changed; {
		changed = false
		for _, node := range p.Steps[1:] {
			var set map[string]bool
			for _, from := range predecessors[node.Name] {
				if set == nil {
					set = maps.Clone(dominators[from])
				} else {
					for name := range set {
						if !dominators[from][name] {
							delete(set, name)
						}
					}
				}
			}
			if set == nil {
				set = map[string]bool{}
			}
			set[node.Name] = true
			if !maps.Equal(set, dominators[node.Name]) {
				dominators[node.Name] = set
				changed = true
			}
		}
	}
	for _, node := range p.Steps {
		if (node.Kind == "break" || node.Kind == "continue") && !strings.Contains(scope[node.Name], "/loop:") {
			return fmt.Errorf("Node %s: loop control requires an enclosing iteration scope", node.Name)
		}
		bindings := slices.Collect(maps.Values(node.Inputs))
		for _, binding := range []*platform.Binding{node.Value, node.Target, node.Collection} {
			if binding != nil {
				bindings = append(bindings, *binding)
			}
		}
		if node.Condition != nil {
			bindings = append(bindings, predicateBindings(*node.Condition)...)
		}
		for _, binding := range bindings {
			if binding.Source == "item" || binding.Source == "index" {
				if !strings.Contains(scope[node.Name], "/loop:") && node.Kind != "while" {
					return fmt.Errorf("Node %s: %s is available only inside a loop scope", node.Name, binding.Source)
				}
			}
			if binding.Source == "step" {
				if binding.Step == node.Name || !dominators[node.Name][binding.Step] {
					return fmt.Errorf("Node %s: output %s is not guaranteed by its upstream control path", node.Name, binding.Step)
				}
				if !strings.HasPrefix(scope[node.Name], scope[binding.Step]) {
					return fmt.Errorf("Node %s: output %s belongs to another scope; bind its loop/fork result", node.Name, binding.Step)
				}
				if strings.Contains(scope[node.Name], "/loop:"+binding.Step) {
					return fmt.Errorf("Node %s: loop %s has not completed inside its own body", node.Name, binding.Step)
				}
			}
		}
	}
	return nil
}
func predicateBindings(p platform.Predicate) []platform.Binding {
	var out []platform.Binding
	if p.Left != nil {
		out = append(out, *p.Left)
	}
	if p.Right != nil {
		out = append(out, *p.Right)
	}
	for _, term := range p.Terms {
		out = append(out, predicateBindings(term)...)
	}
	return out
}
