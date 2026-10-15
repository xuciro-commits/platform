package platformserver

import (
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// DecisionDefinition belongs to the original accepted decision. It records
// its execution contract and resource memberships, never an inferred time
// interval or a second application/decision state.
type DecisionDefinition struct {
	Observation       *ObservationDecisionEvidence `json:"observation,omitempty"`
	Definition        platform.AssetBinding        `json:"definition"`
	Contract          platform.SchemaVersion       `json:"contract"`
	Closure           string                       `json:"closure,omitempty"`
	DependenciesState string                       `json:"dependenciesState"`
	Release           string                       `json:"release,omitempty"`
	Dependencies      []platform.AssetBinding      `json:"dependencies"`
	Applications      []platform.AssetBinding      `json:"applications"`
}

func (t *Tenant) decisionDefinition(owner platform.App, request *pb.Submission, release string, retained ...[]platform.ReleaseAsset) (*DecisionDefinition, error) {
	if declared := t.app(owner.Manifest().ID); declared != nil {
		owner = declared
	}
	action, exists := owner.Manifest().Actions.Action(request.GetSchema().GetName())
	if !exists {
		return nil, nil
	}
	contract, err := t.actionSchemaVersion(owner, action)
	if err != nil {
		return nil, err
	}
	if action.SchemaVersion == nil {
		contract.Family = request.GetSchema().GetVersion()
	}
	root := platform.AssetRef{App: owner.Manifest().ID, Kind: platform.AssetAction, Name: action.Schema}
	out := &DecisionDefinition{Definition: platform.AssetBinding{Ref: root, SourceVersion: owner.Manifest().Version}, Contract: contract, Release: release, DependenciesState: "unavailable", Dependencies: []platform.AssetBinding{}, Applications: []platform.AssetBinding{}}
	var available []platform.ReleaseAsset
	if len(retained) > 0 {
		available = slices.Clone(retained[0])
	} else {
		available, err = t.releaseAssetsLocked(nil, false)
		if err != nil {
			return nil, err
		}
	}
	if i := slices.IndexFunc(available, func(asset platform.ReleaseAsset) bool { return asset.Ref == root }); i >= 0 {
		out.Definition.SourceVersion = available[i].SourceVersion
		closed, err := t.candidateWithBindings([]platform.AssetRef{root}, available, []platform.AssetRef{root})
		if err != nil {
			// Some native code declarations have execution dependencies outside
			// the portable release profile. Record this limit without adding a
			// new admission condition to their original decision path.
			return out, nil
		}
		out.Closure = closed.ID
		out.DependenciesState = "closed"
		for _, asset := range closed.Assets {
			if asset.Ref != root {
				out.Dependencies = append(out.Dependencies, platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion})
			}
		}
	}
	membershipAssets := available
	if len(retained) > 0 && release != "" {
		selected, err := t.releases.candidate(release)
		if err != nil {
			return nil, err
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
		graph := applicationRunGraph(asset.Ref, lookup)
		if !graph[root] && !graph[target] {
			continue
		}
		candidate, err := t.candidateWithBindings([]platform.AssetRef{asset.Ref}, membershipAssets, []platform.AssetRef{asset.Ref})
		if err != nil {
			// An unrelated application's stale declaration does not change the
			// execution contract. Only complete memberships are recorded.
			continue
		}
		execution, err := t.candidateWithBindings([]platform.AssetRef{root}, candidate.Assets, []platform.AssetRef{root})
		if err == nil && execution.ID == out.Closure {
			out.Applications = append(out.Applications, platform.AssetBinding{Ref: asset.Ref, SourceVersion: candidate.ID})
		}
	}
	return out, nil
}

func (b DecisionDefinition) check(app, schema string) error {
	if b.Observation != nil {
		if err := b.Observation.Check(); err != nil {
			return err
		}
	}
	if b.Definition.Ref != (platform.AssetRef{App: app, Kind: platform.AssetAction, Name: schema}) || b.Definition.SourceVersion == "" || b.Contract.Check() != nil || len(b.Dependencies) > 1000 || len(b.Applications) > 1000 || slices.ContainsFunc(b.Applications, func(binding platform.AssetBinding) bool { return binding.Ref.Kind != platform.AssetApp }) {
		return fmt.Errorf("decision definition differs from its receipt")
	}
	for _, bindings := range [][]platform.AssetBinding{b.Dependencies, b.Applications} {
		seen := map[platform.AssetRef]bool{}
		for _, binding := range bindings {
			if binding.Ref.Check() != nil || binding.SourceVersion == "" || seen[binding.Ref] {
				return fmt.Errorf("decision dependency has an invalid or duplicate binding")
			}
			seen[binding.Ref] = true
		}
	}
	return nil
}
