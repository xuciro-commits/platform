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
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
)

func DecodeContent(raw []byte) (platform.PackageDescriptor, error) {
	var descriptor platform.PackageDescriptor
	if err := platform.DecodeJSON(raw, &descriptor, "platform package"); err != nil {
		return platform.PackageDescriptor{}, err
	}
	if descriptor.ID == "" && descriptor.Version == "" && descriptor.Namespace == "" {
		return platform.PackageDescriptor{}, fmt.Errorf("The package file does not describe a platform package")
	}
	if descriptor.Blueprint == nil {
		return platform.PackageDescriptor{}, fmt.Errorf("Marketplace consumes product versions only; create a version from a saved release candidate")
	}
	return descriptor, nil
}

const MaxPackageImportBytes = 16 << 20

const SchemaPackageImport = "platform.package.import"

const SchemaPackageRemoveSource = "platform.package.remove-source"

func OfferKey(tenant, digest string) string {
	return "packages/" + tenant + "/offers/" + strings.TrimPrefix(digest, "sha256:") + ".json"
}

func (d *Owner) decidePackageSource(_ platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *platform.Error) {
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
	if prior, found := packagedesc.Find(index, offer.Descriptor.ID, offer.Descriptor.Version); found {
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
			d.packageSources = map[string]runtimeprotocol.PackageSource{}
		}
		d.packageSources[input.Digest] = runtimeprotocol.PackageSource{ID: input.ID, Version: offer.Descriptor.Version, Digest: input.Digest, Origin: offer.Origin}
	}, nil
}

func (d *Owner) sourceIndex(index *runtimeprotocol.PackageIndex) (*runtimeprotocol.PackageIndex, error) {
	out := &runtimeprotocol.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
	appendDescriptor := func(descriptor platform.PackageDescriptor) error {
		if prior, found := packagedesc.Find(out, descriptor.ID, descriptor.Version); found {
			a, _ := json.Marshal(prior)
			b, _ := json.Marshal(descriptor)
			if !bytes.Equal(a, b) {
				return fmt.Errorf("product %s@%s has conflicting contents", descriptor.ID, descriptor.Version)
			}
			return nil
		}
		out.Packages = append(out.Packages, descriptor)
		return nil
	}
	if d.host != nil {
		for _, version := range d.host.ProductVersions() {
			if err := appendDescriptor(version.Descriptor); err != nil {
				return index, err
			}
		}
	}
	d.mu.Lock()
	sources := make([]runtimeprotocol.PackageSource, 0, len(d.packageSources))
	for _, source := range d.packageSources {
		sources = append(sources, source)
	}
	d.mu.Unlock()
	slices.SortFunc(sources, func(a, b runtimeprotocol.PackageSource) int { return strings.Compare(a.Digest, b.Digest) })
	for _, source := range sources {
		offer, err := d.Offer(source.Digest)
		if err != nil {
			return index, err
		}
		if err := appendDescriptor(offer.Descriptor); err != nil {
			return index, err
		}
	}
	slices.SortFunc(out.Packages, func(a, b platform.PackageDescriptor) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return packagedesc.ComparePackageVersions(a.Version, b.Version)
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

func (d *Owner) PreparationBlueprint(id, version string) (platform.PackageBlueprint, *runtimeprotocol.InstalledPackage, error) {
	installed := d.packagesOf()
	var heldCopy *runtimeprotocol.InstalledPackage
	if held := installed[id]; held != nil {
		copy := *held
		heldCopy = &copy
		if held.Version == version && held.State == "active" {
			blueprint, err := d.packageBlueprint(id, version)
			return blueprint, heldCopy, err
		}
	}
	if index, err := d.packageIndex(); err == nil {
		if descriptor, found := packagedesc.Find(index, id, version); found && descriptor.Blueprint != nil {
			return *descriptor.Blueprint, heldCopy, nil
		}
	}
	if held := installed[id]; held != nil {
		if held.Version == version && held.Artifact != "" {
			if sealed, err := d.sealedPackageDescriptor(id, version, held.Artifact); err == nil && sealed.Blueprint != nil {
				return *sealed.Blueprint, heldCopy, nil
			}
		}
		for _, retained := range held.Retained {
			if retained.Version == version && retained.Digest != "" {
				if sealed, err := d.sealedPackageDescriptor(id, version, retained.Digest); err == nil && sealed.Blueprint != nil {
					return *sealed.Blueprint, heldCopy, nil
				}
			}
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
	active := map[string]platform.PackageDescriptor{}
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
			var index *runtimeprotocol.PackageIndex
			index, err = d.packageIndex()
			if err == nil {
				var found bool
				descriptor, found = packagedesc.Find(index, other, held.Version)
				if !found {
					err = fmt.Errorf("installed declaration %s@%s is unavailable", other, held.Version)
				}
			}
		}
		if err != nil {
			blockers = append(blockers, fmt.Sprintf("Package retirement cannot check %s: %s", other, err))
			continue
		}
		if held.State == "active" {
			active[other] = descriptor
		}
		if slices.ContainsFunc(descriptor.Requires, func(need string) bool { required, _ := packagedesc.PackageDependency(need); return required == id }) {
			blockers = append(blockers, fmt.Sprintf("The package %s is still required by %s", id, other))
		}
	}
	problems, err := requiredContractProblems(active)
	if err != nil {
		return append(blockers, err.Error())
	}
	return append(blockers, problems...)
}

func (d *Owner) packageRemoval(id string) *runtimeprotocol.PackageRemoval {
	if held := d.packagesOf()[id]; held != nil && held.State == "retired" {
		return &runtimeprotocol.PackageRemoval{ID: id, OK: true, Blockers: []string{}}
	}
	blockers := d.packageDependents(id)
	return &runtimeprotocol.PackageRemoval{ID: id, OK: len(blockers) == 0, Blockers: blockers}
}

func Contract(id string) runtimeprotocol.CapabilityContract {
	return runtimeprotocol.CapabilityContract{ID: "package." + id, Version: "1", Semantics: "1"}
}

func Module(p platform.PackageDescriptor) runtimeprotocol.CapabilityModule {
	module := runtimeprotocol.CapabilityModule{ID: "package." + p.ID, Version: p.Version, Title: p.Title, Scope: "tenant", Replacement: "drain", Provides: []runtimeprotocol.CapabilityContract{Contract(p.ID)}, Requires: slices.Clone(p.Capabilities)}
	for _, need := range p.Requires {
		id, _ := packagedesc.PackageDependency(need)
		module.Requires = append(module.Requires, Contract(id))
	}
	return module
}

func (d *Owner) packageComposition(candidate platform.PackageDescriptor, index *runtimeprotocol.PackageIndex, proposed ...platform.PackageDescriptor) (runtimeprotocol.CompositionPlan, error) {
	modules := d.host.BaseModules()
	installed, err := d.installedDescriptors(index)
	if err != nil {
		return runtimeprotocol.CompositionPlan{}, err
	}
	for _, descriptor := range proposed {
		installed[descriptor.ID] = descriptor
	}
	installed[candidate.ID] = candidate
	active := maps.Clone(installed)
	state := d.packagesOf()
	for id := range active {
		if held := state[id]; held != nil && held.State != "active" && id != candidate.ID && !slices.ContainsFunc(proposed, func(p platform.PackageDescriptor) bool { return p.ID == id }) {
			delete(active, id)
		}
	}
	if problems, err := requiredContractProblems(active); err != nil {
		return runtimeprotocol.CompositionPlan{}, err
	} else if len(problems) > 0 {
		return runtimeprotocol.CompositionPlan{}, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	for _, id := range slices.Sorted(maps.Keys(installed)) {
		modules = append(modules, Module(installed[id]))
	}
	return runtimeprotocol.ResolveComposition(modules, nil)
}

type Catalog interface {
	BindPackageIndex(*runtimeprotocol.PackageIndex)
}

func (d *Owner) Precheck(p platform.PackageDescriptor) runtimeprotocol.Precheck {
	index, err := d.packageIndex()
	if err != nil {
		return runtimeprotocol.Precheck{ID: p.ID, Version: p.Version, Problems: []string{err.Error()}}
	}
	if current, found := packagedesc.Find(index, p.ID, p.Version); found {
		p = current
	}
	return d.precheck(p, index)
}

func (d *Owner) Offer(digest string) (runtimeprotocol.Offer, error) {
	var offer runtimeprotocol.Offer
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
	if err := packagedesc.Validate(offer.Descriptor, d.resolver.WithDependencies); err != nil {
		return offer, err
	}
	return offer, nil
}
