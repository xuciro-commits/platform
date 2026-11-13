package host

import (
	"platformkernel/kernel"
	"platformserver/services/devops/packages"
	"time"

	"platformserver/platform"
)

func (h hostView) PackageLifecycleImpact(c platform.Caller, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	return h.t.packageLifecycleImpact(c.Member, id, after)
}

func (t *Tenant) packageLifecycleImpact(m platform.Member, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	ctx := t.applicationContext()
	return packages.StoredImpact(t.Projection, t.definitions, t.OwnerActive, t.releaseExtensionReview, func(ref platform.AssetRef, offset int) (platform.ApplicationRunPage, *kernel.Error) {
		return ctx.Runs(m, ref, offset, 100, time.Now())
	}).Of(id, after)
}
