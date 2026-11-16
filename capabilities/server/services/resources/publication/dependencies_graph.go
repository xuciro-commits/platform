package publication

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

type DependencyGraph struct {
	nodes       map[platform.AssetRef]platform.Dependencies
	usedBy      map[platform.AssetRef][]platform.AssetRef
	navigation  map[[2]platform.AssetRef]bool
	selfData    map[platform.AssetRef]bool
	containment map[[2]platform.AssetRef]bool
	objectViews map[[2]platform.AssetRef]bool
}

func NewDependencyGraph(descriptors []platform.Dependencies) (*DependencyGraph, error) {
	graph := &DependencyGraph{nodes: map[platform.AssetRef]platform.Dependencies{}, usedBy: map[platform.AssetRef][]platform.AssetRef{}, navigation: map[[2]platform.AssetRef]bool{}, selfData: map[platform.AssetRef]bool{}, containment: map[[2]platform.AssetRef]bool{}, objectViews: map[[2]platform.AssetRef]bool{}}
	for _, description := range descriptors {
		if err := description.Ref.Check(); err != nil {
			return nil, err
		}
		if _, exists := graph.nodes[description.Ref]; exists {
			return nil, fmt.Errorf("asset %s is declared twice", description.Ref)
		}
		description.Requires = slices.Clone(description.Requires)
		slices.SortFunc(description.Requires, platform.CompareAssetRef)
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
			if slices.ContainsFunc(description.Provides[:i], func(other platform.Capability) bool {
				return other.Kind == capability.Kind && other.Name == capability.Name
			}) {
				return nil, fmt.Errorf("%s repeats capability %s", description.Ref, capability.Name)
			}
		}
		graph.nodes[description.Ref] = description
		if description.Ref.Kind == platform.AssetObject {
			for _, ref := range description.Requires {
				if ref.App == description.Ref.App && ref.Kind == platform.AssetAction && strings.HasPrefix(ref.Name, description.Ref.Name+".") {
					graph.containment[[2]platform.AssetRef{description.Ref, ref}] = true
					graph.containment[[2]platform.AssetRef{ref, description.Ref}] = true
				}
			}
		}
	}
	for ref, owners := range graph.usedBy {
		slices.SortFunc(owners, platform.CompareAssetRef)
		graph.usedBy[ref] = slices.Compact(owners)
	}
	return graph, nil
}

func (resolver Resolver) NewReleaseDependencyGraph(assets []platform.ReleaseAsset) (*DependencyGraph, error) {
	descriptions := make([]platform.Dependencies, 0, len(assets))
	for _, asset := range assets {
		descriptions = append(descriptions, platform.Dependencies{Ref: asset.Ref, Requires: asset.Requires, Provides: asset.Provides, Version: asset.SourceVersion})
	}
	graph, err := NewDependencyGraph(descriptions)
	if err != nil {
		return nil, err
	}
	graph.containment = map[[2]platform.AssetRef]bool{}
	lookup := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	for _, asset := range assets {
		if asset.Ref.Kind == platform.AssetObject {
			for _, ref := range asset.Requires {
				if ref.Kind == platform.AssetFunction {
					var object struct {
						Actions []struct {
							Adoption *platform.ProposalAdoption `json:"adoption"`
						} `json:"actions"`
					}
					var function platform.AIFunction
					if json.Unmarshal(asset.Body, &object) == nil && json.Unmarshal(lookup[ref].Body, &function) == nil && function.Object == asset.Ref.Name && slices.ContainsFunc(object.Actions, func(action struct {
						Adoption *platform.ProposalAdoption `json:"adoption"`
					}) bool {
						return action.Adoption != nil && action.Adoption.Function.Name == ref.Name && (action.Adoption.Function.App == ref.App || action.Adoption.Function.App == "" && asset.Ref.App == ref.App)
					}) {
						graph.containment[[2]platform.AssetRef{asset.Ref, ref}] = true
						graph.containment[[2]platform.AssetRef{ref, asset.Ref}] = true
					}
				}
				if ref.App == asset.Ref.App && ref.Kind == platform.AssetAction {
					if target, ok := lookup[ref]; ok {
						action, err := ReleaseActionDescriptor(target.Body)
						if err == nil && action.Target == asset.Ref.Name {
							graph.containment[[2]platform.AssetRef{asset.Ref, ref}] = true
							graph.containment[[2]platform.AssetRef{ref, asset.Ref}] = true
						}
					}
				}
			}

			info, err := QueryObjectDescriptor(asset.Body)
			if err == nil {
				if view := info.ObjectView; view != nil && checkObjectView(asset.Ref, *view, lookup) == nil {
					graph.objectViews[[2]platform.AssetRef{asset.Ref, view.Application}] = true
					graph.objectViews[[2]platform.AssetRef{asset.Ref, view.Page}] = true
				}
				graph.selfData[asset.Ref] = slices.ContainsFunc(info.Fields, func(field platform.FieldInfo) bool {
					return field.Ref == asset.Ref.Name && (field.Type == "reference" || field.Type == "references")
				})
			}
		}
		if asset.Ref.Kind == platform.AssetPage {
			for _, ref := range asset.Requires {
				if resolver.PageNavigationEdge(asset, ref) {
					graph.navigation[[2]platform.AssetRef{asset.Ref, ref}] = true
				}
			}
		}
	}
	return graph, nil
}

func (g *DependencyGraph) Requires(ref platform.AssetRef) []platform.AssetRef {
	return slices.Clone(g.nodes[ref].Requires)
}

func (g *DependencyGraph) Closure(roots []platform.AssetRef) ([]platform.AssetRef, error) {
	refs, err := g.Reachable(roots)
	if err != nil {
		return nil, err
	}
	if _, err := g.walk(refs, false); err != nil {
		return nil, err
	}
	return refs, nil
}

func (g *DependencyGraph) InstallationOrder() ([]platform.AssetRef, error) {
	roots := make([]platform.AssetRef, 0, len(g.nodes))
	for ref := range g.nodes {
		roots = append(roots, ref)
	}
	slices.SortFunc(roots, platform.CompareAssetRef)
	return g.walk(roots, true)
}

func (g *DependencyGraph) walk(roots []platform.AssetRef, installedPrerequisites bool) ([]platform.AssetRef, error) {
	visited, visiting := map[platform.AssetRef]bool{}, map[platform.AssetRef]bool{}
	var path, out []platform.AssetRef
	var visit func(platform.AssetRef, platform.AssetRef) error
	visit = func(ref, from platform.AssetRef) error {
		if err := ref.Check(); err != nil {
			return err
		}
		if visiting[ref] {
			if start := slices.Index(path, ref); start >= 0 && slices.ContainsFunc(path[start:], func(node platform.AssetRef) bool { return node.Kind == platform.AssetLinkType }) && !slices.ContainsFunc(path[start:], func(node platform.AssetRef) bool {
				return node.Kind != platform.AssetObject && node.Kind != platform.AssetLinkType
			}) {
				return nil
			}
			if start := slices.Index(path, ref); start >= 0 && len(path)-start == 2 && g.containment[[2]platform.AssetRef{from, ref}] {
				return nil
			}
			if from.Kind == platform.AssetObject && ref.Kind == platform.AssetObject && from != ref {
				return nil
			}
			if from == ref && g.selfData[ref] {
				return nil
			}
			if g.navigation[[2]platform.AssetRef{from, ref}] {
				start := slices.Index(path, ref)
				navigation := start >= 0
				for i := start; i >= 0 && i+1 < len(path); i++ {
					navigation = navigation && g.navigation[[2]platform.AssetRef{path[i], path[i+1]}]
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
			if g.objectViews[[2]platform.AssetRef{ref, dependency}] || installedPrerequisites && ref.Kind == platform.AssetObject && dependency.Kind == platform.AssetFunction && g.containment[[2]platform.AssetRef{ref, dependency}] {
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
		if err := visit(root, platform.AssetRef{}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (g *DependencyGraph) Reachable(roots []platform.AssetRef) ([]platform.AssetRef, error) {
	seen := map[platform.AssetRef]bool{}
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
	out := make([]platform.AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, platform.CompareAssetRef)
	return out, nil
}

func (g *DependencyGraph) CheckExecutionBindings(asset platform.ReleaseAsset, assets map[platform.AssetRef]platform.ReleaseAsset) error {
	if asset.Ref.Kind == platform.AssetApp {
		return nil
	}
	for _, dependency := range asset.Requires {
		if dependency.Kind != platform.AssetAction || dependency.App == asset.Ref.App {
			continue
		}
		if asset.Ref.Kind == platform.AssetPage || asset.Ref.Kind == platform.AssetObservation {
			continue
		}
		bound := false
		for _, protocolRef := range asset.Requires {
			if protocolRef.Kind != platform.AssetProtocol || protocolRef.App != dependency.App {
				continue
			}
			var protocol platform.ProtocolReleaseDescriptor
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

func (g *DependencyGraph) Impact(changed []platform.AssetRef) ([]platform.AssetRef, error) {
	seen := map[platform.AssetRef]bool{}
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
	out := make([]platform.AssetRef, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	slices.SortFunc(out, platform.CompareAssetRef)
	return out, nil
}

func (g *DependencyGraph) CheckReadBindings(asset platform.ReleaseAsset) error {
	if asset.Ref.Kind != platform.AssetPage {
		return nil
	}
	var page platform.Page
	if err := json.Unmarshal(asset.Body, &page); err != nil {
		return err
	}
	var reads []platform.AssetRef
	if page.Object.Name != "" {
		reads = append(reads, page.Object)
	}
	reads = append(reads, page.QueryReferences()...)
	for _, compute := range page.ComputeResources() {
		reads = append(reads, compute.Operation.Ref)
	}
	for _, ref := range reads {
		if ref.Kind == platform.AssetAction || ref.Kind == platform.AssetFlow || ref.Kind == platform.AssetFunction || ref.Kind == platform.AssetWriteback || ref.Kind == platform.AssetSource || ref.Kind == platform.AssetPipeline {
			return fmt.Errorf("%s read binding %s is not pure", asset.Ref, ref)
		}
		capabilities := g.nodes[ref].Provides
		if ref.Kind == platform.AssetObservation {
			if !slices.ContainsFunc(capabilities, func(capability platform.Capability) bool {
				return capability.Kind == "query" && capability.Name == ref.Name && capability.Side == platform.CapabilityPure
			}) {
				return fmt.Errorf("%s read binding %s has no pure observation query", asset.Ref, ref)
			}
			continue
		}
		for _, capability := range capabilities {
			if capability.Side != platform.CapabilityPure {
				return fmt.Errorf("%s read binding %s provides %s capability %s", asset.Ref, ref, capability.Side, capability.Name)
			}
		}
	}
	return nil
}
