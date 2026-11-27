package pages

import (
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/ontology"
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
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range bindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && scopeOwner(safe, owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && scopeOwner(safe, owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := checkWidgetPorts(safe, binding, owner); err != nil {
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
	if err := checkProfile(safe.InputSchema); err != nil {
		return err
	}
	if err := CheckVariables(safe.InputSchema); err != nil {
		return err
	}
	if err := checkQueries(safe, "page"); err != nil {
		return err
	}
	if err := checkPageInterface(safe.InputSchema); err != nil {
		return err
	}
	if safe.Object.Name != "" {
		if err := checkRecordPorts(safe); err != nil {
			return err
		}
	}
	return checkEvents(safe)
}

var publicationDescribes = publication.NewDescribeCache()

func PublicationResolver() publication.Resolver {
	return publication.Resolver{
		Describes: publicationDescribes,
		CheckPage: CheckSafety, CheckFrozenQueries: CheckFrozenQueries,
		Safety: Safety, PageDependencies: PageDependencies, PageNavigationEdge: PageNavigationEdge,
		PageQueryReferences: QueryReferences, PageComputeResources: computeResources, PageEventNavigation: eventNavigation,
		CheckPageEmbeddingGraph: CheckPageEmbeddingGraph, CheckPageVariables: CheckPageVariables,
		PageContentVersion: PageContentVersion, CheckPageContentVersion: CheckPageContentVersion,
		CheckPageNavigation:       CheckPageNavigation,
		ApplicationPage:           ApplicationPage,
		CheckApplicationProfile:   CheckApplicationProfile,
		CheckApplicationVariables: CheckApplicationVariables,
		CheckApplicationResources: CheckApplicationResources, CheckApplicationExtension: CheckApplicationExtension,
		ApplicationDependencies: ApplicationDependencies,
		ObjectDescriptor:        ontology.ObjectDescriptor,
		ActionDescriptor:        ontology.ActionDescriptor,
		ObjectDependencies:      ontology.ObjectDependencies,
		ActionDependencies:      ontology.ActionDependencies,
	}
}
