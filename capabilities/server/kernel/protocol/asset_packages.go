package protocol

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/capability"
	"platformserver/platform"
	"time"
)

type InstalledPackage struct {
	ID            string                         `json:"id"`
	Version       string                         `json:"version"`
	Namespace     string                         `json:"namespace"`
	Space         string                         `json:"space,omitempty"`
	Title         string                         `json:"title,omitempty"`
	State         string                         `json:"state"`
	Contributions []platform.PackageContribution `json:"contributions,omitempty"`
	Digest        string                         `json:"digest,omitempty"`
	Artifact      string                         `json:"artifact,omitempty"`
	InstalledAt   time.Time                      `json:"installedAt,omitempty"`
	ChangedAt     time.Time                      `json:"changedAt,omitempty"`
	Retained      []RetainedArtifact             `json:"retained,omitempty"`
	DemoRecords   []string                       `json:"demoRecords,omitempty"`
	DemoCleared   bool                           `json:"demoCleared,omitempty"`
}

type RetainedArtifact struct {
	Version string    `json:"version"`
	Digest  string    `json:"digest,omitempty"`
	At      time.Time `json:"at"`
}

type Precheck struct {
	ID       string                         `json:"id"`
	Version  string                         `json:"version"`
	OK       bool                           `json:"ok"`
	Problems []string                       `json:"problems,omitempty"`
	Missing  []string                       `json:"missing,omitempty"`
	Bindings []capability.CapabilityBinding `json:"bindings,omitempty"`
}

type PackageView struct {
	Plan         PackageInstallPlan         `json:"plan"`
	Update       bool                       `json:"update,omitempty"`
	Downgrade    bool                       `json:"downgrade,omitempty"`
	Rollback     bool                       `json:"rollback,omitempty"`
	Origin       string                     `json:"origin,omitempty"`
	Product      string                     `json:"product,omitempty"`
	Candidate    string                     `json:"candidate,omitempty"`
	SourceDigest string                     `json:"sourceDigest,omitempty"`
	Removal      *PackageRemoval            `json:"removal,omitempty"`
	Descriptor   platform.PackageDescriptor `json:"descriptor"`
	Installed    *InstalledPackage          `json:"installed,omitempty"`
	Precheck     Precheck                   `json:"precheck"`
	Source       string                     `json:"source"`
	Space        string                     `json:"space,omitempty"`
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

type PackageFieldChange struct {
	Name         string `json:"name"`
	Change       string `json:"change"`
	Before       string `json:"before,omitempty"`
	After        string `json:"after,omitempty"`
	Incompatible bool   `json:"incompatible,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type PackageResourceChange struct {
	Ref    platform.AssetRef    `json:"ref"`
	Change string               `json:"change"`
	Before string               `json:"before,omitempty"`
	After  string               `json:"after,omitempty"`
	Fields []PackageFieldChange `json:"fields,omitempty"`
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
	Packages  []platform.PackageDescriptor
	Directory string
}

type PackageSource struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Origin  string `json:"origin"`
}

type PackageLibrary struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	ResolvedURL string    `json:"resolvedUrl,omitempty"`
	Title       string    `json:"title,omitempty"`
	Builtin     bool      `json:"builtin,omitempty"`
	AddedAt     time.Time `json:"addedAt,omitempty"`
}

type PackageCatalogEntry struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	URL         string          `json:"url"`
	Sha256      string          `json:"sha256"`
	Listing     *PackageListing `json:"listing,omitempty"`
}

type PackageListing struct {
	Industries   []string              `json:"industries,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty"`
	Keywords     []string              `json:"keywords,omitempty"`
	Stage        string                `json:"stage,omitempty"`
	Source       *PackageListingSource `json:"source,omitempty"`
}

type PackageListingSource struct {
	Ecosystem string `json:"ecosystem"`
	App       string `json:"app"`
	Version   string `json:"version,omitempty"`
	License   string `json:"license,omitempty"`
	Relation  string `json:"relation"`
}

type PackageLibraryView struct {
	ID          string                `json:"id"`
	URL         string                `json:"url"`
	ResolvedURL string                `json:"resolvedUrl,omitempty"`
	Title       string                `json:"title,omitempty"`
	Builtin     bool                  `json:"builtin,omitempty"`
	Status      string                `json:"status"`
	Error       string                `json:"error,omitempty"`
	FetchedAt   time.Time             `json:"fetchedAt,omitempty"`
	Entries     []PackageCatalogEntry `json:"entries"`
}

type PackageLibraryRefreshRequest struct {
	ID string `json:"id,omitempty"`
}

type PackageInspectRequest struct {
	URL            string          `json:"url,omitempty"`
	Content        json.RawMessage `json:"content,omitempty"`
	ContentBase64  []byte          `json:"contentBase64,omitempty"`
	ExpectedSha256 string          `json:"expectedSha256,omitempty"`
}

type PackagePreview struct {
	Descriptor     platform.PackageDescriptor `json:"descriptor"`
	Digest         string                     `json:"digest"`
	ArtifactSha256 string                     `json:"artifactSha256,omitempty"`
	Origin         string                     `json:"origin"`
	Precheck       Precheck                   `json:"precheck"`
}

type Offer struct {
	Descriptor platform.PackageDescriptor `json:"descriptor"`
	Origin     string                     `json:"origin"`
}

type PackageRemoval struct {
	ID       string   `json:"id"`
	OK       bool     `json:"ok"`
	Blockers []string `json:"blockers"`
}

type PackageState struct {
	Packages  map[string]*InstalledPackage
	Sources   map[string]PackageSource
	Libraries map[string]PackageLibrary
}

type PackageHost interface {
	PackageLifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
	PrepareManagedPackage(platform.Caller, platform.PackageDescriptor, []platform.ReleaseAsset, bool, bool, bool) (func(*pb.ChangeRecord), *platform.Error)
	Apps() []platform.Service
	CodeReleaseAssets() ([]platform.ReleaseAsset, error)
	PutPackageArtifact(string, []byte) error
	GetPackageArtifact(string) ([]byte, error)
	BaseModules() []capability.CapabilityModule
	ProductVersions() []platform.ProductVersionView
}

type ConsolePackages interface {
	LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
	BlueprintCatalog
	Attach(PackageHost)
	BindPackageIndex(*PackageIndex)
	BoundIndex() *PackageIndex
	SnapshotState() PackageState
	Restore(PackageState)
	Prepare(platform.Caller, *pb.Submission) (func(*pb.ChangeRecord), *platform.Error)
	BlueprintViews() []PackageView
	PackageViews() []PackageView
	PackageLibraries() []PackageLibrary
	Contributions() []Contribution
	Precheck(platform.PackageDescriptor) Precheck
	Offer(string) (Offer, error)
	PreparationBlueprint(string, string) (platform.PackageBlueprint, *InstalledPackage, error)
	PackagePrerequisites(string, string) ([]platform.ReleaseAsset, error)
	InstalledPackages() map[string]*InstalledPackage
	PackageSeed(string) ([]json.RawMessage, bool)
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
