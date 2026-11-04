package host

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	raw := slices.Clone(t.releases.Raw(request.Candidate))
	if len(raw) == 0 {
		return platform.FrozenProductVersion{}, fmt.Errorf("release candidate %s is not saved", request.Candidate)
	}
	candidate, err := platform.ReadCandidate(request.Candidate, raw)
	if err != nil {
		return platform.FrozenProductVersion{}, err
	}
	descriptor, err := t.productDescriptor(candidate, request)
	if err != nil {
		return platform.FrozenProductVersion{}, err
	}
	candidateSum := sha256.Sum256(raw)
	content, err := platform.CanonicalDigest(descriptor)
	if err != nil {
		return platform.FrozenProductVersion{}, err
	}
	return platform.FrozenProductVersion{
		Version: request.Version, Candidate: request.Candidate,
		CandidateDigest: "sha256:" + hex.EncodeToString(candidateSum[:]), ContentDigest: content,
		Descriptor: descriptor, At: request.At,
	}, nil
}

func (t *Tenant) productDescriptor(candidate platform.ReleaseCandidate, request platform.ProductVersionRequest) (platform.PackageDescriptor, error) {
	closed, err := candidate.Closure(request.Roots)
	if err != nil {
		return platform.PackageDescriptor{}, err
	}
	if request.Namespace == "" {
		request.Namespace = request.ID
	}
	product := platform.PackageDescriptor{ID: request.ID, Version: request.Version, Title: request.Title, Namespace: request.Namespace, Requires: request.Requires}
	external := map[platform.AssetRef]bool{}
	if len(product.Requires) > 0 {
		views := t.app("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
		index := &platform.PackageIndex{}
		installed := map[string]platform.PackageDescriptor{}
		for _, view := range views {
			index.Packages = append(index.Packages, view.Descriptor)
			if view.Installed != nil && view.Installed.State == "active" && view.Installed.Version == view.Descriptor.Version {
				installed[view.Descriptor.ID] = view.Descriptor
			}
		}
		dependencies, err := packages.ResolveDependencies(product, index, installed)
		if err != nil {
			return platform.PackageDescriptor{}, err
		}
		for _, dependency := range dependencies {
			if dependency.ID != product.ID && dependency.Blueprint != nil {
				for _, asset := range dependency.Blueprint.Assets {
					external[asset.Ref] = true
				}
			}
		}
	}
	assets := slices.DeleteFunc(slices.Clone(closed.Assets), func(asset platform.ReleaseAsset) bool { return external[asset.Ref] })
	product.Blueprint = &platform.PackageBlueprint{Roots: slices.Clone(request.Roots), Assets: assets}
	if err := product.Validate(); err != nil {
		return platform.PackageDescriptor{}, err
	}
	return product, nil
}

func (t *Tenant) ProductVersions() []platform.ProductVersionView {
	records := t.records.Type(build.ProductVersionType)
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
