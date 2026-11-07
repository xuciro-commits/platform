package packages

import "platformserver/platform"

func (d *Owner) LifecycleImpact(c platform.Caller, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	impact, err := d.host.PackageLifecycleImpact(c, id, after)
	if err != nil {
		return impact, err
	}
	impact.Dependencies = append(impact.Dependencies, d.packageDependents(id)...)
	return impact, nil
}
