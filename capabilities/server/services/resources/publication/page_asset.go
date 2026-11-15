package publication

import (
	"encoding/json"

	"platformserver/platform"
)

func PageReleaseAsset(app, sourceVersion string, page platform.Page) (platform.ReleaseAsset, error) {
	if err := page.CheckSafety(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(page)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: app, Kind: platform.AssetPage, Name: page.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

func ApplicationReleaseAsset(app, sourceVersion string, application platform.Application) (platform.ReleaseAsset, error) {
	if err := application.CheckVariables(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	if err := application.CheckResources(); err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(application)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: app, Kind: platform.AssetApp, Name: application.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}
