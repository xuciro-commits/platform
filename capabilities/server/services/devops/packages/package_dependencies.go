package packages

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func (d *Owner) installedDescriptors(index *platform.PackageIndex) (map[string]platform.PackageDescriptor, error) {
	out := map[string]platform.PackageDescriptor{}
	for id, held := range d.packagesOf() {
		if held.State == "retired" {
			continue
		}
		var descriptor platform.PackageDescriptor
		var err error
		if held.Artifact != "" {
			descriptor, err = d.sealedPackageDescriptor(id, held.Version, held.Artifact)
		} else {
			var found bool
			descriptor, found = index.Find(id, held.Version)
			if !found {
				err = fmt.Errorf("installed product %s has no retained declaration", id)
			}
		}
		if err != nil {
			return nil, err
		}
		out[id] = descriptor
	}
	return out, nil
}

func ResolveDependencies(root platform.PackageDescriptor, index *platform.PackageIndex, installed map[string]platform.PackageDescriptor) ([]platform.PackageDescriptor, error) {
	var failure error
	var solve func(map[string]platform.PackageDescriptor) ([]platform.PackageDescriptor, bool)
	solve = func(chosen map[string]platform.PackageDescriptor) ([]platform.PackageDescriptor, bool) {
		requirements := map[string][]string{}
		for _, descriptor := range chosen {
			for _, requirement := range descriptor.Requires {
				id, constraint := platform.PackageDependency(requirement)
				requirements[id] = append(requirements[id], constraint)
			}
		}
		matches := func(version string, constraints []string) bool {
			for _, constraint := range constraints {
				if !platform.PackageVersionMatches(version, constraint) {
					return false
				}
			}
			return true
		}
		for id, constraints := range requirements {
			if descriptor, found := chosen[id]; found && !matches(descriptor.Version, constraints) {
				failure = fmt.Errorf("product %s@%s does not satisfy %s", id, descriptor.Version, strings.Join(constraints, ", "))
				return nil, false
			}
		}
		for _, id := range slices.Sorted(maps.Keys(requirements)) {
			if _, found := chosen[id]; found {
				continue
			}
			options := []platform.PackageDescriptor{}
			for _, descriptor := range index.Packages {
				if descriptor.ID == id && matches(descriptor.Version, requirements[id]) {
					options = append(options, descriptor)
				}
			}
			slices.SortFunc(options, func(a, b platform.PackageDescriptor) int {
				return -platform.ComparePackageVersions(a.Version, b.Version)
			})
			if held, found := installed[id]; found && matches(held.Version, requirements[id]) {
				options = append([]platform.PackageDescriptor{held}, slices.DeleteFunc(options, func(p platform.PackageDescriptor) bool { return p.Version == held.Version })...)
			}
			for _, descriptor := range options {
				next := maps.Clone(chosen)
				next[id] = descriptor
				if result, ok := solve(next); ok {
					return result, true
				}
			}
			if len(options) == 0 {
				failure = fmt.Errorf("no available version of product %s satisfies %s", id, strings.Join(requirements[id], ", "))
			}
			return nil, false
		}
		for id, held := range installed {
			if _, replaced := chosen[id]; replaced {
				continue
			}
			for _, requirement := range held.Requires {
				need, constraint := platform.PackageDependency(requirement)
				version := installed[need].Version
				if replacement, exists := chosen[need]; exists {
					version = replacement.Version
				}
				if version == "" || !platform.PackageVersionMatches(version, constraint) {
					failure = fmt.Errorf("installed product %s requires %s; update it before switching %s to %s", id, requirement, need, version)
					return nil, false
				}
			}
		}
		var ordered []platform.PackageDescriptor
		state := map[string]int{}
		var visit func(string) bool
		visit = func(id string) bool {
			if state[id] == 1 {
				failure = fmt.Errorf("product dependency cycle contains %s", id)
				return false
			}
			if state[id] == 2 {
				return true
			}
			state[id] = 1
			for _, requirement := range chosen[id].Requires {
				need, _ := platform.PackageDependency(requirement)
				if !visit(need) {
					return false
				}
			}
			state[id] = 2
			ordered = append(ordered, chosen[id])
			return true
		}
		ok := visit(root.ID)
		return ordered, ok
	}
	if result, ok := solve(map[string]platform.PackageDescriptor{root.ID: root}); ok {
		return result, nil
	}
	return nil, failure
}

func (d *Owner) prerequisiteAssets(root platform.PackageDescriptor, index *platform.PackageIndex, proposed []platform.PackageDescriptor) ([]platform.ReleaseAsset, error) {
	available, err := d.host.CodeReleaseAssets()
	if err != nil {
		return nil, err
	}
	installed, err := d.installedDescriptors(index)
	if err != nil {
		return nil, err
	}
	for _, descriptor := range proposed {
		installed[descriptor.ID] = descriptor
	}
	seen := map[string]bool{root.ID: true}
	pending := slices.Clone(root.Requires)
	for len(pending) > 0 {
		requirement := pending[0]
		pending = pending[1:]
		id, _ := platform.PackageDependency(requirement)
		if seen[id] {
			continue
		}
		seen[id] = true
		descriptor, exists := installed[id]
		if !exists {
			continue
		}
		if descriptor.Blueprint != nil {
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
			for _, asset := range assets {
				complete, err := platform.WithDependencies(asset)
				if err != nil {
					return nil, err
				}
				available = append(available, complete)
			}
		}
		pending = append(pending, descriptor.Requires...)
	}
	unique := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range available {
		if prior, exists := unique[asset.Ref]; exists && (prior.SourceVersion != asset.SourceVersion || !bytes.Equal(prior.Body, asset.Body)) {
			return nil, fmt.Errorf("required products provide different definitions of %s", asset.Ref)
		}
		unique[asset.Ref] = asset
	}
	return slices.SortedFunc(maps.Values(unique), func(a, b platform.ReleaseAsset) int { return strings.Compare(a.Ref.String(), b.Ref.String()) }), nil
}

func (d *Owner) PackagePrerequisites(id, version string) ([]platform.ReleaseAsset, error) {
	index, err := d.packageIndex()
	if err != nil {
		return nil, err
	}
	descriptor, found := index.Find(id, version)
	if !found {
		installed, err := d.installedDescriptors(index)
		if err != nil {
			return nil, err
		}
		descriptor, found = installed[id]
		if !found || descriptor.Version != version {
			return nil, fmt.Errorf("product %s@%s is unavailable", id, version)
		}
	}
	return d.prerequisiteAssets(descriptor, index, nil)
}

func (d *Owner) installationPlan(root platform.PackageDescriptor, index *platform.PackageIndex) platform.PackageInstallPlan {
	plan := platform.PackageInstallPlan{Steps: []platform.PackageInstallStep{}}
	installed, err := d.installedDescriptors(index)
	if err != nil {
		plan.Problems = []string{err.Error()}
		return plan
	}
	ordered, err := ResolveDependencies(root, index, installed)
	if err != nil {
		plan.Problems = []string{err.Error()}
		return plan
	}
	for _, descriptor := range ordered {
		held, exists := installed[descriptor.ID]
		step := platform.PackageInstallStep{ID: descriptor.ID, Version: descriptor.Version, Title: descriptor.Title}
		if exists && held.Version == descriptor.Version {
			step.Action, step.Status = SchemaPackageInstall, "installed"
			plan.Steps = append(plan.Steps, step)
			continue
		}
		action := SchemaPackageInstall
		if exists {
			if platform.ComparePackageVersions(descriptor.Version, held.Version) < 0 {
				action = SchemaPackageDowngrade
			} else {
				action = SchemaPackageUpgrade
			}
			step.InstalledVersion = held.Version
		}
		step.Action, step.Status = action, "pending"
		check := d.precheck(descriptor, index, ordered)
		if len(check.Problems) > 0 {
			step.Status, step.Problem = "failed", strings.Join(check.Problems, "; ")
			plan.Problems = append(plan.Problems, check.Problems...)
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan
}
