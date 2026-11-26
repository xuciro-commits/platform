package packages

import (
	"fmt"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
)

func reviewExtensionVersions(extensions []platform.PackageImpactExtension, views []protocol.PackageView, target platform.PackageDescriptor) {
	for i := range extensions {
		extension := &extensions[i]
		if extension.PackageID == "" || extension.PackageID == target.ID {
			continue
		}
		for _, view := range views {
			if view.Descriptor.ID != extension.PackageID || view.Installed == nil || view.Installed.State == "retired" || view.Installed.Version != view.Descriptor.Version {
				continue
			}
			var constraints []string
			for _, requirement := range view.Descriptor.Requires {
				id, constraint := packagedesc.PackageDependency(requirement)
				if id != target.ID {
					continue
				}
				if constraint == "" {
					constraint = "*"
				}
				constraints = append(constraints, constraint)
				if !packagedesc.PackageVersionMatches(target.Version, constraint) {
					reason := fmt.Sprintf("Extension %s requires %s@%s; target %s does not satisfy this range", extension.Type, target.ID, constraint, target.Version)
					if extension.Issue != "" {
						extension.Issue += "; "
					}
					extension.Issue += reason
				}
			}
			extension.VersionRange = strings.Join(constraints, "; ")
			break
		}
	}
}
