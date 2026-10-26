package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// DependencyGraph is an index of the same sealed descriptions. Forward and
// reverse queries never reread mutable owner configuration.
type DependencyGraph struct {
	nodes       map[AssetRef]Dependencies
	usedBy      map[AssetRef][]AssetRef
	navigation  map[[2]AssetRef]bool
	selfData    map[AssetRef]bool
	containment map[[2]AssetRef]bool
}

func NewDependencyGraph(descriptors []Dependencies) (*DependencyGraph, error) {
	graph := &DependencyGraph{nodes: map[AssetRef]Dependencies{}, usedBy: map[AssetRef][]AssetRef{}, navigation: map[[2]AssetRef]bool{}, selfData: map[AssetRef]bool{}, containment: map[[2]AssetRef]bool{}}
	for _, description := range descriptors {
		if err := description.Ref.Check(); err != nil {
			return nil, err
		}
		if _, exists := graph.nodes[description.Ref]; exists {
			return nil, fmt.Errorf("asset %s is declared twice", description.Ref)
		}
		description.Requires = slices.Clone(description.Requires)
		slices.SortFunc(description.Requires, compareRef)
		for i, ref := range description.Requires {
			if err := ref.Check(); err != nil {
				return nil, fmt.Errorf("%s: %w", description.Ref, err)
			}
			if i > 0 && ref == description.Requires[i-1] {
				return nil, fmt.Errorf("%s repeats dependency %s", description.Ref, ref)
			}
			graph.usedBy[ref] = append(graph.usedBy[ref], description.Ref)
		}
		for i, capability := range description.Provides {
			if err := capability.Check(); err != nil {
				return nil, fmt.Errorf("%s: %w", description.Ref, err)
			}
			if slices.ContainsFunc(description.Provides[:i], func(other Capability) bool { return other.Kind == capability.Kind && other.Name == capability.Name }) {
				return nil, fmt.Errorf("%s repeats capability %s", description.Ref, capability.Name)
			}
		}
		graph.nodes[description.Ref] = description
		if description.Ref.Kind == AssetObject {
			for _, ref := range description.Requires {
				if ref.App == description.Ref.App && ref.Kind == AssetAction && strings.HasPrefix(ref.Name, description.Ref.Name+".") {
					graph.containment[[2]AssetRef{description.Ref, ref}] = true
					graph.containment[[2]AssetRef{ref, description.Ref}] = true
				}
			}
		}
	}
	for ref, owners := range graph.usedBy {
		slices.SortFunc(owners, compareRef)
		graph.usedBy[ref] = slices.Compact(owners)
	}
	return graph, nil
}

// NewReleaseDependencyGraph preserves the original allowances for reciprocal
// data references and page-only navigation. Execution cycles remain invalid.
func NewReleaseDependencyGraph(assets []ReleaseAsset) (*DependencyGraph, error) {
	descriptions := make([]Dependencies, 0, len(assets))
	for _, asset := range assets {
		descriptions = append(descriptions, Dependencies{Ref: asset.Ref, Requires: asset.Requires, Provides: asset.Provides, Version: asset.SourceVersion})
	}
	graph, err := NewDependencyGraph(descriptions)
	if err != nil {
		return nil, err
	}
	graph.containment = map[[2]AssetRef]bool{}
	lookup := map[AssetRef]ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	for _, asset := range assets {
		if asset.Ref.Kind == AssetObject {
			for _, ref := range asset.Requires {
				if ref.Kind == AssetFunction {
					// A proposal's source model contains the action that adopts
					// its result. This describes data and its explicit later
					// submission; it cannot recursively invoke the model.
					var object struct {
						Actions []struct {
							Adoption *ProposalAdoption `json:"adoption"`
						} `json:"actions"`
					}
					var function AIFunction
					if json.Unmarshal(asset.Body, &object) == nil && json.Unmarshal(lookup[ref].Body, &function) == nil && function.Object == asset.Ref.Name && slices.ContainsFunc(object.Actions, func(action struct {
						Adoption *ProposalAdoption `json:"adoption"`
					}) bool {
						return action.Adoption != nil && action.Adoption.Function.Name == ref.Name && (action.Adoption.Function.App == ref.App || action.Adoption.Function.App == "" && asset.Ref.App == ref.App)
					}) {
						graph.containment[[2]AssetRef{asset.Ref, ref}] = true
						graph.containment[[2]AssetRef{ref, asset.Ref}] = true
					}
				}
				if ref.App == asset.Ref.App && ref.Kind == AssetAction {
					if target, ok := lookup[ref]; ok {
						action, err := releaseActionDescriptor(target.Body)
						if err == nil && action.Target == asset.Ref.Name {
							graph.containment[[2]AssetRef{asset.Ref, ref}] = true
							graph.containment[[2]AssetRef{ref, asset.Ref}] = true
						}
					}
				}
			}

			info, err := queryObjectDescriptor(asset.Body)
			if err == nil {
				graph.selfData[asset.Ref] = slices.ContainsFunc(info.Fields, func(field FieldInfo) bool {
					return field.Ref == asset.Ref.Name && (field.Type == "reference" || field.Type == "references")
				})
			}
		}
		if asset.Ref.Kind == AssetPage {
			for _, ref := range asset.Requires {
				if pageNavigationEdge(asset, ref) {
					graph.navigation[[2]AssetRef{asset.Ref, ref}] = true
				}
			}
		}
	}
	return graph, nil
}

func (g *DependencyGraph) Descriptors() []Dependencies {
	refs := make([]AssetRef, 0, len(g.nodes))
	for ref := range g.nodes {
		refs = append(refs, ref)
	}
	slices.SortFunc(refs, compareRef)
	out := make([]Dependencies, 0, len(refs))
	for _, ref := range refs {
		description := g.nodes[ref]
		description.Requires = slices.Clone(description.Requires)
		description.Provides = slices.Clone(description.Provides)
		out = append(out, description)
	}
	return out
}

func (g *DependencyGraph) Requires(ref AssetRef) []AssetRef {
	return slices.Clone(g.nodes[ref].Requires)
}
func (g *DependencyGraph) UsedBy(ref AssetRef) []AssetRef { return slices.Clone(g.usedBy[ref]) }

func (g *DependencyGraph) Closure(roots []AssetRef) ([]AssetRef, error) {
	refs, err := g.walk(roots, false)
	slices.SortFunc(refs, compareRef)
	return refs, err
}

// InstallationOrder orders the owner's part of a previously checked release.
// Foreign prerequisites are installed by their owners; semantic data cycles
// use the same allowances as the complete candidate.
func (g *DependencyGraph) InstallationOrder() ([]AssetRef, error) {
	roots := make([]AssetRef, 0, len(g.nodes))
	for ref := range g.nodes {
		roots = append(roots, ref)
	}
	slices.SortFunc(roots, compareRef)
	return g.walk(roots, true)
}

func (g *DependencyGraph) walk(roots []AssetRef, installedPrerequisites bool) ([]AssetRef, error) {
	visited, visiting := map[AssetRef]bool{}, map[AssetRef]bool{}
	var path, out []AssetRef
	var visit func(AssetRef, AssetRef) error
	visit = func(ref, from AssetRef) error {
		if err := ref.Check(); err != nil {
			return err
		}
		if visiting[ref] {
			// Object collections and typed relationships describe a data model.
			// A reciprocal model may include its own link type; no executable
			// asset may participate in this allowance.
			if start := slices.Index(path, ref); start >= 0 && slices.ContainsFunc(path[start:], func(node AssetRef) bool { return node.Kind == AssetLinkType }) && !slices.ContainsFunc(path[start:], func(node AssetRef) bool { return node.Kind != AssetObject && node.Kind != AssetLinkType }) {
				return nil
			}
			if start := slices.Index(path, ref); start >= 0 && len(path)-start == 2 && g.containment[[2]AssetRef{from, ref}] {
				return nil
			}
			if from.Kind == AssetObject && ref.Kind == AssetObject && from != ref {
				return nil
			}
			if from == ref && g.selfData[ref] {
				return nil
			}
			if g.navigation[[2]AssetRef{from, ref}] {
				start := slices.Index(path, ref)
				navigation := start >= 0
				for i := start; i >= 0 && i+1 < len(path); i++ {
					navigation = navigation && g.navigation[[2]AssetRef{path[i], path[i+1]}]
				}
				if navigation {
					return nil
				}
			}
			parts := make([]string, 0, len(path)+1)
			for _, part := range append(slices.Clone(path), ref) {
				parts = append(parts, part.String())
			}
			return fmt.Errorf("release dependency cycle: %s", strings.Join(parts, " -> "))
		}
		if visited[ref] {
			return nil
		}
		node, exists := g.nodes[ref]
		if !exists {
			if installedPrerequisites {
				return nil
			}
			if from.Name != "" {
				return fmt.Errorf("%s requires missing asset %s", from, ref)
			}
			return fmt.Errorf("release requires missing asset %s", ref)
		}
		visiting[ref] = true
		path = append(path, ref)
		for _, dependency := range node.Requires {
			// Adoption retains a function in the closure, but its source model
			// must exist before the function is installed. Installation visits
			// every node as a root; closure traversal still follows this edge.
			if installedPrerequisites && ref.Kind == AssetObject && dependency.Kind == AssetFunction && g.containment[[2]AssetRef{ref, dependency}] {
				continue
			}
			if err := visit(dependency, ref); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		delete(visiting, ref)
		visited[ref] = true
		out = append(out, ref)
		return nil
	}
	for _, root := range roots {
		if err := visit(root, AssetRef{}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Reachable reads an already installed graph. Its cycles were checked by
// publication with full owner bodies, which are intentionally absent here.
func (g *DependencyGraph) Reachable(roots []AssetRef) ([]AssetRef, error) {
	seen := map[AssetRef]bool{}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if err := ref.Check(); err != nil {
			return nil, err
		}
		if seen[ref] {
			continue
		}
		node, exists := g.nodes[ref]
		if !exists {
			return nil, fmt.Errorf("dependency requires missing asset %s", ref)
		}
		seen[ref] = true
		queue = append(queue, node.Requires...)
	}
	out := make([]AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, compareRef)
	return out, nil
}

// CheckExecutionBindings keeps foreign authoritative calls on the original
// protocol boundary. Structural model and read edges do not invoke actions.
func (g *DependencyGraph) CheckExecutionBindings(asset ReleaseAsset, assets map[AssetRef]ReleaseAsset) error {
	if asset.Ref.Kind == AssetApp {
		return nil // application membership references original owners; it invokes no action
	}
	for _, dependency := range asset.Requires {
		if dependency.Kind != AssetAction || dependency.App == asset.Ref.App {
			continue
		}
		if asset.Ref.Kind == AssetPage || asset.Ref.Kind == AssetObservation {
			continue
		} // explicit page/observation actions are independent authorized user submissions
		bound := false
		for _, protocolRef := range asset.Requires {
			if protocolRef.Kind != AssetProtocol || protocolRef.App != dependency.App {
				continue
			}
			var protocol ProtocolReleaseDescriptor
			if frozen, ok := assets[protocolRef]; ok && json.Unmarshal(frozen.Body, &protocol) == nil {
				for _, action := range protocol.ActionBindings {
					if action == dependency {
						bound = true
						break
					}
				}
			}
		}
		if !bound {
			return fmt.Errorf("%s has undeclared cross-application action %s; bind its provider protocol", asset.Ref, dependency)
		}
	}
	return nil
}

// Impact is the reverse transitive closure, including the changed roots. It
// also works when a removed dependency is no longer an installed node.
func (g *DependencyGraph) Impact(changed []AssetRef) ([]AssetRef, error) {
	seen := map[AssetRef]bool{}
	queue := slices.Clone(changed)
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if err := ref.Check(); err != nil {
			return nil, err
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		queue = append(queue, g.usedBy[ref]...)
	}
	out := make([]AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, compareRef)
	return out, nil
}

// CheckReadBindings applies Side to page data resources. Explicit action event
// effects and AI request widgets remain governed submissions, not read bindings.
func (g *DependencyGraph) CheckReadBindings(asset ReleaseAsset) error {
	if asset.Ref.Kind != AssetPage {
		return nil
	}
	var page Page
	if err := json.Unmarshal(asset.Body, &page); err != nil {
		return err
	}
	var reads []AssetRef
	if page.Object.Name != "" {
		reads = append(reads, page.Object)
	}
	reads = append(reads, page.QueryReferences()...)
	for _, compute := range page.ComputeResources() {
		reads = append(reads, compute.Operation.Ref)
	}
	for _, section := range page.Sections {
		if section.Operation != nil {
			reads = append(reads, section.Operation.Ref)
		}
		if section.Query.Name != "" {
			reads = append(reads, section.Query)
		}
	}
	for _, ref := range reads {
		if ref.Kind == AssetAction || ref.Kind == AssetFlow || ref.Kind == AssetFunction || ref.Kind == AssetWriteback || ref.Kind == AssetSource || ref.Kind == AssetPipeline {
			return fmt.Errorf("%s read binding %s is not pure", asset.Ref, ref)
		}
		capabilities := g.nodes[ref].Provides
		if ref.Kind == AssetObservation {
			if !slices.ContainsFunc(capabilities, func(capability Capability) bool {
				return capability.Kind == "query" && capability.Name == ref.Name && capability.Side == CapabilityPure
			}) {
				return fmt.Errorf("%s read binding %s has no pure observation query", asset.Ref, ref)
			}
			continue
		}
		for _, capability := range capabilities {
			if capability.Side != CapabilityPure {
				return fmt.Errorf("%s read binding %s provides %s capability %s", asset.Ref, ref, capability.Side, capability.Name)
			}
		}
	}
	return nil
}
