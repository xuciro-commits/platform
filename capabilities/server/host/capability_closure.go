package host

import (
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

func (t *Tenant) closures() functionauthoring.Closures {
	return functionauthoring.Closures{
		Assets: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) },
		Describe: func(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
			return t.describer().DescribeOwnedAsset(asset)
		},
		Retain: func(candidate platform.ReleaseCandidate, ref platform.AssetRef, sourceVersion string, retained *string) (platform.ReleaseCandidate, string, error) {
			return t.releases.RetainedClosure(candidate, ref, sourceVersion, retained)
		},
		Version: func(app string) string { return t.App(app).Manifest().Version },
	}
}
