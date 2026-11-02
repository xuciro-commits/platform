package packages

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func DecodeContent(raw []byte) (platform.PackageDescriptor, error) {
	var descriptor platform.PackageDescriptor
	if err := platform.DecodeJSON(raw, &descriptor, "platform package"); err != nil {
		return platform.PackageDescriptor{}, err
	}
	if descriptor.ID == "" && descriptor.Version == "" && descriptor.Namespace == "" {
		return platform.PackageDescriptor{}, fmt.Errorf("The package file does not describe a platform package")
	}
	return descriptor, nil
}

const MaxPackageImportBytes = 16 << 20

const SchemaPackageImport = "platform.package.import"

const SchemaPackageRemoveSource = "platform.package.remove-source"

func OfferKey(tenant, digest string) string {
	return "packages/" + tenant + "/offers/" + strings.TrimPrefix(digest, "sha256:") + ".json"
}

func (d *Owner) decidePackageSource(_ platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	var input struct{ ID, Digest string }
	if json.Unmarshal(s.GetPayload(), &input) != nil || input.ID == "" || !platform.ValidDigest(input.Digest) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the reviewed package content")
	}
	if s.GetSchema().GetName() == SchemaPackageRemoveSource {
		d.mu.Lock()
		source, found := d.packageSources[input.Digest]
		held := d.packages[input.ID]
		d.mu.Unlock()
		if !found || source.ID != input.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The package source is unavailable")
		}
		if held != nil && held.State != "retired" && held.Version == source.Version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Uninstall this version before removing its source")
		}
		return func(*pb.ChangeRecord) { delete(d.packageSources, input.Digest) }, nil
	}
	offer, err := d.Offer(input.Digest)
	if err != nil || offer.Descriptor.ID != input.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The reviewed package does not match this request")
	}
	index, err := d.packageIndex()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package catalogue cannot be read")
	}
	if prior, found := index.Find(offer.Descriptor.ID, offer.Descriptor.Version); found {
		before, _ := json.Marshal(prior)
		after, _ := json.Marshal(offer.Descriptor)
		if !bytes.Equal(before, after) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A package version cannot be replaced; publish a new version")
		}
	}
	if err = d.verifyPackageVersion(offer.Descriptor, d.packagesOf()[input.ID]); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	return func(*pb.ChangeRecord) {
		if d.packageSources == nil {
			d.packageSources = map[string]platform.PackageSource{}
		}
		d.packageSources[input.Digest] = platform.PackageSource{ID: input.ID, Version: offer.Descriptor.Version, Digest: input.Digest, Origin: offer.Origin}
	}, nil
}

func (d *Owner) sourceIndex(index *platform.PackageIndex) (*platform.PackageIndex, error) {
	out := &platform.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
	d.mu.Lock()
	sources := make([]platform.PackageSource, 0, len(d.packageSources))
	for _, source := range d.packageSources {
		sources = append(sources, source)
	}
	d.mu.Unlock()
	slices.SortFunc(sources, func(a, b platform.PackageSource) int { return strings.Compare(a.Digest, b.Digest) })
	for _, source := range sources {
		offer, err := d.Offer(source.Digest)
		if err != nil {
			return index, err
		}
		if prior, found := out.Find(source.ID, source.Version); found {
			a, _ := json.Marshal(prior)
			b, _ := json.Marshal(offer.Descriptor)
			if !bytes.Equal(a, b) {
				return index, fmt.Errorf("package %s@%s has conflicting contents", source.ID, source.Version)
			}
			continue
		}
		out.Packages = append(out.Packages, offer.Descriptor)
	}
	slices.SortFunc(out.Packages, func(a, b platform.PackageDescriptor) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return platform.ComparePackageVersions(a.Version, b.Version)
	})
	return out, nil
}

func (d *Owner) packageBlueprint(id, version string) (platform.PackageBlueprint, error) {
	d.mu.Lock()
	held := d.packages[id]
	artifact := ""
	if held != nil && held.State == "active" && held.Version == version {
		artifact = held.Artifact
	}
	d.mu.Unlock()
	if artifact == "" {
		return platform.PackageBlueprint{}, fmt.Errorf("package %s@%s is not the installed active version", id, version)
	}
	descriptor, err := d.sealedPackageDescriptor(id, version, artifact)
	if err != nil {
		return platform.PackageBlueprint{}, err
	}
	if descriptor.Blueprint == nil {
		return platform.PackageBlueprint{}, fmt.Errorf("package %s@%s has no application blueprint", id, version)
	}
	return *descriptor.Blueprint, nil
}

func (d *Owner) InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error) {
	blueprint, err := d.packageBlueprint(id, version)
	return blueprint.Roots, blueprint.Assets, err
}

func (d *Owner) PreparationBlueprint(id, version string) (platform.PackageBlueprint, *platform.InstalledPackage, error) {
	for _, view := range d.PackageViews() {
		if view.Descriptor.ID != id || view.Descriptor.Version != version {
			continue
		}
		if view.Installed != nil && view.Installed.Version == version && view.Installed.State == "active" {
			blueprint, err := d.packageBlueprint(id, version)
			return blueprint, view.Installed, err
		}
		if view.Descriptor.Blueprint != nil {
			return *view.Descriptor.Blueprint, view.Installed, nil
		}
	}
	return platform.PackageBlueprint{}, nil, fmt.Errorf("application package %s@%s is unavailable", id, version)
}

func (d *Owner) packageDependents(id string) []string {
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
		var descriptor platform.PackageDescriptor
		var err error
		if held.Artifact != "" {
			descriptor, err = d.sealedPackageDescriptor(other, held.Version, held.Artifact)
		} else {
			var index *platform.PackageIndex
			index, err = d.packageIndex()
			if err == nil {
				var found bool
				descriptor, found = index.Find(other, held.Version)
				if !found {
					err = fmt.Errorf("installed declaration %s@%s is unavailable", other, held.Version)
				}
			}
		}
		if err != nil {
			blockers = append(blockers, fmt.Sprintf("Package retirement cannot check %s: %s", other, err))
			continue
		}
		if slices.ContainsFunc(descriptor.Requires, func(need string) bool { required, _ := platform.PackageDependency(need); return required == id }) {
			blockers = append(blockers, fmt.Sprintf("The package %s is still required by %s", id, other))
		}
	}
	return blockers
}

func (d *Owner) packageRemoval(id string) *platform.PackageRemoval {
	if held := d.packagesOf()[id]; held != nil && held.State == "retired" {
		return &platform.PackageRemoval{ID: id, OK: true, Blockers: []string{}}
	}
	blockers := d.packageDependents(id)
	return &platform.PackageRemoval{ID: id, OK: len(blockers) == 0, Blockers: blockers}
}

func Contract(id string) platform.CapabilityContract {
	return platform.CapabilityContract{ID: "package." + id, Version: "1", Semantics: "1"}
}

func Module(p platform.PackageDescriptor) platform.CapabilityModule {
	module := platform.CapabilityModule{ID: "package." + p.ID, Version: p.Version, Title: p.Title, Scope: "tenant", Replacement: "drain", Provides: []platform.CapabilityContract{Contract(p.ID)}, Requires: slices.Clone(p.Capabilities)}
	for _, need := range p.Requires {
		id, _ := platform.PackageDependency(need)
		module.Requires = append(module.Requires, Contract(id))
	}
	return module
}

func (d *Owner) packageComposition(candidate platform.PackageDescriptor, index *platform.PackageIndex, proposed ...platform.PackageDescriptor) (platform.CompositionPlan, error) {
	modules := d.host.BaseModules()
	installed, err := d.installedDescriptors(index)
	if err != nil {
		return platform.CompositionPlan{}, err
	}
	for _, descriptor := range proposed {
		installed[descriptor.ID] = descriptor
	}
	installed[candidate.ID] = candidate
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		modules = append(modules, Module(installed[id]))
	}
	return platform.ResolveComposition(modules, nil)
}

type Catalog interface{ BindPackageIndex(*platform.PackageIndex) }

func (d *Owner) Precheck(p platform.PackageDescriptor) platform.Precheck {
	index, err := d.packageIndex()
	if err != nil {
		return platform.Precheck{ID: p.ID, Version: p.Version, Problems: []string{err.Error()}}
	}
	if current, found := index.Find(p.ID, p.Version); found {
		p = current
	}
	return d.precheck(p, index)
}

func (d *Owner) Offer(digest string) (platform.Offer, error) {
	var offer platform.Offer
	if !platform.ValidDigest(digest) {
		return offer, fmt.Errorf("The reviewed package digest is invalid")
	}
	raw, err := d.host.GetPackageArtifact(OfferKey(d.tenant, digest))
	if err != nil {
		return offer, fmt.Errorf("The reviewed package is unavailable; inspect it again")
	}
	sum := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return offer, fmt.Errorf("The reviewed package content changed")
	}
	if err = platform.DecodeJSON(raw, &offer, "reviewed package"); err != nil {
		return offer, err
	}
	if err := offer.Descriptor.Validate(); err != nil {
		return offer, err
	}
	return offer, nil
}
