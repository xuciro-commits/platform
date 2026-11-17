package descriptor

import "platformserver/platform"

func Latest(i *platform.PackageIndex, id string) (platform.PackageDescriptor, bool) {
	var out platform.PackageDescriptor
	found := false
	for _, p := range i.Packages {
		version, _ := ParsePackageVersion(p.Version)
		if p.ID == id && version.Prerelease == "" && (!found || ComparePackageVersions(p.Version, out.Version) > 0) {
			out, found = p, true
		}
	}
	return out, found
}

func Find(i *platform.PackageIndex, id, version string) (platform.PackageDescriptor, bool) {
	if version == "" {
		return Latest(i, id)
	}
	for _, p := range i.Packages {
		if p.ID == id && (version == "" || p.Version == version) {
			return p, true
		}
	}
	return platform.PackageDescriptor{}, false
}
