package publication

import (
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	"platformserver/platform"
)

func (resolver Resolver) OrderReleaseAssets(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	graph, err := resolver.NewReleaseDependencyGraph(assets)
	if err != nil {
		return nil, err
	}
	refs, err := graph.InstallationOrder()
	if err != nil {
		return nil, err
	}
	byRef := make(map[platform.AssetRef]platform.ReleaseAsset, len(assets))
	for _, asset := range assets {
		byRef[asset.Ref] = asset
	}
	out := make([]platform.ReleaseAsset, 0, len(assets))
	for _, ref := range refs {
		out = append(out, byRef[ref])
	}
	return out, nil
}

func (resolver Resolver) OrderInstallations(assets []platform.ReleaseAsset, plans []protocol.AssetInstallation) ([]protocol.AssetInstallation, error) {
	ordered, err := resolver.OrderReleaseAssets(assets)
	if err != nil {
		return nil, err
	}
	byRef := map[platform.AssetRef]protocol.AssetInstallation{}
	for _, plan := range plans {
		if _, ok := byRef[plan.Ref]; ok {
			return nil, fmt.Errorf("duplicate installation %s", plan.Ref)
		}
		byRef[plan.Ref] = plan
	}
	var out []protocol.AssetInstallation
	for _, asset := range ordered {
		if plan, ok := byRef[asset.Ref]; ok {
			out = append(out, plan)
			delete(byRef, asset.Ref)
		}
	}
	if len(byRef) != 0 {
		return nil, fmt.Errorf("installation lacks its candidate descriptor: %v", slices.SortedFunc(maps.Keys(byRef), func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) }))
	}
	return out, nil
}
