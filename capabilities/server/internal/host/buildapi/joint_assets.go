package buildapi

import "platformserver/platform"

type JointDraftAssets struct {
	Before []platform.ReleaseAsset `json:"-"`
	After  []platform.ReleaseAsset `json:"-"`
	Priors []platform.AssetRef     `json:"-"`
	Nexts  []platform.AssetRef     `json:"-"`
}
