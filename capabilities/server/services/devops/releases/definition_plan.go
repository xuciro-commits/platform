package releases

import (
	"platformserver/platform"
	"platformserver/services/resources/publication"
	"slices"
	"sync"
)

type DefinitionPlanKey struct {
	App, Action, Release string
	Family               uint32
}

type DefinitionPlanCache struct {
	mu                    sync.Mutex
	generation, resources uint64
	plans                 map[DefinitionPlanKey]platform.DecisionDefinition
}

func NewDefinitionPlanCache(generation, resources uint64) *DefinitionPlanCache {
	return &DefinitionPlanCache{generation: generation, resources: resources}
}

func (c *DefinitionPlanCache) Lookup(generation, resources uint64, key DefinitionPlanKey, build func() (*platform.DecisionDefinition, error)) (*platform.DecisionDefinition, error) {
	if c == nil || c.generation != generation || c.resources != resources {
		return build()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if saved, ok := c.plans[key]; ok {
		return &saved, nil
	}
	definition, err := build()
	if err == nil && definition != nil && definition.DependenciesState == "closed" {
		if c.plans == nil {
			c.plans = map[DefinitionPlanKey]platform.DecisionDefinition{}
		}
		c.plans[key] = *definition
	}
	return definition, err
}

func DecisionDefinition(app, appVersion string, action platform.Action, release string, contract platform.SchemaVersion, registry *platform.AssetRegistry, store Store, retained ...[]platform.ReleaseAsset) (*platform.DecisionDefinition, error) {
	root := platform.AssetRef{App: app, Kind: platform.AssetAction, Name: action.Schema}
	out := &platform.DecisionDefinition{
		Definition:        platform.AssetBinding{Ref: root, SourceVersion: appVersion},
		Contract:          contract,
		Release:           release,
		DependenciesState: "unavailable",
		Dependencies:      []platform.AssetBinding{},
		Applications:      []platform.AssetBinding{},
	}
	if registry == nil {
		return out, nil
	}
	var available []platform.ReleaseAsset
	var err error
	if len(retained) > 0 {
		available = slices.Clone(retained[0])
	} else if available, err = registry.ReleaseAssets(); err != nil {
		return out, nil
	}
	if i := slices.IndexFunc(available, func(asset platform.ReleaseAsset) bool { return asset.Ref == root }); i >= 0 {
		out.Definition.SourceVersion = available[i].SourceVersion
		closed, candidateErr := CandidateWithBindings([]platform.AssetRef{root}, available, []platform.AssetRef{root}, registry.ResolvePinned)
		if candidateErr == nil {
			out.Closure = closed.ID
			out.DependenciesState = "closed"
			for _, asset := range closed.Assets {
				if asset.Ref != root {
					out.Dependencies = append(out.Dependencies, platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion})
				}
			}
		}
	}
	membershipAssets := available
	if len(retained) > 0 && release != "" {
		selected, selectedErr := store.Candidate(release)
		if selectedErr != nil {
			return nil, selectedErr
		}
		membershipAssets = selected.Assets
	}
	lookup := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range membershipAssets {
		lookup[asset.Ref] = asset
	}
	target := platform.AssetRef{App: root.App, Kind: platform.AssetObject, Name: action.Target}
	for _, asset := range membershipAssets {
		if asset.Ref.Kind != platform.AssetApp {
			continue
		}
		graph := publication.RunGraph(asset.Ref, lookup)
		if !graph[root] && !graph[target] {
			continue
		}
		candidate, candidateErr := CandidateWithBindings([]platform.AssetRef{asset.Ref}, membershipAssets, []platform.AssetRef{asset.Ref}, registry.ResolvePinned)
		if candidateErr != nil {
			continue
		}
		execution, executionErr := CandidateWithBindings([]platform.AssetRef{root}, candidate.Assets, []platform.AssetRef{root}, registry.ResolvePinned)
		if executionErr == nil && execution.ID == out.Closure {
			out.Applications = append(out.Applications, platform.AssetBinding{Ref: asset.Ref, SourceVersion: candidate.ID})
		}
	}
	return out, nil
}
