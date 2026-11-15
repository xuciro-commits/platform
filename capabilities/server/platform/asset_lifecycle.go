package platform

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"strings"
	"time"
)

func ResourceID(tenant string, ref AssetRef) string {
	sum := sha256.Sum256([]byte(tenant + "/" + ref.String()))
	return "ri." + ref.App + "." + tenant + "." + string(ref.Kind) + "." + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16]))
}

type AssetEnvironmentRequirement struct {
	Ref        AssetRef `json:"ref"`
	Profile    string   `json:"profile,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

type AssetReleaseContext struct {
	ManagedAssets map[AssetRef]bool
	ManagedBy     string
	DraftsOnly    bool
	DraftIDs      map[AssetRef]string
	Project       string
	Member        Member
	At            time.Time
	Upgrade       bool
}

type AssetApplyContext struct {
	Private  bool
	Validate bool
	Upgrade  bool
}

type AssetInstallation struct {
	Ref    AssetRef        `json:"ref"`
	Schema string          `json:"schema"`
	Record string          `json:"record"`
	Image  json.RawMessage `json:"image"`
}

type AssetDraft struct {
	Ref    AssetRef        `json:"ref"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type AssetLifecycleOwner interface {
	App
	AssetKinds() []AssetKind
	ReleaseAssets() ([]ReleaseAsset, error)
	ResolvePinned(ref AssetRef, sourceVersion string, contentVersion ...string) (ReleaseAsset, error)
	AssetEnvironmentRequirements(ReleaseAsset) ([]AssetEnvironmentRequirement, error)
	BeginAssetBootstrap([]ReleaseAsset) (func(), error)
	ReleaseDrafts([]ReleaseAsset, AssetReleaseContext) ([]AssetDraft, error)
	PrepareReleaseAssets([]ReleaseAsset, AssetReleaseContext) ([]AssetInstallation, error)
	DecodeAssetInstallation(schema string, image json.RawMessage) (AssetInstallation, error)
	ValidateAssetInstallation(AssetInstallation) error
	ApplyAssetInstallation(AssetInstallation, AssetApplyContext) error
}

type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]AssetRef, []ReleaseAsset, error)
}

type LinkPublicationReader interface {
	LinkPublication([]byte) (LinkType, error)
}

type FlowReleaseOwner interface {
	FlowReleaseAsset(string, int) (ReleaseAsset, error)
}

type ReleaseExecutionOwner interface {
	ForkReleaseExecution([]ReleaseAsset) (App, error)
}

type OperationDefinitions interface {
	OperationDefinition(string, int) (Operation, int, bool)
}

type FlowDefinitionPresence interface {
	HasDefinition(string, string, int) bool
}

type OperationCompletion interface {
	OperationEnded(Caller, string, time.Time) *Error
}

type NativeFlowAssets interface {
	NativeFlowAsset(Manifest, string, int, []AssetRef) (ReleaseAsset, error)
}

type AssetDefinitionReader interface {
	DefinitionMetadataKind(AssetKind) bool
	DefinitionDescriptor(ReleaseAsset) (Definition, error)
}
