package publication

import (
	"encoding/json"
	"platformserver/platform"
)

func QuerySourceVersion(d platform.Definition, version string) *platform.Definition {
	if d.Ref.Kind == platform.AssetLinkType {
		return d.LinkVersion(version)
	}
	return QueryVersion(d, version)
}

func QueryVersion(d platform.Definition, version string) *platform.Definition {
	if version == d.Version && d.Query != nil {
		return &d
	}
	q, ok := d.QueryVersions[version]
	if !ok {
		return nil
	}
	body, err := json.Marshal(q)
	if err != nil {
		return nil
	}
	description, err := platform.DescribeAsset(platform.ReleaseAsset{Ref: d.Ref, SourceVersion: version, Body: body})
	if err != nil {
		return nil
	}
	d.Query, d.Version, d.QueryVersions = &q, version, nil
	d.Requires, d.Provides = description.Requires, description.Provides
	return &d
}

func RunGraph(root platform.AssetRef, assets map[platform.AssetRef]platform.ReleaseAsset) map[platform.AssetRef]bool {
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
