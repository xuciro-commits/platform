package platformserver

import (
	"fmt"
	"slices"
)

// PackageRemoval is what a person sees before uninstalling: blockers only.
type PackageRemoval struct {
	ID       string   `json:"id"`
	OK       bool     `json:"ok"`
	Blockers []string `json:"blockers"`
}

func (d *Console) packageDependents(id string) []string {
	installed := d.packagesOf()
	ids := make([]string, 0, len(installed))
	for other := range installed {
		ids = append(ids, other)
	}
	slices.Sort(ids)
	var blockers []string
	for _, other := range ids {
		held := installed[other]
		if other == id || held.State == "retired" {
			continue
		}
		var descriptor PackageDescriptor
		var err error
		if held.Artifact != "" {
			descriptor, err = d.sealedPackageDescriptor(other, held.Version, held.Artifact)
		} else {
			var index *PackageIndex
			index, err = d.packageIndex()
			if err == nil {
				var found bool
				descriptor, found = index.find(other, held.Version)
				if !found {
					err = fmt.Errorf("installed declaration %s@%s is unavailable", other, held.Version)
				}
			}
		}
		if err != nil {
			blockers = append(blockers, fmt.Sprintf("Package retirement cannot check %s: %s", other, err))
			continue
		}
		if slices.Contains(descriptor.Requires, id) {
			blockers = append(blockers, fmt.Sprintf("The package %s is still required by %s", id, other))
		}
	}
	return blockers
}

func (d *Console) packageRemoval(id string) *PackageRemoval {
	if held := d.packagesOf()[id]; held != nil && held.State == "retired" {
		return &PackageRemoval{ID: id, OK: true, Blockers: []string{}}
	}
	blockers := d.packageDependents(id)
	return &PackageRemoval{ID: id, OK: len(blockers) == 0, Blockers: blockers}
}
