package publication

import (
	"encoding/json"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type ApplicationEnvironment interface {
	Host() protocol.BuildHost
	AppID() string
	Version() string
	Resolver() Resolver
	Entity(string) (platform.RecordTypeInfo, bool)
	DraftObjectLookup(platform.Caller) (func(string) (platform.RecordTypeInfo, bool), error)
	CheckName(string, string) error
}

type ApplicationProjection struct {
	Member         platform.Member
	Resolver       Resolver
	Definitions    []platform.Definition
	Entities       map[string]platform.RecordTypeInfo
	Pages          map[platform.AssetRef]bool
	Resources      map[platform.AssetRef]bool
	PublishedFlows map[platform.AssetRef]bool
}

type ApplicationAuthoring interface {
	Pages() ([]protocol.Page, error)
	Applications() ([]protocol.Application, error)
	PreparePagePublication(platform.ReleaseAsset, []platform.ReleaseAsset, protocol.Page) (protocol.Page, bool, error)
	PrepareApplicationPublication(platform.ReleaseAsset, protocol.Application) (protocol.Application, error)
	IsGeneratedReleasePage(platform.ReleaseAsset, []platform.ReleaseAsset) (bool, error)
	PageFromDescriptor(platform.Page) (protocol.Page, error)
	ApplicationFromAsset(platform.ReleaseAsset) (protocol.Application, error)
	PageContent(string, string) (platform.Page, bool)
	PageAssetFromView(platform.Page, string) (platform.ReleaseAsset, error)
	HandlesPublication(string) bool
	ValidateImage(string, []byte) error
	ApplyImage(string, []byte) error
	RestorePages() error
	RestoreApplications() error
	HasPublishedPage(func(name, object string) bool) (bool, error)
	PageAsset(protocol.Page, string) (platform.ReleaseAsset, error)
	ApplicationAsset(protocol.Application, string) (platform.ReleaseAsset, error)
	DecodeInstallation(string, json.RawMessage) (protocol.AssetInstallation, error)
	PageEntity() platform.RecordType
	ApplicationEntity() platform.RecordType
	CheckPageObject(platform.Caller, string) (bool, error)
	PageDraftEntity() platform.RecordType
	CheckPageDraft(protocol.Page, bool) (protocol.Page, platform.AssetRef, platform.AssetRef, bool, error)
	CheckApplicationDraft(protocol.Application, bool) (protocol.Application, platform.AssetRef, platform.AssetRef, bool, error)
}
