package publication

import "platformserver/platform"

type Resolver struct {
	CheckPage          func(platform.Page) error
	CheckFrozenQueries func(platform.Page, map[platform.AssetRef]platform.ReleaseAsset) error
}
