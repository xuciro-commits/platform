package publication

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"

	"platformserver/platform"
)

type DraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
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
	Installations []protocol.AssetInstallation
}

type SchemaHooks struct {
	Name     string
	Decode   func(json.RawMessage) (platform.AssetRef, error)
	Validate func(json.RawMessage) error
	Install  func(json.RawMessage) error
}

type Hooks struct {
	Kinds               []platform.AssetKind
	Schemas             []SchemaHooks
	FreezeOn            platform.AssetKind
	Freeze              func(platform.Caller, string, platform.ReleaseAsset, []platform.ReleaseAsset) (Frozen, error)
	Closure             func(platform.Caller, string, platform.Member) (DraftClosure, error)
	ReadFrozen          func(json.RawMessage) (json.RawMessage, error)
	FreezeBatch         func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	FreezeInstallations func([]platform.ReleaseAsset) ([]protocol.AssetInstallation, error)
	DirectInstallation  func(json.RawMessage) (protocol.AssetInstallation, error)
	VersionedDraft      func(string, func() ([]platform.ReleaseAsset, error)) (DraftClosure, error)
	LoadExecution       func(platform.ReleaseAsset) error
}

func Kind(table []Hooks, kind platform.AssetKind) (Hooks, bool) {
	for _, hooks := range table {
		if slices.Contains(hooks.Kinds, kind) {
			return hooks, true
		}
	}
	return Hooks{}, false
}

func Schema(table []Hooks, schema string) (SchemaHooks, bool) {
	for _, hooks := range table {
		for _, candidate := range hooks.Schemas {
			if candidate.Name == schema {
				return candidate, true
			}
		}
	}
	return SchemaHooks{}, false
}
