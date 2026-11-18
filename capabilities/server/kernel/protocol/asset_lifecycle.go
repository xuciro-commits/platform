package protocol

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"platformserver/platform"
	"strings"
	"time"
)

func ResourceID(tenant string, ref platform.AssetRef) string {
	sum := sha256.Sum256([]byte(tenant + "/" + ref.String()))
	return "ri." + ref.App + "." + tenant + "." + string(ref.Kind) + "." + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))
}

type AssetEnvironmentRequirement struct {
	Ref        platform.AssetRef `json:"ref"`
	Profile    string            `json:"profile,omitempty"`
	Credential bool              `json:"credential,omitempty"`
}

type AssetReleaseContext struct {
	ManagedAssets map[platform.AssetRef]bool
	ManagedBy     string
	DraftsOnly    bool
	DraftIDs      map[platform.AssetRef]string
	Project       string
	Member        platform.Member
	At            time.Time
	Upgrade       bool
}

type AssetApplyContext struct {
	Private  bool
	Validate bool
	Upgrade  bool
}

type AssetInstallation struct {
	Ref    platform.AssetRef `json:"ref"`
	Schema string            `json:"schema"`
	Record string            `json:"record"`
	Image  json.RawMessage   `json:"image"`
}

type AssetDraft struct {
	Ref    platform.AssetRef `json:"ref"`
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Schema string            `json:"schema"`
	Image  json.RawMessage   `json:"image"`
}

type AssetLifecycleOwner interface {
	platform.Service
	AssetKinds() []platform.AssetKind
	ReleaseAssets() ([]platform.ReleaseAsset, error)
	ResolvePinned(ref platform.AssetRef, sourceVersion string, contentVersion ...string) (platform.ReleaseAsset, error)
	AssetEnvironmentRequirements(platform.ReleaseAsset) ([]AssetEnvironmentRequirement, error)
	BeginAssetBootstrap([]platform.ReleaseAsset) (func(), error)
	ReleaseDrafts([]platform.ReleaseAsset, AssetReleaseContext) ([]AssetDraft, error)
	PrepareReleaseAssets([]platform.ReleaseAsset, AssetReleaseContext) ([]AssetInstallation, error)
	DecodeAssetInstallation(schema string, image json.RawMessage) (AssetInstallation, error)
	ValidateAssetInstallation(AssetInstallation) error
	ApplyAssetInstallation(AssetInstallation, AssetApplyContext) error
}

type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error)
}

type LinkPublicationReader interface {
	LinkPublication([]byte) (platform.LinkType, error)
}

type FlowReleaseOwner interface {
	FlowReleaseAsset(string, int) (platform.ReleaseAsset, error)
}

type ReleaseExecutionOwner interface {
	ForkReleaseExecution([]platform.ReleaseAsset) (platform.Service, error)
}

type OperationDefinitions interface {
	OperationDefinition(string, int) (platform.Operation, int, bool)
}

type OperationCompletion interface {
	OperationEnded(platform.Caller, string, time.Time) *platform.Error
}

type AssetDefinitionReader interface {
	DefinitionMetadataKind(platform.AssetKind) bool
	DefinitionDescriptor(platform.ReleaseAsset) (platform.Definition, error)
}
