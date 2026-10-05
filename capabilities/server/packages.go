package platformserver

import (
	"bytes"
	"context"
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
	"platformserver/platform"
)

// Packages and contributions (ADR-0047 §10.3, §11; ordered 2026-10-05).
//
// A package declares its identity, immutable version, the capabilities and
// contributions it brings, its required dependencies, its namespace and its
// compatibility. Installing is a controlled decision of the platform console:
// precheck first (required dependencies, compatibility, namespace conflicts),
// then register. An upgrade replaces the version and keeps the retired
// artifact; draining stops new use of a contribution while work in flight
// finishes; retiring keeps history and artifacts and registers nothing.
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
	ID             string                `json:"id"`
	Version        string                `json:"version"`
	Title          string                `json:"title,omitempty"`
	Namespace      string                `json:"namespace"`
	Requires       []string              `json:"requires,omitempty"` // package IDs
	Compatibility  string                `json:"compatibility,omitempty"`
	Contributions  []PackageContribution `json:"contributions,omitempty"`
	ArtifactDigest string                `json:"artifactDigest,omitempty"` // sha256 over the descriptor
	Description    string                `json:"description,omitempty"`
}

// InstalledPackage is the tenant's state for one installed package.
type InstalledPackage struct {
	ID            string                `json:"id"`
	Version       string                `json:"version"`
	Namespace     string                `json:"namespace"`
	Title         string                `json:"title,omitempty"`
	State         string                `json:"state"` // active, draining, retired
	Contributions []PackageContribution `json:"contributions,omitempty"`
	Digest        string                `json:"digest,omitempty"`   // the index's artifact digest, when it declares one
	Artifact      string                `json:"artifact,omitempty"` // sha256 over the bytes sealed in this tenant's store
	InstalledAt   time.Time             `json:"installedAt,omitempty"`
	ChangedAt     time.Time             `json:"changedAt,omitempty"`
	// Retained are versions this tenant replaced: their artifacts stay.
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
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	OK       bool     `json:"ok"`
	Problems []string `json:"problems,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

// PackageView is one package as the console lists it: what the index offers and
// what the tenant has.
type PackageView struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Installed  *InstalledPackage `json:"installed,omitempty"`
	Precheck   Precheck          `json:"precheck"`
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

// PackageIndex is the deployment's index: descriptors from a directory (local
// files) or an HTTP document (an online index), read at start and cached.
type PackageIndex struct {
	Packages []PackageDescriptor
}

// LoadPackageIndex reads every *.json descriptor of a directory, or one JSON
// document with an array of descriptors when the path is a URL's file name.
// The online case uses the same reader: a mirror is a directory of JSON files.
func LoadPackageIndex(dir string) (*PackageIndex, error) {
	index := &PackageIndex{}
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
	if strings.ContainsAny(p.ID+p.Namespace+p.Version, " \t\n") {
		return fmt.Errorf("package %s: ID, namespace and version have no spaces", p.ID)
	}
	for _, c := range p.Contributions {
		if c.View == "" {
			return fmt.Errorf("package %s: a contribution needs a view", p.ID)
		}
	}
	return nil
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
	out := Precheck{ID: p.ID, Version: p.Version, OK: true}
	if p.Compatibility != "" && !strings.HasPrefix(platformCompatibility, p.Compatibility) {
		out.Problems = append(out.Problems, fmt.Sprintf("needs platform %s, this host is %s", p.Compatibility, platformCompatibility))
	}
	installed := d.packagesOf()
	for _, need := range p.Requires {
		held, ok := installed[need]
		if !ok || held.State == "retired" {
			if _, offered := d.index.latest(need); offered {
				out.Missing = append(out.Missing, need)
			} else {
				out.Problems = append(out.Problems, fmt.Sprintf("requires %s, which is not installed and not in the index", need))
			}
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
	out.OK = len(out.Problems) == 0
	return out
}

// contributionKeys maps every live namespaced contribution key to its owner.
func (d *Console) contributionKeys() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]string{}
	for id, p := range d.packages {
		if p.State != "active" {
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

// sealPackageBytes writes a descriptor into the tenant's artifact store and
// returns the digest of the sealed bytes. Sealing is idempotent: the same
// descriptor seals to the same key and digest, so a retried install writes the
// same bytes. The sealed artifact is what an upgrade verifies before it
// replaces the version, and what a retained version keeps.
func (d *Console) sealPackageBytes(descriptor PackageDescriptor) (string, error) {
	if d.t == nil {
		return "", fmt.Errorf("the console is not composed with a tenant")
	}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	key := packageArtifactKey(d.t.ID, descriptor.ID, descriptor.Version)
	if err := d.t.files().Put(context.Background(), key, raw, "application/json"); err != nil {
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
	key := packageArtifactKey(d.t.ID, held.ID, held.Version)
	reader, _, err := d.t.files().Get(context.Background(), key)
	if err != nil {
		return fmt.Errorf("sealed artifact of %s@%s is missing: %w", held.ID, held.Version, err)
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return fmt.Errorf("sealed artifact of %s@%s: %w", held.ID, held.Version, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != held.Artifact {
		return fmt.Errorf("sealed artifact of %s@%s changed", held.ID, held.Version)
	}
	return nil
}

// decidePackage applies the console's package decisions. It returns the closure
// shape the console's other decisions use.
func (d *Console) decidePackage(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		ID, Version string
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil || p.ID == "" {
		return nil, invalid
	}
	schema := s.GetSchema().GetName()
	if schema == SchemaPackageRetire || schema == SchemaPackageDrain {
		held := d.packagesOf()[p.ID]
		if held == nil || held.State == "retired" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		state := "draining"
		if schema == SchemaPackageRetire {
			state = "retired"
		}
		return func(*pb.ChangeRecord) {
			held.State = state
			held.ChangedAt = time.Now().UTC()
		}, nil
	}
	descriptor, ok := d.index.find(p.ID, p.Version)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	pre := d.Precheck(descriptor)
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
		if err := d.verifySealedPackage(*prior); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The installed package {package} cannot be upgraded: {reason}", p.ID, err.Error())
		}
	} else if prior != nil && prior.State == "active" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package {package} is already installed; upgrade it instead", p.ID)
	}
	sealed, err := d.sealPackageBytes(descriptor)
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
			Title: descriptor.Title, State: "active", Contributions: descriptor.Contributions, Digest: descriptor.ArtifactDigest,
			Artifact: sealed, InstalledAt: now, ChangedAt: now}
		if upgrade && prior != nil {
			installed.InstalledAt = prior.InstalledAt
			retained := prior.Artifact
			if retained == "" {
				retained = prior.Digest
			}
			installed.Retained = append(slices.Clone(prior.Retained),
				RetainedArtifact{Version: prior.Version, Digest: retained, At: now})
		}
		d.packages[descriptor.ID] = installed
	}, nil
}

// PackageViews lists the index beside what the tenant holds.
func (d *Console) PackageViews() []PackageView {
	out := []PackageView{}
	for _, descriptor := range d.index.Packages {
		view := PackageView{Descriptor: descriptor, Precheck: d.Precheck(descriptor)}
		if held := d.packagesOf()[descriptor.ID]; held != nil {
			copy := *held
			view.Installed = &copy
		}
		out = append(out, view)
	}
	return out
}

// Contributions lists this tenant's namespaced contributions: every app's
// declared views and every active package's.
func (d *Console) Contributions() []Contribution {
	out := []Contribution{}
	if d.t != nil {
		for _, app := range d.t.apps {
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
