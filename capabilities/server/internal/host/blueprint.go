package host

import "platformserver/platform"

// BlueprintCatalog exposes only the installed Package owner's sealed,
// licensed definition set. Consumers cannot change installation or grants.
type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error)
}
