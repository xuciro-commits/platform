package platformserver

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

func applicationRunGraph(root platform.AssetRef, assets map[platform.AssetRef]platform.ReleaseAsset) map[platform.AssetRef]bool {
	graph := map[platform.AssetRef]bool{}
	var visit func(platform.AssetRef)
	visit = func(ref platform.AssetRef) {
		if graph[ref] {
			return
		}
		graph[ref] = true
		asset, ok := assets[ref]
		if !ok {
			return
		}
		for _, dependency := range asset.Requires {
			visit(dependency)
		}
		if ref.Kind == platform.AssetApp {
			var app platform.Application
			if json.Unmarshal(asset.Body, &app) == nil {
				for _, dependency := range app.Dependencies(ref.App) {
					visit(dependency)
				}
			}
		}
	}
	visit(root)
	return graph
}

// The canonical release owner supplies published flow/compute dependencies;
// the runtime view does not reconstruct them from menu labels or draft data.
func (t *Tenant) applicationRunGraphsLocked(root platform.AssetRef) (map[platform.AssetRef]platform.ReleaseAsset, map[string]map[platform.AssetRef]bool, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return nil, nil, err
	}
	current := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, a := range available {
		current[a.Ref] = a
	}
	graphs := map[string]map[platform.AssetRef]bool{"": applicationRunGraph(root, current)}
	for id, raw := range t.releases.candidates {
		candidate, err := platform.ReadCandidate(id, raw)
		if err != nil {
			return nil, nil, err
		}
		assets := map[platform.AssetRef]platform.ReleaseAsset{}
		for _, a := range candidate.Assets {
			assets[a.Ref] = a
		}
		if _, ok := assets[root]; ok {
			graphs[id] = applicationRunGraph(root, assets)
		}
	}
	return current, graphs, nil
}

func operationStartupVersion(call platform.OperationCall) string {
	if call.Version > 0 {
		return fmt.Sprint(call.Version)
	}
	if call.OwnerVersion != "" {
		return "code:" + call.OwnerVersion
	}
	return ""
}
