package publication

import (
	"encoding/json"
	"slices"

	"platformserver/platform"
)

type DraftRef struct {
	Kind platform.AssetKind
	ID   string
}

type DraftClosure struct {
	Before   []platform.ReleaseAsset
	After    []platform.ReleaseAsset
	Requires []DraftRef
	Prior    platform.AssetRef
	Next     platform.AssetRef
	HasPrior bool
}

type Frozen struct {
	Asset         platform.ReleaseAsset
	Skip          bool
	Publications  []platform.ReleasePublication
	Installations []platform.AssetInstallation
}

type Hooks struct {
	Kinds          []platform.AssetKind
	Schemas        []string
	FreezeOn       platform.AssetKind
	Decode         func(json.RawMessage) (platform.AssetRef, error)
	Validate       func(json.RawMessage) error
	Freeze         func(platform.Caller, string, platform.ReleaseAsset, []platform.ReleaseAsset) (Frozen, error)
	Closure        func(platform.Caller, string, platform.Member) (DraftClosure, error)
	Install        func(json.RawMessage) error
	ReadFrozen     func(json.RawMessage) (json.RawMessage, error)
	FreezeBatch    func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	VersionedDraft func(string) (DraftClosure, error)
	LoadExecution  func(platform.ReleaseAsset) error
}

func Kind(table []Hooks, kind platform.AssetKind) (Hooks, bool) {
	for _, hooks := range table {
		if slices.Contains(hooks.Kinds, kind) {
			return hooks, true
		}
	}
	return Hooks{}, false
}

func Schema(table []Hooks, schema string) (Hooks, bool) {
	for _, hooks := range table {
		if slices.Contains(hooks.Schemas, schema) {
			return hooks, true
		}
	}
	return Hooks{}, false
}
