package main

import (
	"fmt"
	"io"
	"strings"

	"platformserver/host"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
	"platformserver/services/devops/packages"
)

type bump int

const (
	bumpNone bump = iota
	bumpPatch
	bumpMinor
	bumpMajor
)

func (b bump) String() string {
	return [...]string{"none", "patch", "minor", "major"}[b]
}

func diff(dir string, out io.Writer) (built, error) {
	previous, _, err := snapshot(dir)
	if err != nil {
		return built{}, err
	}
	b, err := build(dir, previous)
	if err != nil {
		return b, err
	}
	if previous == nil {
		fmt.Fprintf(out, "%s@%s: first release\n", b.id, b.version)
		return b, nil
	}
	current, err := packages.DecodeContent([]byte(b.artifact))
	if err != nil {
		return b, err
	}
	review, err := reviewUpgrade(*previous, current)
	if err != nil {
		return b, fmt.Errorf("%s: %w", dir, err)
	}
	needed := bumpNone
	fmt.Fprintf(out, "%s %s -> %s\n", b.id, previous.Version, current.Version)
	for _, change := range review.Changes {
		fmt.Fprintf(out, "  %-7s %s", change.Change, change.Ref)
		if change.Before != "" || change.After != "" {
			fmt.Fprintf(out, " (%s -> %s)", change.Before, change.After)
		}
		fmt.Fprintln(out)
		needed = max(needed, bumpPatch)
		if change.Change != "Changed" {
			needed = max(needed, bumpMinor)
		}
		for _, field := range change.Fields {
			fmt.Fprintf(out, "          %s field %s\n", field.Change, field.Name)
			if field.Change == "added" {
				needed = max(needed, bumpMinor)
			}
			if field.Incompatible {
				needed = bumpMajor
			}
		}
	}
	if len(review.Changes) == 0 {
		fmt.Fprintln(out, "  no resource changes")
	}
	if len(review.Problems) > 0 {
		return b, fmt.Errorf("%s@%s: the platform refuses this upgrade:\n  %s", b.id, b.version, strings.Join(review.Problems, "\n  "))
	}
	if got := versionBump(previous.Version, current.Version); got < needed {
		hint := ""
		if strings.HasPrefix(previous.Version, "0.") {
			hint = "; below 1.0 a patch bump counts as minor and a minor bump as major"
		}
		return b, fmt.Errorf("%s: the changes need a %s release over %s, but the version is %s%s", b.id, needed, previous.Version, current.Version, hint)
	}
	return b, nil
}

func reviewUpgrade(previous, current platform.PackageDescriptor) (packages.PackageReview, error) {
	previous.Version += "+released"
	views := []protocol.PackageView{{Descriptor: previous}, {Descriptor: current}}
	views[1].Installed = &protocol.InstalledPackage{ID: previous.ID, Version: previous.Version, State: "installed"}
	return packages.Review{
		Resolver:  host.PublicationResolver(),
		Blueprint: func(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) { return assets, nil },
		Impact: func(string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
			return platform.PackageLifecycleImpact{}, nil
		},
	}.Of(views, &views[1], false)
}

func versionBump(before, after string) bump {
	a, errA := packagedesc.ParsePackageVersion(before)
	b, errB := packagedesc.ParsePackageVersion(after)
	if errA != nil || errB != nil || packagedesc.ComparePackageVersions(after, before) <= 0 {
		return bumpNone
	}
	switch {
	case b.Major > a.Major:
		return bumpMajor
	case b.Minor > a.Minor:
		if a.Major == 0 {
			return bumpMajor
		}
		return bumpMinor
	case b.Patch > a.Patch && a.Major == 0:
		return bumpMinor
	}
	return bumpPatch
}
