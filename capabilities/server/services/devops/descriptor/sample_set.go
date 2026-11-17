package descriptor

import (
	"encoding/json"

	"platformserver/platform"
)

type SampleSetRecord struct {
	platform.Record
	Name              string                   `json:"name" field:"required,search"`
	Title             string                   `json:"title" field:"required,search"`
	Samples           []platform.SampleRecords `json:"samples" type:"json" field:"required"`
	State             string                   `json:"state" field:"readonly" choices:"draft,published"`
	DefinitionVersion int                      `json:"definitionVersion,omitempty" field:"readonly" title:"Definition version"`
	Published         string                   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string                 `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

func (m SampleSetRecord) Descriptor() platform.SampleSet {
	return platform.SampleSet{Name: m.Name, Title: m.Title, Samples: m.Samples}
}

type SampleSetEnvironment struct {
	App, Type, Builder, Version string
	Caller                      func() platform.Caller
	ReleaseAssets               func() ([]platform.ReleaseAsset, error)
	WithDependencies            func(platform.ReleaseAsset) (platform.ReleaseAsset, error)
	CheckDependencies           func(platform.ReleaseAsset) error
}

type SampleSetPublication struct {
	Entity               func() platform.Entity
	Asset                func(SampleSetRecord, int) (platform.ReleaseAsset, error)
	Draft                func(platform.ReleaseAsset) (SampleSetRecord, error)
	Assets               func() ([]platform.ReleaseAsset, error)
	Resolve              func(platform.AssetRef, string) (platform.ReleaseAsset, error)
	PrepareInstallations func([]platform.ReleaseAsset) ([]platform.AssetInstallation, error)
	DecodeInstallation   func(json.RawMessage) (platform.AssetInstallation, error)
	ReadFrozen           func(json.RawMessage) (json.RawMessage, error)
	VersionedDraft       func(string) ([]platform.ReleaseAsset, []platform.ReleaseAsset, platform.AssetRef, platform.AssetRef, bool, error)
}
