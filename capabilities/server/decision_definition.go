package platformserver

import (
	appowner "platformserver/engine/applications"

	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// DecisionDefinition belongs to the original accepted decision. It records
// its execution contract and resource memberships, never an inferred time
// interval or a second application/decision state.
type DecisionDefinition = platform.DecisionDefinition

func (t *Tenant) decisionDefinition(owner platform.App, request *pb.Submission, release string, retained ...[]platform.ReleaseAsset) (*platform.DecisionDefinition, error) {
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
	out := &platform.DecisionDefinition{Definition: platform.AssetBinding{Ref: root, SourceVersion: owner.Manifest().Version}, Contract: contract, Release: release, DependenciesState: "unavailable", Dependencies: []platform.AssetBinding{}, Applications: []platform.AssetBinding{}}
	var available []platform.ReleaseAsset
	if len(retained) > 0 {
		available = slices.Clone(retained[0])
	} else {
		available, err = t.releaseAssetsLocked(nil, false)
		if err != nil {
			// Diagnostics must not block business work or repairing an unready
			// connection. Execution still uses the installed owner and its own
			// dependency checks; an unavailable closure is recorded as such.
			return out, nil
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
		selected, err := t.releases.Candidate(release)
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
		graph := appowner.RunGraph(asset.Ref, lookup)
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
