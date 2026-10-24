package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/driver"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Packages and contributions (ordered 2026-10-05).
//
// A package declares its identity, immutable version, the capabilities and
// contributions it brings, its required dependencies, its namespace and its
// compatibility. Installing is a controlled decision of the platform console:
// precheck first (required dependencies, compatibility, namespace conflicts),
// then register. An upgrade replaces the version and keeps the retired
// artifact; draining stops new use of a contribution while work in flight
// finishes; retiring keeps history and artifacts and registers nothing. The
// same sealed version can be installed again without changing its contents.
// Installation never grants business permissions: a package asks for
// capabilities, and the tenant's own authorization still decides.
//
// Contributions carry a namespace: a view is addressed "<namespace>.<view>",
// so two packages may both bring a "board" without colliding, and a package
// from the online index installs through the same precheck and the same
// decisions as a local one.

// PackageContribution is one view or capability a package brings.
type PackageContribution struct {
	View    string `json:"view"`              // the view's own name, e.g. "board"
	Surface string `json:"surface,omitempty"` // work, studio or tenant; default work
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"` // view (default) or action
	App     string `json:"app,omitempty"`  // the app whose records the view reads
}

// PackageDescriptor is one installable package, as the index declares it.
type PackageDescriptor struct {
	Blueprint      *PackageBlueprint             `json:"blueprint,omitempty"`
	Runtime        *platform.DriverRuntime       `json:"runtime,omitempty"`
	ID             string                        `json:"id"`
	Version        string                        `json:"version"`
	Title          string                        `json:"title,omitempty"`
	Namespace      string                        `json:"namespace"`
	Requires       []string                      `json:"requires,omitempty"`     // package IDs
	Capabilities   []platform.CapabilityContract `json:"capabilities,omitempty"` // required platform contracts
	Compatibility  string                        `json:"compatibility,omitempty"`
	Contributions  []PackageContribution         `json:"contributions,omitempty"`
	ArtifactDigest string                        `json:"artifactDigest,omitempty"` // sha256 over the descriptor
	Description    string                        `json:"description,omitempty"`
}

// PackageBlueprint contains the original owners' portable definitions. It
// creates drafts only; publication and environment credentials stay separate.
type PackageBlueprint struct {
	Roots  []platform.AssetRef     `json:"roots"`
	Assets []platform.ReleaseAsset `json:"assets"`
	Guide  string                  `json:"guide,omitempty"`
	Setup  *PackageSetup           `json:"setup,omitempty"`
}

// PackageSetup is reviewed client input, not an installed action or worker.
// Each request still needs the person's current authorization at its owner.
type PackageSetup struct {
	Roles []PackageSetupRole `json:"roles"`
	Steps []PackageSetupStep `json:"steps"`
}
type PackageSetupRole struct {
	App  string `json:"app"`
	Role string `json:"role"`
}
type PackageSetupStep struct {
	Name    string        `json:"name"`
	App     string        `json:"app"`
	Schema  string        `json:"schema"`
	Target  *pb.EntityRef `json:"target"`
	Payload string        `json:"payload"`
}

func (p PackageSetup) check() error {
	if len(p.Steps) == 0 || len(p.Steps) > 128 || len(p.Roles) > 32 {
		return fmt.Errorf("a setup plan needs 1–128 requests and at most 32 role requirements")
	}
	for _, role := range p.Roles {
		if role.App == "" || role.Role == "" || len(role.App) > 128 || len(role.Role) > 128 {
			return fmt.Errorf("setup role requirements need an app and role")
		}
	}
	seen := map[string]bool{}
	for _, step := range p.Steps {
		if step.Name == "" || len(step.Name) > 128 || seen[step.Name] || step.App == "" ||
			!strings.HasPrefix(step.Schema, step.App+".") || step.Target.GetType() == "" || step.Target.GetId() == "" ||
			len(step.Payload) > 64<<10 || !json.Valid([]byte(step.Payload)) {
			return fmt.Errorf("setup requests need unique names, an owner, schema, target and bounded JSON payload")
		}
		seen[step.Name] = true
	}
	return nil
}

// InstalledPackage is the tenant's state for one installed package.
type InstalledPackage struct {
	Runtime       *platform.DriverRuntime `json:"runtime,omitempty"`
	ID            string                  `json:"id"`
	Version       string                  `json:"version"`
	Namespace     string                  `json:"namespace"`
	Title         string                  `json:"title,omitempty"`
	State         string                  `json:"state"` // active, draining, retired
	Contributions []PackageContribution   `json:"contributions,omitempty"`
	Digest        string                  `json:"digest,omitempty"`   // the index's artifact digest, when it declares one
	Artifact      string                  `json:"artifact,omitempty"` // sha256 over the bytes sealed in this tenant's store
	InstalledAt   time.Time               `json:"installedAt,omitempty"`
	ChangedAt     time.Time               `json:"changedAt,omitempty"`
	// Retained are inactive versions this tenant installed: their artifacts stay.
	Retained []RetainedArtifact `json:"retained,omitempty"`
}

// RetainedArtifact is one replaced version whose artifact the tenant keeps.
type RetainedArtifact struct {
	Version string    `json:"version"`
	Digest  string    `json:"digest,omitempty"`
	At      time.Time `json:"at"`
}

// Precheck is the read a person sees before installing.
type Precheck struct {
	ID       string                       `json:"id"`
	Version  string                       `json:"version"`
	OK       bool                         `json:"ok"`
	Problems []string                     `json:"problems,omitempty"`
	Missing  []string                     `json:"missing,omitempty"`
	Bindings []platform.CapabilityBinding `json:"bindings,omitempty"`
}

// PackageView is one package as the console lists it: what the index offers and
// what the tenant has.
type PackageView struct {
	Origin       string            `json:"origin,omitempty"`
	SourceDigest string            `json:"sourceDigest,omitempty"`
	Removal      *PackageRemoval   `json:"removal,omitempty"`
	Descriptor   PackageDescriptor `json:"descriptor"`
	Installed    *InstalledPackage `json:"installed,omitempty"`
	Precheck     Precheck          `json:"precheck"`
	Source       string            `json:"source"` // index or installed; an installed record is not an installation offer
}

// Contribution is one namespaced contribution of this tenant: built-in app
// contributions and installed packages'. The shell addresses it by Key.
type Contribution struct {
	Key     string `json:"key"` // "<namespace>.<view>"
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
	Source  string `json:"source"` // the app or package that brings it
	State   string `json:"state"`  // active or draining
}

const (
	// PackageType is the decision target: a package's installed state.
	PackageType          = "platform.package"
	SchemaPackageInstall = "platform.package.install"
	SchemaPackageUpgrade = "platform.package.upgrade"
	SchemaPackageDrain   = "platform.package.drain"
	SchemaPackageRetire  = "platform.package.retire"
	// platformCompatibility is the API version packages must accept.
	platformCompatibility = "1"
)

// PackageActions are the console's controlled install, upgrade, drain and
// retire decisions: the index is precheck-only, so a refused package changes
// nothing.
func PackageActions() []platform.Action {
	admin := []string{Admin}
	fields := []platform.Field{{Name: "id", Type: "string", Required: true, Description: "The package's ID"},
		{Name: "version", Type: "string", Description: "The version; empty: the newest the index offers"}}
	return []platform.Action{
		{Schema: SchemaPackageCleanupConfirm, Target: PackageType, Capability: "packages", Title: "Confirm resource cleanup", Description: "Keep a verified technical cleanup receipt for this exact installation; no business facts are removed.", Payload: []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "digest", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageImport, Target: PackageType, Capability: "packages", Title: "Add reviewed package", Description: "Keep exactly the reviewed package in this tenant's available library; install separately.", Payload: []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "digest", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageRemoveSource, Target: PackageType, Capability: "packages", Title: "Remove package source", Description: "Remove an uninstalled source from the library; installed artifacts and business history stay.", Payload: []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "digest", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageInstall, Target: PackageType, Capability: "packages", Title: "Install package",
			Description: "Precheck a package's requirements and register the contributions it brings.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageUpgrade, Target: PackageType, Capability: "packages", Title: "Upgrade package",
			Description: "Replace an installed package's version, keeping the artifact it replaced.", Payload: fields, Roles: admin},
		{Schema: SchemaPackageDrain, Target: PackageType, Capability: "packages", Title: "Drain package",
			Description: "Stop new use of the package's contributions; work in flight finishes and history stays.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
		{Schema: SchemaPackageRetire, Target: PackageType, Capability: "packages", Title: "Retire package",
			Description: "Register nothing further from the package; its records and artifacts stay.",
			Payload:     []platform.Field{{Name: "id", Type: "string", Required: true}}, Roles: admin},
	}
}

// PackageIndex is the deployment's trusted directory or an in-memory index.
// Console reads a directory afresh for each listing and installation decision.
type PackageIndex struct {
	Packages  []PackageDescriptor
	Directory string
}

// LoadPackageIndex reads every *.json descriptor of a directory, or one JSON
// document with an array of descriptors when the path is a URL's file name.
// The online case uses the same reader: a mirror is a directory of JSON files.
func LoadPackageIndex(dir string) (*PackageIndex, error) {
	index := &PackageIndex{Directory: dir}
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
		var list []PackageDescriptor
		if err := json.Unmarshal(raw, &list); err != nil {
			var one PackageDescriptor
			if err := json.Unmarshal(raw, &one); err != nil {
				return nil, fmt.Errorf("package index %s: %w", entry.Name(), err)
			}
			list = []PackageDescriptor{one}
		}
		for _, p := range list {
			if err := p.validate(); err != nil {
				return nil, fmt.Errorf("package index %s: %w", entry.Name(), err)
			}
			index.Packages = append(index.Packages, p)
		}
	}
	slices.SortFunc(index.Packages, func(a, b PackageDescriptor) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Version, b.Version)
	})
	return index, nil
}

func (p PackageDescriptor) validate() error {
	if p.ID == "" || p.Version == "" || p.Namespace == "" {
		return fmt.Errorf("a package needs an ID, a version and a namespace")
	}
	for _, value := range []string{p.ID, p.Namespace, p.Version} {
		if len(value) > 200 || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
			return fmt.Errorf("package ID, namespace and version must be safe path segments")
		}
	}
	for _, c := range p.Contributions {
		if c.View == "" {
			return fmt.Errorf("package %s: a contribution needs a view", p.ID)
		}
	}
	for _, need := range p.Capabilities {
		if err := need.Check(); err != nil {
			return err
		}
	}
	if p.Runtime != nil {
		if err := p.Runtime.Check(); err != nil {
			return err
		}
	}
	if p.Blueprint != nil {
		return p.Blueprint.check()
	}
	return nil
}

func (b PackageBlueprint) check() error {
	if len(b.Roots) == 0 || len(b.Assets) == 0 || len(b.Assets) > 1000 {
		return fmt.Errorf("a blueprint needs roots and at most 1000 owner definitions")
	}
	if b.Setup != nil {
		if err := b.Setup.check(); err != nil {
			return err
		}
	}
	for _, root := range b.Roots {
		if root.Check() != nil || root.App != "build" {
			return fmt.Errorf("a package blueprint has only Build-owned roots")
		}
	}
	seen := map[platform.AssetRef]bool{}
	for _, asset := range b.Assets {
		if seen[asset.Ref] || asset.Ref.App != "build" || asset.ContractVersion < 1 || asset.SourceVersion == "" {
			return fmt.Errorf("blueprint owner definitions need unique pinned identities")
		}
		seen[asset.Ref] = true
		if _, err := platform.WithDependencies(asset); err != nil {
			return err
		}
	}
	return nil
}

func (b PackageBlueprint) candidate(owner host.ConstructionDrafts, code []platform.ReleaseAsset) (platform.ReleaseCandidate, error) {
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

// latest picks the newest version of a package by version string order.
func (i *PackageIndex) latest(id string) (PackageDescriptor, bool) {
	var out PackageDescriptor
	found := false
	for _, p := range i.Packages {
		if p.ID == id && (!found || strings.Compare(p.Version, out.Version) > 0) {
			out, found = p, true
		}
	}
	return out, found
}

// find picks one exact version.
func (i *PackageIndex) find(id, version string) (PackageDescriptor, bool) {
	for _, p := range i.Packages {
		if p.ID == id && (version == "" || p.Version == version) {
			return p, true
		}
	}
	return PackageDescriptor{}, false
}

func packageBinaryKey(tenant, id, version, digest string) string {
	return "packages/" + tenant + "/" + id + "/" + version + "/" + strings.TrimPrefix(digest, "sha256:") + ".bin"
}

// ResolveDriver uses this deployment's existing trusted package index. It is
// also the gateway's artifact resolver; bindings cannot supply program paths.
func (i *PackageIndex) ResolveDriver(id, version, digest, profile string) (platform.DriverRuntime, error) {
	p, found := i.find(id, version)
	if !found || version == "" || p.Runtime == nil {
		return platform.DriverRuntime{}, fmt.Errorf("the exact protocol package is unavailable")
	}
	if err := checkDriverRuntime(*p.Runtime, digest, profile); err != nil {
		return platform.DriverRuntime{}, err
	}
	target, err := p.Runtime.Target()
	if err != nil || strings.TrimPrefix(target.Artifact, "sha256:") != strings.TrimPrefix(digest, "sha256:") {
		return platform.DriverRuntime{}, fmt.Errorf("this protocol binary does not target the gateway operating system and architecture")
	}
	if _, _, err := driver.Binary(i.Directory, digest); err != nil {
		return platform.DriverRuntime{}, err
	}
	return *p.Runtime, nil
}
func checkDriverRuntime(r platform.DriverRuntime, digest, profile string) error {
	if err := r.Check(); err != nil {
		return err
	}
	if !slices.ContainsFunc(r.Targets, func(target platform.DriverTarget) bool {
		return strings.TrimPrefix(target.Artifact, "sha256:") == strings.TrimPrefix(digest, "sha256:")
	}) {
		return fmt.Errorf("the protocol binary digest differs from the selected package")
	}
	if !slices.ContainsFunc(r.Profiles, func(p platform.DriverProfile) bool { return p.ID == profile }) {
		return fmt.Errorf("the protocol package does not provide this profile")
	}
	return nil
}

func (d *Console) ProtocolPackages() []platform.DriverPackage {
	d.mu.Lock()
	installed := make([]InstalledPackage, 0, len(d.packages))
	for _, p := range d.packages {
		if p.State == "active" || p.State == "draining" {
			copy := *p
			copy.Retained = slices.Clone(p.Retained)
			installed = append(installed, copy)
		}
	}
	d.mu.Unlock()
	out := []platform.DriverPackage{}
	for _, p := range installed {
		appendVersion := func(version, artifact, state string) {
			descriptor, err := d.driverDescriptor(p.ID, version, artifact)
			if err != nil {
				return
			}
			for _, target := range descriptor.Runtime.Targets {
				out = append(out, platform.DriverPackage{ID: p.ID, Version: version, Title: descriptor.Title, Digest: target.Artifact, State: state, Runtime: *descriptor.Runtime})
			}
		}
		if p.Runtime != nil {
			appendVersion(p.Version, p.Artifact, p.State)
		}
		for _, retained := range p.Retained {
			state := "retained"
			if p.State == "draining" {
				state = "draining"
			}
			appendVersion(retained.Version, retained.Digest, state)
		}
	}
	slices.SortFunc(out, func(a, b platform.DriverPackage) int {
		if a.ID != b.ID {
			return strings.Compare(a.ID, b.ID)
		}
		return strings.Compare(a.Version, b.Version)
	})
	return out
}

func (d *Console) sealedPackageDescriptor(id, version, artifact string) (PackageDescriptor, error) {
	raw, err := d.host.GetPackageArtifact(packageArtifactKey(d.tenant, id, version))
	if err != nil {
		return PackageDescriptor{}, err
	}
	sealed := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sealed[:]) != artifact {
		return PackageDescriptor{}, fmt.Errorf("the retained package descriptor content differs")
	}
	var descriptor PackageDescriptor
	if json.Unmarshal(raw, &descriptor) != nil || descriptor.ID != id || descriptor.Version != version {
		return PackageDescriptor{}, fmt.Errorf("the retained package descriptor is unavailable")
	}
	return descriptor, nil
}

func (d *Console) driverDescriptor(id, version, artifact string) (PackageDescriptor, error) {
	descriptor, err := d.sealedPackageDescriptor(id, version, artifact)
	if err != nil {
		return descriptor, err
	}
	if descriptor.Runtime == nil {
		return descriptor, fmt.Errorf("the package has no protocol runtime")
	}
	return descriptor, nil
}

func (d *Console) blueprintViews() []PackageView {
	d.mu.Lock()
	var installed []InstalledPackage
	for _, p := range d.packages {
		if p.State == "active" {
			installed = append(installed, *p)
		}
	}
	d.mu.Unlock()
	out := []PackageView{}
	for _, p := range installed {
		descriptor, err := d.sealedPackageDescriptor(p.ID, p.Version, p.Artifact)
		if err != nil || descriptor.Blueprint == nil {
			continue
		}
		copy := p
		out = append(out, PackageView{Descriptor: descriptor, Installed: &copy, Precheck: Precheck{ID: p.ID, Version: p.Version, OK: true}, Source: "installed"})
	}
	slices.SortFunc(out, func(a, b PackageView) int { return strings.Compare(a.Descriptor.ID, b.Descriptor.ID) })
	return out
}

// Retained versions remain resolvable for the original in-flight binding.
// Authoring selects only active ProtocolPackages; it cannot grant installation.
func (d *Console) ResolveDriver(id, version, digest, profile string) (platform.DriverRuntime, error) {
	d.mu.Lock()
	held := d.packages[id]
	artifact := ""
	if held != nil {
		if held.Version == version {
			artifact = held.Artifact
		}
		for _, retained := range held.Retained {
			if retained.Version == version {
				artifact = retained.Digest
				break
			}
		}
	}
	d.mu.Unlock()
	if artifact == "" || version == "" {
		return platform.DriverRuntime{}, fmt.Errorf("the exact protocol package has not been installed")
	}
	descriptor, err := d.driverDescriptor(id, version, artifact)
	if err != nil {
		return platform.DriverRuntime{}, err
	}
	if err = checkDriverRuntime(*descriptor.Runtime, digest, profile); err != nil {
		return platform.DriverRuntime{}, err
	}
	binary, err := d.host.GetPackageArtifact(packageBinaryKey(d.tenant, id, version, digest))
	if err != nil {
		return platform.DriverRuntime{}, err
	}
	sum := sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != strings.TrimPrefix(digest, "sha256:") {
		return platform.DriverRuntime{}, fmt.Errorf("the retained protocol binary content differs")
	}
	return *descriptor.Runtime, nil
}

// packagesOf is the tenant's installed map, when the console runs it.
func (d *Console) packagesOf() map[string]*InstalledPackage {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.packages == nil {
		d.packages = map[string]*InstalledPackage{}
	}
	return d.packages
}

// Precheck tells whether this tenant may install a descriptor now.
func (d *Console) Precheck(p PackageDescriptor) Precheck {
	index, err := d.packageIndex()
	if err != nil {
		return Precheck{ID: p.ID, Version: p.Version, Problems: []string{err.Error()}}
	}
	if current, found := index.find(p.ID, p.Version); found {
		p = current
	}
	return d.precheck(p, index)
}

// A fresh index belongs to this call. The shared deployment index is never
// replaced by a reader; in-memory indexes keep their original behavior.
func (d *Console) packageIndex() (*PackageIndex, error) {
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

func (d *Console) precheck(p PackageDescriptor, index *PackageIndex) Precheck {
	out := Precheck{ID: p.ID, Version: p.Version, OK: true}
	if err := p.validate(); err != nil {
		out.OK, out.Problems = false, []string{err.Error()}
		return out
	}
	if p.Blueprint != nil {
		var owner host.ConstructionDrafts
		for _, app := range d.host.Apps() {
			if app.Manifest().ID == "build" {
				owner, _ = app.(host.ConstructionDrafts)
				break
			}
		}
		if owner == nil {
			out.Problems = append(out.Problems, "application blueprints need the Build owner")
		} else {
			code, err := d.host.CodeReleaseAssets()
			if err == nil {
				_, err = p.Blueprint.candidate(owner, code)
			}
			if err != nil {
				out.Problems = append(out.Problems, err.Error())
			}
		}
	}
	if p.Runtime != nil {
		if err := p.Runtime.Check(); err != nil {
			out.Problems = append(out.Problems, err.Error())
		} else {
			for _, target := range p.Runtime.Targets {
				if _, _, err := driver.Binary(index.Directory, target.Artifact); err != nil {
					out.Problems = append(out.Problems, err.Error())
				}
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
			if _, offered := index.latest(need); offered {
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
func (d *Console) contributionKeys() map[string]string {
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

// packageArtifactKey is where one package version's sealed bytes live.
func packageArtifactKey(tenant, id, version string) string {
	return "packages/" + tenant + "/" + id + "/" + version + ".json"
}

// Installation state can change; an accepted version's contents cannot.
// Check the current and retained versions alike, including packages without a runtime.
func (d *Console) verifyPackageVersion(descriptor PackageDescriptor, prior *InstalledPackage) error {
	if prior == nil {
		return nil
	}
	artifact := ""
	if prior.Version == descriptor.Version {
		artifact = prior.Artifact
	} else if i := slices.IndexFunc(prior.Retained, func(r RetainedArtifact) bool { return r.Version == descriptor.Version }); i >= 0 {
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
func (d *Console) sealPackageBytes(descriptor PackageDescriptor, directory string) (string, error) {
	if d.host == nil {
		return "", fmt.Errorf("the console is not composed with a tenant")
	}
	if descriptor.Runtime != nil {
		for _, target := range descriptor.Runtime.Targets {
			_, binary, err := driver.Binary(directory, target.Artifact)
			if err != nil {
				return "", err
			}
			if err = d.host.PutPackageArtifact(packageBinaryKey(d.tenant, descriptor.ID, descriptor.Version, target.Artifact), binary); err != nil {
				return "", err
			}
		}
	}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	key := packageArtifactKey(d.tenant, descriptor.ID, descriptor.Version)
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
func (d *Console) verifySealedPackage(held InstalledPackage) error {
	if held.Artifact == "" {
		return nil // installed before sealing existed: nothing to verify
	}
	key := packageArtifactKey(d.tenant, held.ID, held.Version)
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

// decidePackage applies the console's package decisions. It returns the closure
// shape the console's other decisions use.
func (d *Console) decidePackage(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	if s.GetSchema().GetName() == SchemaPackageCleanupConfirm {
		return d.decidePackageCleanup(s)
	}
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
	descriptor, ok := index.find(p.ID, p.Version)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if (descriptor.Runtime != nil || descriptor.Blueprint != nil) && p.Version == "" {
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
			d.packages = map[string]*InstalledPackage{}
		}
		prior := d.packages[descriptor.ID]
		installed := &InstalledPackage{ID: descriptor.ID, Version: descriptor.Version, Namespace: descriptor.Namespace,
			Runtime: descriptor.Runtime,
			Title:   descriptor.Title, State: "active", Contributions: descriptor.Contributions, Digest: descriptor.ArtifactDigest,
			Artifact: sealed, InstalledAt: now, ChangedAt: now}
		if prior != nil {
			installed.InstalledAt = prior.InstalledAt
			installed.Retained = slices.DeleteFunc(slices.Clone(prior.Retained), func(r RetainedArtifact) bool {
				return r.Version == descriptor.Version || r.Version == prior.Version
			})
			if prior.Version != descriptor.Version {
				retained := prior.Artifact
				if retained == "" {
					retained = prior.Digest
				}
				installed.Retained = append(installed.Retained, RetainedArtifact{Version: prior.Version, Digest: retained, At: now})
			}
		}
		d.packages[descriptor.ID] = installed
	}, nil
}

// PackageViews lists the index beside what the tenant holds.
func (d *Console) PackageViews() []PackageView {
	out := []PackageView{}
	index, indexErr := d.packageIndex()
	installed := d.packagesOf()
	listed := map[string]bool{}
	for _, descriptor := range index.Packages {
		pre := d.precheck(descriptor, index)
		if indexErr != nil {
			pre.OK = false
			pre.Problems = append(pre.Problems, indexErr.Error())
		}
		view := PackageView{Descriptor: descriptor, Precheck: pre, Source: "index"}
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
		descriptor := PackageDescriptor{ID: held.ID, Version: held.Version, Title: held.Title, Namespace: held.Namespace, Runtime: held.Runtime, Contributions: held.Contributions}
		pre := Precheck{ID: held.ID, Version: held.Version, Problems: []string{"This installed version is not in the deployment index and cannot be installed from this list."}}
		if held.Artifact != "" {
			if sealed, err := d.sealedPackageDescriptor(held.ID, held.Version, held.Artifact); err == nil {
				descriptor = sealed
			} else {
				pre.Problems = append(pre.Problems, err.Error())
			}
		}
		copy := *held
		out = append(out, PackageView{Descriptor: descriptor, Installed: &copy, Precheck: pre, Source: "installed", Removal: d.packageRemoval(held.ID)})
	}
	slices.SortFunc(out, func(a, b PackageView) int {
		if a.Descriptor.ID != b.Descriptor.ID {
			return strings.Compare(a.Descriptor.ID, b.Descriptor.ID)
		}
		return strings.Compare(a.Descriptor.Version, b.Descriptor.Version)
	})
	return out
}

// Contributions lists this tenant's namespaced contributions: every app's
// declared views and every active package's.
func (d *Console) Contributions() []Contribution {
	out := []Contribution{}
	if d.host != nil {
		for _, app := range d.host.Apps() {
			manifest := app.Manifest()
			for _, page := range manifest.Pages {
				out = append(out, Contribution{Key: manifest.ID + "." + page.Name, View: page.Name, Title: page.Title,
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
			out = append(out, Contribution{Key: p.Namespace + "." + c.View, View: c.View, Surface: surface, Title: c.Title,
				Kind: kind, App: c.App, Source: p.ID, State: p.State})
		}
	}
	d.mu.Unlock()
	slices.SortFunc(out, func(a, b Contribution) int { return strings.Compare(a.Key, b.Key) })
	return out
}
