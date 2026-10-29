package packages

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"maps"

	"os"
	"path/filepath"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/studio/build"
	"slices"
	"strings"
	"sync"
	"time"
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
	// PackageType is the decision target: a package's installed state.
	SchemaPackageInstall = "platform.package.install"
	SchemaPackageUpgrade = "platform.package.upgrade"
	SchemaPackageDrain   = "platform.package.drain"
	SchemaPackageRetire  = "platform.package.retire"
	// platformCompatibility is the API version packages must accept.
	platformCompatibility = "1"
)

// Actions are the console's controlled install, upgrade, drain and
// retire decisions: the index is precheck-only, so a refused package changes
// nothing.
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
		{Schema: SchemaPackageDrain, Target: platform.PackageType, Capability: "packages", Title: "Drain package",
			Description: "Stop new use of the package's contributions; work in flight finishes and history stays.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageRetire, Target: platform.PackageType, Capability: "packages", Title: "Retire package",
			Description: "Register nothing further from the package; its records and artifacts stay.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
	}
}

// LoadPackageIndex reads every *.json descriptor of a directory, or one JSON
// document with an array of descriptors when the path is a URL's file name.
// The online case uses the same reader: a mirror is a directory of JSON files.
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
		return strings.Compare(a.Version, b.Version)
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

// sealedPackageDescriptor reads back the exact descriptor bytes a tenant sealed.
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

// packagesOf is the tenant's installed map, when the console runs it.
func (d *Owner) packagesOf() map[string]*platform.InstalledPackage {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.packages == nil {
		d.packages = map[string]*platform.InstalledPackage{}
	}
	return d.packages
}

// A fresh index belongs to this call. The shared deployment index is never
// replaced by a reader; in-memory indexes keep their original behavior.
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

func (d *Owner) precheck(p platform.PackageDescriptor, index *platform.PackageIndex) platform.Precheck {
	out := platform.Precheck{ID: p.ID, Version: p.Version, OK: true}
	if err := p.Validate(); err != nil {
		out.OK, out.Problems = false, []string{err.Error()}
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
			code, err := d.host.CodeReleaseAssets()
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
	for _, need := range p.Requires {
		held, ok := installed[need]
		if !ok || held.State == "retired" {
			if _, offered := index.Latest(need); offered {
				out.Missing = append(out.Missing, need)
			} else {
				out.Problems = append(out.Problems, fmt.Sprintf("requires %s, which is not installed and not in the index", need))
			}
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
	if plan, err := d.packageComposition(p, index); err != nil {
		out.Problems = append(out.Problems, err.Error())
	} else {
		out.Bindings = plan.Bindings
	}
	out.OK = len(out.Problems) == 0 && len(out.Missing) == 0
	return out
}

// contributionKeys maps every live namespaced contribution key to its owner.
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

// ArtifactKey is where one package version's sealed bytes live.
func ArtifactKey(tenant, id, version string) string {
	return "packages/" + tenant + "/" + id + "/" + version + ".json"
}

// Installation state can change; an accepted version's contents cannot.
// Check the current and retained versions alike, including packages without a runtime.
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

// sealPackageBytes writes a descriptor into the tenant's artifact store and
// returns the digest of the sealed bytes. Sealing is idempotent: the same
// descriptor seals to the same key and digest, so a retried install writes the
// same bytes. The sealed artifact is what an upgrade verifies before it
// replaces the version, and what a retained version keeps.
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

// verifySealedPackage reads an installed version's sealed bytes back and checks
// them against the digest it was installed with. A changed or missing artifact
// refuses the upgrade, so the running version is kept rather than replaced on
// bytes nobody can prove.
func (d *Owner) verifySealedPackage(held platform.InstalledPackage) error {
	if held.Artifact == "" {
		return nil // installed before sealing existed: nothing to verify
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

// decide applies the console's package decisions. It returns the closure
// shape the console's other decisions use.
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

		return func(*pb.ChangeRecord) {
			held.State = state
			held.ChangedAt = time.Now().UTC()
		}, nil
	}
	index, indexErr := d.packageIndex()
	if indexErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package index cannot be read: {reason}", indexErr.Error())
	}
	descriptor, ok := index.Find(p.ID, p.Version)
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
	prior := d.packagesOf()[p.ID]
	if upgrade {
		if prior == nil || prior.State == "retired" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
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
	// The apply closure runs under the console's lock (Console.Submit), so it
	// reads the map directly: packagesOf would lock it again.
	return func(*pb.ChangeRecord) {
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

// PackageViews lists the index beside what the tenant holds.
func (d *Owner) PackageViews() []platform.PackageView {
	out := []platform.PackageView{}
	index, indexErr := d.packageIndex()
	installed := d.packagesOf()
	listed := map[string]bool{}
	for _, descriptor := range index.Packages {
		pre := d.precheck(descriptor, index)
		if indexErr != nil {
			pre.OK = false
			pre.Problems = append(pre.Problems, indexErr.Error())
		}
		view := platform.PackageView{Descriptor: descriptor, Precheck: pre, Source: "index"}
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
		pre := platform.Precheck{ID: held.ID, Version: held.Version, Problems: []string{"This installed version is not in the deployment index and cannot be installed from this list."}}
		if held.Artifact != "" {
			if sealed, err := d.sealedPackageDescriptor(held.ID, held.Version, held.Artifact); err == nil {
				descriptor = sealed
			} else {
				pre.Problems = append(pre.Problems, err.Error())
			}
		}
		copy := *held
		out = append(out, platform.PackageView{Descriptor: descriptor, Installed: &copy, Precheck: pre, Source: "installed", Removal: d.packageRemoval(held.ID)})
	}
	slices.SortFunc(out, func(a, b platform.PackageView) int {
		if a.Descriptor.ID != b.Descriptor.ID {
			return strings.Compare(a.Descriptor.ID, b.Descriptor.ID)
		}
		return strings.Compare(a.Descriptor.Version, b.Descriptor.Version)
	})
	return out
}

// Contributions lists this tenant's namespaced contributions: every app's
// declared views and every active package's.
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

// DecodeContent reads the reviewed JSON of a platform package: one
// installable descriptor, without embedded native bytes.
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
		return strings.Compare(a.Version, b.Version)
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
		if slices.Contains(descriptor.Requires, id) {
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
	for _, id := range p.Requires {
		module.Requires = append(module.Requires, Contract(id))
	}
	return module
}

func (d *Owner) packageComposition(candidate platform.PackageDescriptor, index *platform.PackageIndex) (platform.CompositionPlan, error) {
	modules := d.host.BaseModules()
	installed := d.packagesOf()
	for id, held := range installed {
		if id == candidate.ID || held.State != "active" {
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
				err = fmt.Errorf("installed package %s has no retained declaration", id)
			}
		}
		if err != nil {
			return platform.CompositionPlan{}, err
		}
		modules = append(modules, Module(descriptor))
	}
	modules = append(modules, Module(candidate))
	return platform.ResolveComposition(modules, nil)
}

type Catalog interface{ BindPackageIndex(*platform.PackageIndex) }

// Precheck tells whether this tenant may install a descriptor now.
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

// Preparation is a read projection of sealed package content and its original
// owner records. It is not an installation, execution plan or second registry.
type PackagePreparation struct {
	Package           string                   `json:"package"`
	Version           string                   `json:"version"`
	Applications      []PackageApplication     `json:"applications"`
	Connections       []PackageConnection      `json:"connections"`
	AI                []PackageAIRequirement   `json:"ai"`
	Roles             []PackageRoleRequirement `json:"roles"`
	Setup             *platform.PackageSetup   `json:"setup,omitempty"`
	Installed         bool                     `json:"installed"`
	InstalledVersion  string                   `json:"installedVersion,omitempty"`
	InstallationState string                   `json:"installationState,omitempty"`
}

type PackageApplication struct {
	Ref       platform.AssetRef `json:"ref"`
	Title     string            `json:"title"`
	DraftID   string            `json:"draftId,omitempty"`
	State     string            `json:"state,omitempty"`
	Published bool              `json:"published"`
	Pages     []string          `json:"pages"`
}

type PackageConnection struct {
	Ref        platform.AssetRef `json:"ref"`
	Title      string            `json:"title"`
	DraftID    string            `json:"draftId,omitempty"`
	Kind       string            `json:"kind"`
	Credential bool              `json:"credential"`
	Configured bool              `json:"configured"`
	Ready      bool              `json:"ready"`
}

type PackageAIRequirement struct {
	Problem string            `json:"problem,omitempty"`
	Ref     platform.AssetRef `json:"ref"`
	DraftID string            `json:"draftId,omitempty"`
	Model   string            `json:"model,omitempty"`
	Ready   bool              `json:"ready"`
}

type PackageRoleRequirement struct {
	App       string `json:"app"`
	Role      string `json:"role"`
	Held      bool   `json:"held"`
	Available bool   `json:"available"`
}

type DraftState struct {
	platform.Record
	Name, Title, State, Address, Secret, Published, Model string
	ModelOptions                                          platform.ModelOptions `json:"modelOptions,omitempty"`
	MaxTokens                                             int                   `json:"maxTokens,omitempty"`
	Pages                                                 []string
}

// Preparation reads captured owner declarations and member-filtered drafts.
func Preparation(id, version string, blueprint platform.PackageBlueprint, installed *platform.InstalledPackage, candidate platform.ReleaseCandidate, m platform.Member, source PreparationSource) (PackagePreparation, error) {
	out := PackagePreparation{Package: id, Version: version, Applications: []PackageApplication{}, Connections: []PackageConnection{}, AI: []PackageAIRequirement{}, Roles: []PackageRoleRequirement{}, Setup: blueprint.Setup}
	if installed != nil {
		out.InstalledVersion, out.InstallationState = installed.Version, installed.State
		out.Installed = installed.Version == version && installed.State == "active"
	}
	role := func(app, name string) {
		if name == "" || slices.ContainsFunc(out.Roles, func(r PackageRoleRequirement) bool { return r.App == app && r.Role == name }) {
			return
		}
		owner := source.App(app)
		out.Roles = append(out.Roles, PackageRoleRequirement{App: app, Role: name, Held: m.Holds(app, name), Available: owner != nil && slices.Contains(owner.Manifest().AllRoles(), name)})
	}
	for _, root := range blueprint.Roots {
		if root.Kind != platform.AssetApp {
			continue
		}
		artifact, err := platform.ArtifactOfApplication(root, candidate.Assets)
		if err != nil {
			return PackagePreparation{}, err
		}
		state, err := source.Find(build.AppType, root.Name)
		if err != nil {
			return PackagePreparation{}, err
		}
		var pages []string
		for _, asset := range candidate.Assets {
			if asset.Ref == root {
				var app platform.Application
				if err := json.Unmarshal(asset.Body, &app); err != nil {
					return PackagePreparation{}, err
				}
				pages = app.Pages
			}
		}
		out.Applications = append(out.Applications, PackageApplication{Ref: root, Title: artifact.Title, DraftID: state.ID, State: state.State, Published: state.Published != "", Pages: pages})
		for _, r := range artifact.Roles {
			role(r.Resource.Ref.App, r.Role)
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetConnection:
			var def build.ConnectionDefinition
			if err := json.Unmarshal(asset.Body, &def); err != nil {
				return PackagePreparation{}, err
			}
			state, err := source.Find(build.ConnectionType, asset.Ref.Name)
			if err != nil {
				return PackagePreparation{}, err
			}
			out.Connections = append(out.Connections, PackageConnection{Ref: asset.Ref, Title: def.Title, DraftID: state.ID, Kind: def.Kind, Credential: def.Credential, Configured: state.Address != "" && (!def.Credential || state.Secret != ""), Ready: state.State == "ready" && (!def.Credential || state.Secret != "")})
		case platform.AssetAgent, platform.AssetFunction, platform.AssetEvaluation:
			typ := build.FunctionType
			if asset.Ref.Kind == platform.AssetAgent {
				typ = "build.agent"
			} else if asset.Ref.Kind == platform.AssetEvaluation {
				typ = build.TestPlanType
			}
			state, err := source.Find(typ, asset.Ref.Name)
			if err != nil {
				return PackagePreparation{}, err
			}
			var def struct {
				Model        string                `json:"model"`
				ModelOptions platform.ModelOptions `json:"modelOptions,omitempty"`
				MaxTokens    int                   `json:"maxTokens,omitempty"`
			}
			if err := json.Unmarshal(asset.Body, &def); err != nil {
				return PackagePreparation{}, err
			}
			model, options, tokens := def.Model, def.ModelOptions, def.MaxTokens
			if state.ID != "" {
				model, options, tokens = state.Model, state.ModelOptions, state.MaxTokens
			}
			purpose := "generation"
			if asset.Ref.Kind == platform.AssetAgent {
				purpose = "agent"
			}
			if asset.Ref.Kind == platform.AssetFunction {
				options = options.Override(platform.ModelOptions{MaxTokens: tokens})
			}
			resolved, problem := source.Model(purpose, model, options)
			ready := problem == ""
			if ready {
				model = resolved
			}

			out.AI = append(out.AI, PackageAIRequirement{Ref: asset.Ref, DraftID: state.ID, Model: model, Ready: ready, Problem: problem})
		}
	}
	if out.Setup != nil {
		for _, r := range out.Setup.Roles {
			role(r.App, r.Role)
		}
	}
	slices.SortFunc(out.Roles, func(a, b PackageRoleRequirement) int { return strings.Compare(a.App+"/"+a.Role, b.App+"/"+b.Role) })
	return out, nil
}

type PreparationSource struct {
	Find  func(string, string) (DraftState, error)
	App   func(string) platform.App
	Model func(string, string, platform.ModelOptions) (string, string)
}

func Inspect(ctx context.Context, tenant string, request platform.PackageInspectRequest, send func(*http.Request, bool) (*http.Response, error), files platform.FileStore, check func(context.Context, platform.PackageDescriptor) (platform.Precheck, error)) (platform.PackagePreview, error) {
	var preview platform.PackagePreview
	content, origin := []byte(request.Content), "local-file"
	if (request.URL == "") == (len(content) == 0) {
		return preview, fmt.Errorf("Choose one package source: a URL or a local package file")
	}
	if request.URL != "" {
		address, err := url.Parse(request.URL)
		if err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || address.Fragment != "" {
			return preview, fmt.Errorf("Use an HTTPS address for a platform package, without embedded credentials")
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
		if err != nil {
			return preview, err
		}
		req.Header.Set("Accept", "application/json")
		response, err := send(req, false)
		if err != nil {
			return preview, fmt.Errorf("The package could not be downloaded; check its address and try again")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return preview, fmt.Errorf("The package source answered HTTP %d", response.StatusCode)
		}
		content, err = io.ReadAll(io.LimitReader(response.Body, MaxPackageImportBytes+1))
		if err != nil {
			return preview, fmt.Errorf("The package download could not be read")
		}
		address.RawQuery, address.ForceQuery = "", false
		origin = address.String()
	}
	if len(content) == 0 || len(content) > MaxPackageImportBytes {
		return preview, fmt.Errorf("A platform package must be non-empty and at most 16 MiB")
	}
	descriptor, err := DecodeContent(content)
	if err != nil {
		return preview, err
	}
	if err := descriptor.Validate(); err != nil {
		return preview, err
	}
	precheck, err := check(ctx, descriptor)
	if err != nil {
		return preview, err
	}
	raw, err := json.Marshal(platform.Offer{Descriptor: descriptor, Origin: origin})
	if err != nil {
		return platform.PackagePreview{}, err
	}
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err = files.Put(ctx, OfferKey(tenant, digest), raw, "application/json"); err != nil {
		return platform.PackagePreview{}, err
	}
	return platform.PackagePreview{Descriptor: descriptor, Digest: digest, Origin: origin, Precheck: precheck}, nil

}
