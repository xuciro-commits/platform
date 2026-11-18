package publication

import (
	"encoding/json"
	"platformserver/kernel/protocol"

	"platformserver/platform"
)

type QueryAuthoringEnvironment struct {
	App, Type, Builder, Version string
	Host                        func() protocol.BuildHost
	InstalledObject             func(string) (platform.RecordType, bool)
	LookupEntity                func(string) (platform.RecordTypeInfo, bool)
	JointEntities               func() map[string]platform.RecordTypeInfo
	JointDeclarations           func() map[platform.AssetRef]platform.ReleaseAsset
	JointMode                   func() bool
	AssetVersion                func(platform.ReleaseAsset) int
	WithDependencies            func(platform.ReleaseAsset) (platform.ReleaseAsset, error)
	CheckDependencies           func(platform.ReleaseAsset) error
}

type QueryAuthoringBinding struct {
	Fork            func(QueryAuthoringEnvironment) QueryAuthoringBinding
	Entity          func() platform.RecordType
	Declarations    func() []platform.NamedQuery
	Definition      func(string, int) (platform.NamedQuery, int, bool)
	Exists          func(string) bool
	ToolNames       func(platform.Caller) ([]string, error)
	Stamps          func() []string
	Published       func(platform.Caller, string) bool
	CheckRecordSets func(platform.RecordTypeInfo, func(string) (platform.RecordTypeInfo, bool), ...platform.RuleEngine) error
	Assets          func() ([]platform.ReleaseAsset, error)
	Resolve         func(string, string) (platform.ReleaseAsset, error)
	InstallAsset    func(platform.ReleaseAsset) error
	Restore         func() error
	ValidateImage   func(json.RawMessage) error
	InstallImage    func(json.RawMessage) error
	FreezeBatch     func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	VersionedDraft  func(string, func() ([]platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, []platform.ReleaseAsset, platform.AssetRef, platform.AssetRef, bool, error)
	LoadExecution   func(platform.ReleaseAsset) error
}
