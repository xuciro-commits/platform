package host

import (
	"slices"

	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/authoring"
)

func (h hostView) ProductVersion(request platform.ProductVersionRequest) (platform.FrozenProductVersion, error) {
	return packages.FreezeVersion(slices.Clone(h.t.releases.Raw(request.Candidate)), request, func() []platform.PackageView {
		return h.t.App("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
	})
}

func (h hostView) ProductVersions() []platform.ProductVersionView {
	t := h.t
	var rows []build.ProductVersion
	if versions := t.Projection.Type(build.ProductVersionType); versions != nil {
		for _, row := range versions.Rows().All() {
			rows = append(rows, row.Value.Interface().(build.ProductVersion))
		}
	}
	return packages.ProductVersionViews(rows)
}
