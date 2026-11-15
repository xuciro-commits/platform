package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
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
	Downgrade    bool               `json:"downgrade,omitempty"`
	Rollback     bool               `json:"rollback,omitempty"`
	Origin       string             `json:"origin,omitempty"`
	Product      string             `json:"product,omitempty"`
	Candidate    string             `json:"candidate,omitempty"`
	SourceDigest string             `json:"sourceDigest,omitempty"`
	Removal      *PackageRemoval    `json:"removal,omitempty"`
	Descriptor   PackageDescriptor  `json:"descriptor"`
	Installed    *InstalledPackage  `json:"installed,omitempty"`
	Precheck     Precheck           `json:"precheck"`
	Source       string             `json:"source"`
}

type PackageInstallStep struct {
	ID               string `json:"id"`
	Version          string `json:"version"`
	Title            string `json:"title"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	Problem          string `json:"problem,omitempty"`
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

type PackageImpactReference struct {
	Resource AssetRef `json:"resource"`
	Consumer AssetRef `json:"consumer"`
}

type PackageImpactExtension struct {
	Type          string       `json:"type"`
	Base          AssetBinding `json:"base"`
	TargetVersion string       `json:"targetVersion"`
	Fields        []string     `json:"fields"`
	Issue         string       `json:"issue,omitempty"`
}

type PackageImpactOutput struct {
	Resource AssetRef `json:"resource"`
	Records  int      `json:"records"`
	Retained bool     `json:"retained"`
}

type PackageImpactConnection struct {
	Resource   AssetRef `json:"resource"`
	Configured bool     `json:"configured"`
	Credential bool     `json:"credential"`
}

type PackageLifecycleImpact struct {
	Dependencies []string                  `json:"dependencies"`
	References   []PackageImpactReference  `json:"references"`
	Extensions   []PackageImpactExtension  `json:"extensions"`
	Runs         []ApplicationRun          `json:"runs"`
	Connections  []PackageImpactConnection `json:"connections"`
	Outputs      []PackageImpactOutput     `json:"outputs"`
}

type PackageState struct {
	Packages map[string]*InstalledPackage
	Sources  map[string]PackageSource
}

type PackageHost interface {
	PackageLifecycleImpact(Caller, string, []ReleaseAsset) (PackageLifecycleImpact, error)
	PrepareManagedPackage(Caller, PackageDescriptor, []ReleaseAsset, bool, bool, bool) (func(*pb.ChangeRecord), *Error)
	Apps() []App
	CodeReleaseAssets() ([]ReleaseAsset, error)
	PutPackageArtifact(string, []byte) error
	GetPackageArtifact(string) ([]byte, error)
	BaseModules() []CapabilityModule
	ProductVersions() []ProductVersionView
}

type ConsolePackages interface {
	LifecycleImpact(Caller, string, []ReleaseAsset) (PackageLifecycleImpact, error)
	BlueprintCatalog
	Attach(PackageHost)
	BindPackageIndex(*PackageIndex)
	BoundIndex() *PackageIndex
	SnapshotState() PackageState
	Restore(PackageState)
	Prepare(Caller, *pb.Submission) (func(*pb.ChangeRecord), *Error)
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
