package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"strings"
	"time"
)

type PackageContribution struct {
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
}

type PackageDescriptor struct {
	Blueprint      *PackageBlueprint     `json:"blueprint,omitempty"`
	ID             string                `json:"id"`
	Version        string                `json:"version"`
	Title          string                `json:"title,omitempty"`
	Namespace      string                `json:"namespace"`
	Requires       []string              `json:"requires,omitempty"`
	Capabilities   []CapabilityContract  `json:"capabilities,omitempty"`
	Compatibility  string                `json:"compatibility,omitempty"`
	Contributions  []PackageContribution `json:"contributions,omitempty"`
	ArtifactDigest string                `json:"artifactDigest,omitempty"`
	Description    string                `json:"description,omitempty"`
}

type PackageBlueprint struct {
	Roots  []AssetRef     `json:"roots"`
	Assets []ReleaseAsset `json:"assets"`
	Guide  string         `json:"guide,omitempty"`
	Setup  *PackageSetup  `json:"setup,omitempty"`
}

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

func (p PackageSetup) Check() error {
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

type InstalledPackage struct {
	ID            string                `json:"id"`
	Version       string                `json:"version"`
	Namespace     string                `json:"namespace"`
	Title         string                `json:"title,omitempty"`
	State         string                `json:"state"`
	Contributions []PackageContribution `json:"contributions,omitempty"`
	Digest        string                `json:"digest,omitempty"`
	Artifact      string                `json:"artifact,omitempty"`
	InstalledAt   time.Time             `json:"installedAt,omitempty"`
	ChangedAt     time.Time             `json:"changedAt,omitempty"`
	Retained      []RetainedArtifact    `json:"retained,omitempty"`
}

type RetainedArtifact struct {
	Version string    `json:"version"`
	Digest  string    `json:"digest,omitempty"`
	At      time.Time `json:"at"`
}

type Precheck struct {
	ID       string              `json:"id"`
	Version  string              `json:"version"`
	OK       bool                `json:"ok"`
	Problems []string            `json:"problems,omitempty"`
	Missing  []string            `json:"missing,omitempty"`
	Bindings []CapabilityBinding `json:"bindings,omitempty"`
}

type PackageView struct {
	Plan         PackageInstallPlan `json:"plan"`
	Update       bool               `json:"update,omitempty"`
	Origin       string             `json:"origin,omitempty"`
	SourceDigest string             `json:"sourceDigest,omitempty"`
	Removal      *PackageRemoval    `json:"removal,omitempty"`
	Descriptor   PackageDescriptor  `json:"descriptor"`
	Installed    *InstalledPackage  `json:"installed,omitempty"`
	Precheck     Precheck           `json:"precheck"`
	Source       string             `json:"source"`
}

type PackageInstallStep struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Title   string `json:"title"`
	Action  string `json:"action"`
}

type PackageResourceChange struct {
	Ref    AssetRef `json:"ref"`
	Change string   `json:"change"`
	Before string   `json:"before,omitempty"`
	After  string   `json:"after,omitempty"`
}

type PackageInstallPlan struct {
	Steps    []PackageInstallStep `json:"steps"`
	Problems []string             `json:"problems,omitempty"`
}

type Contribution struct {
	Key     string `json:"key"`
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
	Source  string `json:"source"`
	State   string `json:"state"`
}

type PackageIndex struct {
	Packages  []PackageDescriptor
	Directory string
}

func (p PackageDescriptor) Validate() error {
	if p.ID == "" || p.Version == "" || p.Namespace == "" {
		return fmt.Errorf("a package needs an ID, a version and a namespace")
	}
	for _, value := range []string{p.ID, p.Namespace, p.Version} {
		if len(value) > 200 || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
			return fmt.Errorf("package ID, namespace and version must be safe path segments")
		}
	}
	if _, err := ParsePackageVersion(p.Version); err != nil {
		return err
	}
	for _, need := range p.Requires {
		id, constraint := PackageDependency(need)
		if id == "" || id == p.ID {
			return fmt.Errorf("a product dependency names another product")
		}
		if constraint != "" && constraint != "*" {
			for _, alternative := range strings.Split(constraint, "||") {
				for _, term := range strings.Fields(alternative) {
					if _, err := ParsePackageVersion(strings.TrimLeft(term, "<>=^~")); err != nil {
						return err
					}
				}
			}
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
	if p.Blueprint != nil {
		return p.Blueprint.Check()
	}
	return nil
}

func (b PackageBlueprint) Check() error {
	if len(b.Roots) == 0 || len(b.Assets) == 0 || len(b.Assets) > 1000 {
		return fmt.Errorf("a blueprint needs roots and at most 1000 owner definitions")
	}
	if b.Setup != nil {
		if err := b.Setup.Check(); err != nil {
			return err
		}
	}
	for _, root := range b.Roots {
		if root.Check() != nil || root.App != "build" {
			return fmt.Errorf("a package blueprint has only Build-owned roots")
		}
	}
	seen := map[AssetRef]bool{}
	for _, asset := range b.Assets {
		if seen[asset.Ref] || asset.Ref.App != "build" || asset.ContractVersion < 1 || asset.SourceVersion == "" {
			return fmt.Errorf("blueprint owner definitions need unique pinned identities")
		}
		seen[asset.Ref] = true
		if _, err := WithDependencies(asset); err != nil {
			return err
		}
	}
	return nil
}

func (i *PackageIndex) Latest(id string) (PackageDescriptor, bool) {
	var out PackageDescriptor
	found := false
	for _, p := range i.Packages {
		version, _ := ParsePackageVersion(p.Version)
		if p.ID == id && version.Prerelease == "" && (!found || ComparePackageVersions(p.Version, out.Version) > 0) {
			out, found = p, true
		}
	}
	return out, found
}

func (i *PackageIndex) Find(id, version string) (PackageDescriptor, bool) {
	if version == "" {
		return i.Latest(id)
	}
	for _, p := range i.Packages {
		if p.ID == id && (version == "" || p.Version == version) {
			return p, true
		}
	}
	return PackageDescriptor{}, false
}

type PackageSource struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Origin  string `json:"origin"`
}

type PackageInspectRequest struct {
	URL     string          `json:"url,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type PackagePreview struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Digest     string            `json:"digest"`
	Origin     string            `json:"origin"`
	Precheck   Precheck          `json:"precheck"`
}

type Offer struct {
	Descriptor PackageDescriptor `json:"descriptor"`
	Origin     string            `json:"origin"`
}

type PackageRemoval struct {
	ID       string   `json:"id"`
	OK       bool     `json:"ok"`
	Blockers []string `json:"blockers"`
}

type PackageState struct {
	Packages map[string]*InstalledPackage
	Sources  map[string]PackageSource
}

type PackageHost interface {
	PrepareManagedPackage(Caller, PackageDescriptor, []ReleaseAsset, bool) (func(*pb.ChangeRecord), *kernel.Error)
	Apps() []App
	CodeReleaseAssets() ([]ReleaseAsset, error)
	PutPackageArtifact(string, []byte) error
	GetPackageArtifact(string) ([]byte, error)
	BaseModules() []CapabilityModule
}

type ConsolePackages interface {
	BlueprintCatalog
	Attach(PackageHost)
	BindPackageIndex(*PackageIndex)
	BoundIndex() *PackageIndex
	SnapshotState() PackageState
	Restore(PackageState)
	Prepare(Caller, *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error)
	BlueprintViews() []PackageView
	PackageViews() []PackageView
	Contributions() []Contribution
	Precheck(PackageDescriptor) Precheck
	Offer(string) (Offer, error)
	PreparationBlueprint(string, string) (PackageBlueprint, *InstalledPackage, error)
	PackagePrerequisites(string, string) ([]ReleaseAsset, error)
}

const PackageType = "platform.package"

type CompositeEdit struct {
	ExpectedRevision *uint32         `json:"expectedRevision,omitempty"`
	Schema           string          `json:"schema"`
	Target           *pb.EntityRef   `json:"target"`
	Payload          json.RawMessage `json:"payload,omitempty"`
}

type CompositeAnswer struct {
	Key     string `json:"key"`
	Applied int    `json:"applied"`
	Digest  string `json:"digest,omitempty"`
}
