package publication

import (
	"encoding/json"

	"platformserver/platform"
)

type Resolver struct {
	CheckPage                 func(platform.Page) error
	CheckFrozenQueries        func(platform.Page, map[platform.AssetRef]platform.ReleaseAsset) error
	Safety                    func(platform.Page) (platform.Page, error)
	PageDependencies          func(platform.Page) []platform.AssetRef
	PageNavigationEdge        func(platform.ReleaseAsset, platform.AssetRef) bool
	PageQueryReferences       func(platform.Page) []platform.AssetRef
	PageComputeResources      func(platform.Page) map[string]platform.PageComputeResource
	PageEventNavigation       func(platform.PageEventBinding) *platform.PageNavigation
	CheckPageEmbeddingGraph   func(platform.AssetRef, map[platform.AssetRef]platform.ReleaseAsset) error
	CheckPageVariables        func(platform.Application, platform.Page) error
	PageContentVersion        func(platform.Page) (string, error)
	CheckPageContentVersion   func(string) error
	CheckPageNavigation       func(platform.Page, platform.Page, platform.PageNavigation) error
	ApplicationPage           func(platform.Application) platform.Page
	CheckApplicationProfile   func(*platform.ApplicationProfile) error
	CheckApplicationVariables func(platform.Application) error
	CheckApplicationResources func(platform.Application) error
	CheckApplicationExtension func(platform.Application) error
	ApplicationDependencies   func(platform.Application, string) []platform.AssetRef
	ObjectDescriptor          func([]byte) (platform.RecordTypeInfo, error)
	ActionDescriptor          func(json.RawMessage) (platform.Action, error)
	ObjectDependencies        func(platform.ReleaseAsset, bool) ([]platform.AssetRef, []platform.Capability, error)
	ActionDependencies        func(platform.ReleaseAsset, bool) ([]platform.AssetRef, []platform.Capability, error)
	CheckAgent                func(platform.AgentReleaseDescriptor) error
	AgentDependencies         func(platform.AgentReleaseDescriptor, string) []platform.AssetRef
	AgentDescriptor           func(platform.Agent) (platform.AgentReleaseDescriptor, error)
}
