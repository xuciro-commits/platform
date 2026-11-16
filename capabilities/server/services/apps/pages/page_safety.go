package pages

import (
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func CheckSafety(p platform.Page) error {
	safe, err := Safety(p)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, binding := range safe.DataBindings {
		if !platform.PageNodeID.MatchString(binding.ID) || ids[binding.ID] {
			return fmt.Errorf("page data bindings need unique stable IDs")
		}
		ids[binding.ID] = true
	}
	scopes := map[string]bool{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if !slices.Contains([]string{"page", "application", "overlay", "loop"}, scope.Kind) {
			return fmt.Errorf("unknown page scope kind")
		}
		if !platform.PageNodeID.MatchString(scope.ID) || scopes[scope.ID] {
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
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > platform.PageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range platform.PageBindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && safe.ScopeOwner(owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && safe.ScopeOwner(owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := safe.CheckWidgetPorts(binding, owner); err != nil {
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
	if err := safe.CheckEnvelope(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckProfile(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := safe.CheckQueries("page"); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckInterface(); err != nil {
		return err
	}
	if safe.Object.Name != "" {
		if err := checkRecordPorts(safe); err != nil {
			return err
		}
	}
	return safe.CheckEvents()
}

func PublicationResolver() publication.Resolver {
	return publication.Resolver{
		CheckPage: CheckSafety, CheckFrozenQueries: CheckFrozenQueries,
		Safety: Safety, PageDependencies: PageDependencies, PageNavigationEdge: PageNavigationEdge,
		CheckPageEmbeddingGraph: CheckPageEmbeddingGraph, CheckPageVariables: CheckPageVariables,
		PageContentVersion: PageContentVersion,
	}
}
