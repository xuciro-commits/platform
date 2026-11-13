package host

import (
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

func (t *Tenant) closures() functionauthoring.Closures {
	return functionauthoring.NewClosures(
		func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) },
		t.describer().DescribeOwnedAsset,
		t.releases.RetainedClosure,
		func(app string) string { return t.App(app).Manifest().Version })
}
