package host

import (
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/authoring"
)

func (h hostView) ProductVersion(request platform.ProductVersionRequest) (platform.FrozenProductVersion, error) {
	return h.t.freezeProductVersion(request)
}

func (h hostView) ProductVersions() []platform.ProductVersionView { return h.t.ProductVersions() }

func (t *Tenant) freezeProductVersion(request platform.ProductVersionRequest) (platform.FrozenProductVersion, error) {
	return packages.FreezeVersion(slices.Clone(t.releases.Raw(request.Candidate)), request, func() []platform.PackageView {
		return t.App("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
	})
}

func (t *Tenant) ProductVersions() []platform.ProductVersionView {
	records := t.Projection.Type(build.ProductVersionType)
	if records == nil {
		return []platform.ProductVersionView{}
	}
	out := []platform.ProductVersionView{}
	for _, row := range records.Rows().All() {
		version := row.Value.Interface().(build.ProductVersion)
		out = append(out, platform.ProductVersionView{
			Product: version.Product, Version: version.Version, Candidate: version.Candidate, Descriptor: version.Descriptor,
		})
	}
	slices.SortFunc(out, func(a, b platform.ProductVersionView) int {
		if a.Product != b.Product {
			return strings.Compare(a.Product, b.Product)
		}
		return platform.ComparePackageVersions(a.Version, b.Version)
	})
	return out
}
