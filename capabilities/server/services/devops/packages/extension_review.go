package packages

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
	build "platformserver/services/resources/authoring"
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

// ExtensionProblems is shared by review and the authoritative submit gate.
func ExtensionProblems(extensions []platform.PackageImpactExtension) []string {
	var problems []string
	for _, extension := range extensions {
		if extension.Issue != "" {
			problems = append(problems, extension.Issue)
		}
	}
	return problems
}

func (d *Owner) extensionVersions(extensions []platform.PackageImpactExtension, target platform.PackageDescriptor, index *protocol.PackageIndex) error {
	if len(extensions) == 0 {
		return nil
	}
	installed, err := d.installedDescriptors(index)
	if err != nil {
		return err
	}
	held := d.packagesOf()
	views := make([]protocol.PackageView, 0, len(installed))
	for id, descriptor := range installed {
		views = append(views, protocol.PackageView{Descriptor: descriptor, Installed: held[id]})
	}
	reviewExtensionVersions(extensions, views, target)
	return nil
}

// extensionUpgradeIssues runs on the accepted decision's private read view,
// before any new artifact is sealed or managed resource is staged.
func (d *Owner) extensionUpgradeIssues(c platform.Caller, descriptor platform.PackageDescriptor, prior *protocol.InstalledPackage, index *protocol.PackageIndex) ([]string, error) {
	if prior == nil || prior.State == "retired" || prior.Version == descriptor.Version || descriptor.Blueprint == nil {
		return nil, nil
	}
	var err error
	if prior.Space != "" {
		descriptor, err = Rebind(descriptor, prior.Space)
		if err != nil {
			return nil, err
		}
	}
	var owner build.ConstructionDrafts
	for _, app := range d.host.Apps() {
		if app.Manifest().ID == build.ID {
			owner, _ = app.(build.ConstructionDrafts)
			break
		}
	}
	if owner == nil {
		return nil, fmt.Errorf("application blueprints need the Build owner")
	}
	assets, err := owner.CompleteBlueprintAssets(descriptor.Blueprint.Assets)
	if err != nil {
		return nil, err
	}
	for i, asset := range assets {
		assets[i], err = d.resolver.WithDependencies(asset)
		if err != nil {
			return nil, err
		}
	}
	impact, err := d.host.PackageLifecycleImpact(c, descriptor.ID, assets)
	if err != nil {
		return nil, err
	}
	if err := d.extensionVersions(impact.Extensions, descriptor, index); err != nil {
		return nil, err
	}
	return slices.Compact(ExtensionProblems(impact.Extensions)), nil
}
