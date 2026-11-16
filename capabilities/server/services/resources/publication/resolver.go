package publication

import "platformserver/platform"

type Resolver struct {
	CheckPage               func(platform.Page) error
	CheckFrozenQueries      func(platform.Page, map[platform.AssetRef]platform.ReleaseAsset) error
	Safety                  func(platform.Page) (platform.Page, error)
	PageDependencies        func(platform.Page) []platform.AssetRef
	PageNavigationEdge      func(platform.ReleaseAsset, platform.AssetRef) bool
	CheckPageEmbeddingGraph func(platform.AssetRef, map[platform.AssetRef]platform.ReleaseAsset) error
	CheckPageVariables      func(platform.Application, platform.Page) error
	PageContentVersion      func(platform.Page) (string, error)
}
