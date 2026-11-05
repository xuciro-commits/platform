package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type Owner struct {
	mu             sync.Mutex
	tenant         string
	packages       map[string]*platform.InstalledPackage
	packageSources map[string]platform.PackageSource
	index          *platform.PackageIndex
	host           platform.PackageHost
}

func New(tenant string) *Owner {
	return &Owner{tenant: tenant, packages: map[string]*platform.InstalledPackage{}, index: &platform.PackageIndex{}}
}

func (d *Owner) Attach(host platform.PackageHost) { d.host = host }

func (d *Owner) BindPackageIndex(index *platform.PackageIndex) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.index = index
}

func (d *Owner) BoundIndex() *platform.PackageIndex { d.mu.Lock(); defer d.mu.Unlock(); return d.index }

func (d *Owner) SnapshotState() platform.PackageState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return platform.PackageState{Packages: maps.Clone(d.packages), Sources: maps.Clone(d.packageSources)}
}

func (d *Owner) Restore(s platform.PackageState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.packages = s.Packages
	d.packageSources = s.Sources
	if d.packages == nil {
		d.packages = map[string]*platform.InstalledPackage{}
	}
}

func (d *Owner) Prepare(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	apply, err := d.decide(c, s)
	if apply == nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { d.mu.Lock(); defer d.mu.Unlock(); apply(r) }, err
}

var _ platform.BlueprintCatalog = (*Owner)(nil)

const (
	SchemaPackageInstall   = "platform.package.install"
	SchemaPackageUpgrade   = "platform.package.upgrade"
	SchemaPackageDowngrade = "platform.package.downgrade"
	SchemaPackageRollback  = "platform.package.rollback"
	SchemaPackageDrain     = "platform.package.drain"
	SchemaPackageRetire    = "platform.package.retire"
	platformCompatibility  = "1"
)

func Actions() []platform.Action {
	admin := []string{"admin"}
	fields := []platform.Field{{Name: "id", Type: "string", Required: true, Description: "The package's ID"},
		{Name: "version", Type: "string", Description: "The version; empty: the newest the index offers"}}
	return []platform.Action{
		{Schema: SchemaPackageImport, Target: platform.PackageType, Capability: "packages", Title: "Add reviewed package", Description: "Keep exactly the reviewed package in this tenant's available library; install separately.", Payload: []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "digest", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageRemoveSource, Target: platform.PackageType, Capability: "packages", Title: "Remove package source", Description: "Remove an uninstalled source from the library; installed artifacts and business history stay.", Payload: []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "digest", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageInstall, Target: platform.PackageType, Capability: "packages", Title: "Install package",
			Description: "Precheck a package's requirements and register the contributions it brings.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageUpgrade, Target: platform.PackageType, Capability: "packages", Title: "Upgrade package",
			Description: "Replace an installed package's version, keeping the artifact it replaced.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageDowngrade, Target: platform.PackageType, Capability: "packages", Title: "Downgrade package",
			Description: "Switch to an older exact version only when its storage and extensions remain compatible; the replaced artifact stays retained.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageRollback, Target: platform.PackageType, Capability: "packages", Title: "Roll back package",
			Description: "Restore the immediately previous retained version only when its storage and extensions remain compatible.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageDrain, Target: platform.PackageType, Capability: "packages", Title: "Mark package for uninstall",
			Description: "Check dependent products and mark the package for uninstall. New use is not blocked until runtime draining is enabled; finish work in flight before retiring.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageRetire, Target: platform.PackageType, Capability: "packages", Title: "Retire package",
			Description: "Archive the package's managed definitions and project; business records, extensions, history and artifacts stay.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
	}
}

func LoadPackageIndex(dir string) (*platform.PackageIndex, error) {
	index := &platform.PackageIndex{Directory: dir}
	if dir == "" {
		return index, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("package index %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("package index %s: %w", entry.Name(), err)
		}
		var list []platform.PackageDescriptor
		if err := json.Unmarshal(raw, &list); err != nil {
			var one platform.PackageDescriptor
			if err := json.Unmarshal(raw, &one); err != nil {
				return nil, fmt.Errorf("package index %s: %w", entry.Name(), err)
			}
			list = []platform.PackageDescriptor{one}
		}
		for _, p := range list {
			if err := p.Validate(); err != nil {
				return nil, fmt.Errorf("package index %s: %w", entry.Name(), err)
			}
			index.Packages = append(index.Packages, p)
		}
	}
	slices.SortFunc(index.Packages, func(a, b platform.PackageDescriptor) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return platform.ComparePackageVersions(a.Version, b.Version)
	})
	return index, nil
}

func BlueprintCandidate(b platform.PackageBlueprint, owner build.ConstructionDrafts, code []platform.ReleaseAsset) (platform.ReleaseCandidate, error) {
	assets, err := owner.CompleteBlueprintAssets(b.Assets)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	for i, asset := range assets {
		if asset.Ref.App != "build" {
			return platform.ReleaseCandidate{}, fmt.Errorf("a package blueprint contains only Build-owned definitions")
		}
		var err error
		assets[i], err = platform.WithDependencies(asset)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
	}
	return platform.Candidate(b.Roots, append(assets, code...))
}

func (d *Owner) sealedPackageDescriptor(id, version, artifact string) (platform.PackageDescriptor, error) {
	raw, err := d.host.GetPackageArtifact(ArtifactKey(d.tenant, id, version))
	if err != nil {
		return platform.PackageDescriptor{}, err
	}
	sealed := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sealed[:]) != artifact {
		return platform.PackageDescriptor{}, fmt.Errorf("the retained package descriptor content differs")
	}
	var descriptor platform.PackageDescriptor
	if json.Unmarshal(raw, &descriptor) != nil || descriptor.ID != id || descriptor.Version != version {
		return platform.PackageDescriptor{}, fmt.Errorf("the retained package descriptor is unavailable")
	}
	return descriptor, nil
}

func (d *Owner) BlueprintViews() []platform.PackageView {
	d.mu.Lock()
	var installed []platform.InstalledPackage
	for _, p := range d.packages {
		if p.State == "active" {
			installed = append(installed, *p)
		}
	}
	d.mu.Unlock()
	out := []platform.PackageView{}
	for _, p := range installed {
		descriptor, err := d.sealedPackageDescriptor(p.ID, p.Version, p.Artifact)
		if err != nil || descriptor.Blueprint == nil {
			continue
		}
		copy := p
		out = append(out, platform.PackageView{Descriptor: descriptor, Installed: &copy, Precheck: platform.Precheck{ID: p.ID, Version: p.Version, OK: true}, Source: "installed"})
	}
	slices.SortFunc(out, func(a, b platform.PackageView) int { return strings.Compare(a.Descriptor.ID, b.Descriptor.ID) })
	return out
}

func (d *Owner) packagesOf() map[string]*platform.InstalledPackage {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.packages)
}

func (d *Owner) packageIndex() (*platform.PackageIndex, error) {
	d.mu.Lock()
	bound := d.index
	d.mu.Unlock()
	if bound.Directory == "" {
		return d.sourceIndex(bound)
	}
	current, err := LoadPackageIndex(bound.Directory)
	if err != nil {
		return bound, err
	}
	return d.sourceIndex(current)
}

func (d *Owner) precheck(p platform.PackageDescriptor, index *platform.PackageIndex, planned ...[]platform.PackageDescriptor) platform.Precheck {
	out := platform.Precheck{ID: p.ID, Version: p.Version, OK: true}
	var proposed []platform.PackageDescriptor
	if len(planned) > 0 {
		proposed = planned[0]
	}
	if err := p.Validate(); err != nil {
		out.OK, out.Problems = false, []string{err.Error()}
		return out
	}
	if p.Blueprint == nil {
		out.OK = false
		out.Problems = append(out.Problems, "Marketplace installs product versions only; create a version from a saved release candidate")
		return out
	}
	if p.Blueprint != nil {
		var owner build.ConstructionDrafts
		for _, app := range d.host.Apps() {
			if app.Manifest().ID == "build" {
				owner, _ = app.(build.ConstructionDrafts)
				break
			}
		}
		if owner == nil {
			out.Problems = append(out.Problems, "application blueprints need the Build owner")
		} else {
			code, err := d.prerequisiteAssets(p, index, proposed)
			if err == nil {
				_, err = BlueprintCandidate(*p.Blueprint, owner, code)
			}
			if err != nil {
				out.Problems = append(out.Problems, err.Error())
			}
		}
	}
	if p.Compatibility != "" && !strings.HasPrefix(platformCompatibility, p.Compatibility) {
		out.Problems = append(out.Problems, fmt.Sprintf("needs platform %s, this host is %s", p.Compatibility, platformCompatibility))
	}
	installed := d.packagesOf()
	if err := d.verifyPackageVersion(p, installed[p.ID]); err != nil {
		out.Problems = append(out.Problems, err.Error())
	}
	for _, descriptor := range proposed {
		if descriptor.ID != p.ID {
			installed[descriptor.ID] = &platform.InstalledPackage{ID: descriptor.ID, Version: descriptor.Version, State: "active"}
		}
	}
	for _, requirement := range p.Requires {
		need, constraint := platform.PackageDependency(requirement)
		held, ok := installed[need]
		if !ok || held.State == "retired" {
			if _, offered := index.Latest(need); offered {
				out.Missing = append(out.Missing, need)
			} else {
				out.Problems = append(out.Problems, fmt.Sprintf("requires %s, which is not installed and not in the index", need))
			}
		} else if !platform.PackageVersionMatches(held.Version, constraint) {
			out.Missing = append(out.Missing, requirement)
			out.Problems = append(out.Problems, fmt.Sprintf("requires %s; installed version is %s", requirement, held.Version))
		} else if held.State != "active" {
			out.Missing = append(out.Missing, need)
			out.Problems = append(out.Problems, "A required package is preparing to uninstall")
		}
	}
	if held, ok := installed[p.ID]; ok && held.State != "retired" && held.Version == p.Version {
		out.Problems = append(out.Problems, fmt.Sprintf("version %s is already installed", p.Version))
	}
	views := d.contributionKeys()
	for _, c := range p.Contributions {
		key := p.Namespace + "." + c.View
		if owner, taken := views[key]; taken && owner != p.ID {
			out.Problems = append(out.Problems, fmt.Sprintf("view %s is already brought by %s", key, owner))
		}
	}
	if plan, err := d.packageComposition(p, index, proposed...); err != nil {
		out.Problems = append(out.Problems, err.Error())
	} else {
		out.Bindings = plan.Bindings
	}
	out.OK = len(out.Problems) == 0 && len(out.Missing) == 0
	return out
}

func (d *Owner) contributionKeys() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]string{}
	for id, p := range d.packages {
		if p.State == "retired" {
			continue
		}
		for _, c := range p.Contributions {
			out[p.Namespace+"."+c.View] = id
		}
	}
	return out
}

func ArtifactKey(tenant, id, version string) string {
	return "packages/" + tenant + "/" + id + "/" + version + ".json"
}

func (d *Owner) verifyPackageVersion(descriptor platform.PackageDescriptor, prior *platform.InstalledPackage) error {
	if prior == nil {
		return nil
	}
	artifact := ""
	if prior.Version == descriptor.Version {
		artifact = prior.Artifact
	} else if i := slices.IndexFunc(prior.Retained, func(r platform.RetainedArtifact) bool { return r.Version == descriptor.Version }); i >= 0 {
		artifact = prior.Retained[i].Digest
	} else {
		return nil
	}
	if artifact == "" {
		return fmt.Errorf("The original package has no sealed artifact; its contents cannot be verified")
	}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sum[:]) != artifact {
		return fmt.Errorf("A package version cannot be replaced; publish a new version")
	}
	_, err = d.sealedPackageDescriptor(descriptor.ID, descriptor.Version, artifact)
	return err
}

func (d *Owner) sealPackageBytes(descriptor platform.PackageDescriptor, directory string) (string, error) {
	if d.host == nil {
		return "", fmt.Errorf("the console is not composed with a tenant")
	}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	key := ArtifactKey(d.tenant, descriptor.ID, descriptor.Version)
	if err := d.host.PutPackageArtifact(key, raw); err != nil {
		return "", fmt.Errorf("seal %s@%s: %w", descriptor.ID, descriptor.Version, err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (d *Owner) verifySealedPackage(held platform.InstalledPackage) error {
	if held.Artifact == "" {
		return nil
	}
	key := ArtifactKey(d.tenant, held.ID, held.Version)
	raw, err := d.host.GetPackageArtifact(key)
	if err != nil {
		return fmt.Errorf("sealed artifact of %s@%s is missing: %w", held.ID, held.Version, err)
	}
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != held.Artifact {
		return fmt.Errorf("sealed artifact of %s@%s changed", held.ID, held.Version)
	}
	return nil
}

func latestRetained(retained []platform.RetainedArtifact) (platform.RetainedArtifact, bool) {
	if len(retained) == 0 {
		return platform.RetainedArtifact{}, false
	}
	latest := retained[0]
	for _, item := range retained[1:] {
		if !item.At.Before(latest.At) {
			latest = item
		}
	}
	return latest, true
}

func (d *Owner) decide(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	if s.GetSchema().GetName() == SchemaPackageImport || s.GetSchema().GetName() == SchemaPackageRemoveSource {
		return d.decidePackageSource(c, s)
	}
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		ID, Version string
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil || p.ID == "" {
		return nil, invalid
	}
	schema := s.GetSchema().GetName()
	if schema == SchemaPackageRetire || schema == SchemaPackageDrain {
		installed := d.packagesOf()
		held := installed[p.ID]
		if held == nil || held.State == "retired" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		state := "draining"
		removal := d.packageRemoval(p.ID)
		if schema == SchemaPackageDrain {
			removal.Blockers = d.packageDependents(p.ID)
			removal.OK = len(removal.Blockers) == 0
		}
		if !removal.OK {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", strings.Join(removal.Blockers, "; "))
		}
		if schema == SchemaPackageRetire {
			state = "retired"
		}
		var archive func(*pb.ChangeRecord)
		if schema == SchemaPackageRetire {
			var refusal *kernel.Error
			archive, refusal = d.host.PrepareManagedPackage(c, platform.PackageDescriptor{ID: p.ID}, nil, true, false, false)
			if refusal != nil {
				return nil, refusal
			}
		}
		return func(r *pb.ChangeRecord) {
			if archive != nil {
				archive(r)
			}
			held.State = state
			held.ChangedAt = time.Now().UTC()
		}, nil
	}
	index, indexErr := d.packageIndex()
	if indexErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package index cannot be read: {reason}", indexErr.Error())
	}
	rollback := schema == SchemaPackageRollback
	prior := d.packagesOf()[p.ID]
	var descriptor platform.PackageDescriptor
	var ok bool
	if rollback {
		if prior == nil || prior.State == "retired" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		previous, found := latestRetained(prior.Retained)
		if !found || previous.Version != p.Version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A rollback can only restore the immediately previous retained version")
		}
		sealed, err := d.sealedPackageDescriptor(p.ID, p.Version, previous.Digest)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained package artifact is unavailable: {reason}", err.Error())
		}
		descriptor, ok = sealed, true
		index = &platform.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
		index.Packages = append(index.Packages, descriptor)
	} else {
		descriptor, ok = index.Find(p.ID, p.Version)
	}
	if !ok && p.Version != "" {
		if prior != nil {
			artifact := ""
			if prior.Version == p.Version {
				artifact = prior.Artifact
			} else if i := slices.IndexFunc(prior.Retained, func(r platform.RetainedArtifact) bool { return r.Version == p.Version }); i >= 0 {
				artifact = prior.Retained[i].Digest
			}
			if artifact == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			sealed, err := d.sealedPackageDescriptor(p.ID, p.Version, artifact)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained package artifact is unavailable: {reason}", err.Error())
			}
			descriptor, ok = sealed, true
			index = &platform.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
			index.Packages = append(index.Packages, descriptor)
		}
	}
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if descriptor.Blueprint != nil && p.Version == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A versioned package requires its exact version")
	}
	pre := d.precheck(descriptor, index)
	if len(pre.Missing) > 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The package {package} needs {required} installed first", p.ID, strings.Join(pre.Missing, ", "))
	}
	if !pre.OK {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The package {package} cannot be installed: {reason}", p.ID, strings.Join(pre.Problems, "; "))
	}
	upgrade := schema == SchemaPackageUpgrade
	downgrade := schema == SchemaPackageDowngrade
	rollbackUpgrade := false
	if upgrade || downgrade {
		if prior == nil || prior.State == "retired" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		direction := platform.ComparePackageVersions(descriptor.Version, prior.Version)
		if upgrade && direction <= 0 || downgrade && direction >= 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The requested version is not an {direction} from the installed version", map[bool]string{true: "upgrade", false: "downgrade"}[upgrade])
		}
	} else if rollback {
		direction := platform.ComparePackageVersions(descriptor.Version, prior.Version)
		if direction == 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The retained version is already installed")
		}
		rollbackUpgrade = direction > 0
	} else if prior != nil && prior.State != "retired" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package {package} is already installed; upgrade it instead", p.ID)
	}
	if prior != nil && prior.Version != descriptor.Version {
		if err := d.verifySealedPackage(*prior); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package {package} cannot be installed: {reason}", p.ID, err.Error())
		}
	}
	sealed, err := d.sealPackageBytes(descriptor, index.Directory)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package {package} cannot be sealed: {reason}", p.ID, err.Error())
	}
	available, assetErr := d.prerequisiteAssets(descriptor, index, nil)
	if assetErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", assetErr.Error())
	}
	install, refusal := d.host.PrepareManagedPackage(c, descriptor, available, false, upgrade || rollbackUpgrade, downgrade || rollback && !rollbackUpgrade)
	if refusal != nil {
		return nil, refusal
	}
	return func(r *pb.ChangeRecord) {
		if install != nil {
			install(r)
		}
		now := time.Now().UTC()
		if d.packages == nil {
			d.packages = map[string]*platform.InstalledPackage{}
		}
		prior := d.packages[descriptor.ID]
		installed := &platform.InstalledPackage{ID: descriptor.ID, Version: descriptor.Version, Namespace: descriptor.Namespace,
			Title: descriptor.Title, State: "active", Contributions: descriptor.Contributions, Digest: descriptor.ArtifactDigest,
			Artifact: sealed, InstalledAt: now, ChangedAt: now}
		if prior != nil {
			installed.InstalledAt = prior.InstalledAt
			installed.Retained = slices.DeleteFunc(slices.Clone(prior.Retained), func(r platform.RetainedArtifact) bool {
				return r.Version == descriptor.Version || r.Version == prior.Version
			})
			if prior.Version != descriptor.Version {
				retained := prior.Artifact
				if retained == "" {
					retained = prior.Digest
				}
				installed.Retained = append(installed.Retained, platform.RetainedArtifact{Version: prior.Version, Digest: retained, At: now})
			}
		}
		d.packages[descriptor.ID] = installed
	}, nil
}

func (d *Owner) PackageViews() []platform.PackageView {
	out := []platform.PackageView{}
	index, indexErr := d.packageIndex()
	installed := d.packagesOf()
	products := map[string]platform.ProductVersionView{}
	if d.host != nil {
		for _, version := range d.host.ProductVersions() {
			products[version.Product+"@"+version.Version] = version
		}
	}
	listed := map[string]bool{}
	for _, descriptor := range index.Packages {
		if descriptor.Blueprint == nil {
			continue
		}
		pre := d.precheck(descriptor, index)
		if indexErr != nil {
			pre.OK = false
			pre.Problems = append(pre.Problems, indexErr.Error())
		}
		view := platform.PackageView{Descriptor: descriptor, Precheck: pre, Plan: d.installationPlan(descriptor, index), Source: "index"}
		if product, found := products[descriptor.ID+"@"+descriptor.Version]; found {
			view.Source, view.Product, view.Candidate = "product", product.Product, product.Candidate
		}
		d.mu.Lock()
		for digest, source := range d.packageSources {
			if source.ID == descriptor.ID && source.Version == descriptor.Version {
				view.Source, view.Origin, view.SourceDigest = "imported", source.Origin, digest
				break
			}
		}
		d.mu.Unlock()
		if held := installed[descriptor.ID]; held != nil {
			copy := *held
			view.Installed = &copy
			direction := platform.ComparePackageVersions(descriptor.Version, held.Version)
			view.Update = held.State == "active" && direction > 0
			view.Downgrade = held.State == "active" && direction < 0
			previous, found := latestRetained(held.Retained)
			view.Rollback = held.State == "active" && direction != 0 && found && previous.Version == descriptor.Version
			view.Removal = d.packageRemoval(descriptor.ID)
		}
		out = append(out, view)
		listed[descriptor.ID+"@"+descriptor.Version] = true
	}
	for _, held := range installed {
		if listed[held.ID+"@"+held.Version] {
			continue
		}
		descriptor := platform.PackageDescriptor{ID: held.ID, Version: held.Version, Title: held.Title, Namespace: held.Namespace, Contributions: held.Contributions}
		pre := platform.Precheck{ID: held.ID, Version: held.Version}
		if held.Artifact != "" {
			if sealed, err := d.sealedPackageDescriptor(held.ID, held.Version, held.Artifact); err == nil {
				descriptor = sealed
			} else {
				pre.Problems = append(pre.Problems, err.Error())
			}
		}
		sealedIndex := &platform.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
		sealedIndex.Packages = append(sealedIndex.Packages, descriptor)
		pre = d.precheck(descriptor, sealedIndex)
		copy := *held
		out = append(out, platform.PackageView{Descriptor: descriptor, Installed: &copy, Precheck: pre, Plan: d.installationPlan(descriptor, sealedIndex), Source: "installed", Removal: d.packageRemoval(held.ID)})
	}
	for _, held := range installed {
		if held.State != "active" {
			continue
		}
		for _, retained := range held.Retained {
			if listed[held.ID+"@"+retained.Version] {
				continue
			}
			descriptor, err := d.sealedPackageDescriptor(held.ID, retained.Version, retained.Digest)
			copy := *held
			view := platform.PackageView{Source: "retained", Installed: &copy, Descriptor: descriptor}
			view.Downgrade = err == nil && platform.ComparePackageVersions(retained.Version, held.Version) < 0
			previous, found := latestRetained(held.Retained)
			view.Rollback = err == nil && platform.ComparePackageVersions(retained.Version, held.Version) != 0 && found && previous.Version == retained.Version
			if err != nil {
				descriptor = platform.PackageDescriptor{ID: held.ID, Version: retained.Version, Namespace: held.Namespace, Title: held.Title}
				view.Descriptor = descriptor
				view.Precheck.Problems = []string{err.Error()}
			} else {
				sealedIndex := &platform.PackageIndex{Directory: index.Directory, Packages: slices.Clone(index.Packages)}
				sealedIndex.Packages = append(sealedIndex.Packages, descriptor)
				view.Descriptor, view.Precheck, view.Plan = descriptor, d.precheck(descriptor, sealedIndex), d.installationPlan(descriptor, sealedIndex)
			}
			listed[held.ID+"@"+retained.Version] = true
			out = append(out, view)
		}
	}
	slices.SortFunc(out, func(a, b platform.PackageView) int {
		if a.Descriptor.ID != b.Descriptor.ID {
			return strings.Compare(a.Descriptor.ID, b.Descriptor.ID)
		}
		return platform.ComparePackageVersions(a.Descriptor.Version, b.Descriptor.Version)
	})
	return out
}

func (d *Owner) Contributions() []platform.Contribution {
	out := []platform.Contribution{}
	if d.host != nil {
		for _, app := range d.host.Apps() {
			manifest := app.Manifest()
			for _, page := range manifest.Pages {
				out = append(out, platform.Contribution{Key: manifest.ID + "." + page.Name, View: page.Name, Title: page.Title,
					Kind: "view", App: manifest.ID, Source: manifest.ID, State: "active"})
			}
		}
	}
	d.mu.Lock()
	for _, p := range d.packages {
		if p.State == "retired" {
			continue
		}
		for _, c := range p.Contributions {
			surface := c.Surface
			if surface == "" {
				surface = "work"
			}
			kind := c.Kind
			if kind == "" {
				kind = "view"
			}
			out = append(out, platform.Contribution{Key: p.Namespace + "." + c.View, View: c.View, Surface: surface, Title: c.Title,
				Kind: kind, App: c.App, Source: p.ID, State: p.State})
		}
	}
	d.mu.Unlock()
	slices.SortFunc(out, func(a, b platform.Contribution) int { return strings.Compare(a.Key, b.Key) })
	return out
}
