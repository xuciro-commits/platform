package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"platformserver/platform"
)

func FreezeVersion(raw []byte, request platform.ProductVersionRequest, views func() []platform.PackageView) (platform.FrozenProductVersion, error) {
	if len(raw) == 0 {
		return platform.FrozenProductVersion{}, fmt.Errorf("release candidate %s is not saved", request.Candidate)
	}
	candidate, err := platform.ReadCandidate(request.Candidate, raw)
	if err != nil {
		return platform.FrozenProductVersion{}, err
	}
	descriptor, err := productDescriptor(candidate, request, views)
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

func productDescriptor(candidate platform.ReleaseCandidate, request platform.ProductVersionRequest, packageViews func() []platform.PackageView) (platform.PackageDescriptor, error) {
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
		views := packageViews()
		index := &platform.PackageIndex{}
		installed := map[string]platform.PackageDescriptor{}
		for _, view := range views {
			index.Packages = append(index.Packages, view.Descriptor)
			if view.Installed != nil && view.Installed.State == "active" && view.Installed.Version == view.Descriptor.Version {
				installed[view.Descriptor.ID] = view.Descriptor
			}
		}
		dependencies, err := ResolveDependencies(product, index, installed)
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
