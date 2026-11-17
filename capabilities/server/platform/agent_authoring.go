package platform

import (
	"encoding/json"
	"reflect"
)

type AgentAuthoringEnvironment struct {
	App, Type, Builder string
	Authority          func() Authority
	JointEntities      func() map[string]EntityInfo
	JointAssets        func() map[AssetRef]bool
	JointDeclarations  func() map[AssetRef]ReleaseAsset
	QueryExists        func(string) bool
	InstalledAction    func(string) string
	DeclaresRole       func(string) bool
	DraftTools         func(Caller) (map[string]bool, []string, error)
	ReleaseAssets      func() ([]ReleaseAsset, error)
	SourceVersion      func() string
	WithDependencies   func(ReleaseAsset) (ReleaseAsset, error)
	CheckDependencies  func(ReleaseAsset) error
	Inventory          func(reflect.Type) ([]any, error)
	Read               func(string, any) bool
	ValidName          func(string) bool
	EqualAsset         func(ReleaseAsset, ReleaseAsset) bool
}

type AgentAuthoringBinding struct {
	Fork           func(AgentAuthoringEnvironment) AgentAuthoringBinding
	Entity         func() Entity
	DraftEntity    func() Entity
	Stamps         func() []string
	Declarations   func() []Agent
	Version        func(string) int
	Published      func(Caller, string) bool
	DraftAsset     func(Caller, string) (ReleaseAsset, error)
	DraftFromAsset func(ReleaseAsset) (any, error)
	Assets         func() ([]ReleaseAsset, error)
	Resolve        func(AssetRef, string) (ReleaseAsset, error)
	InstallAsset   func(ReleaseAsset) error
	Restore        func() error
	ValidateImage  func(json.RawMessage) error
	InstallImage   func(json.RawMessage) error
	ReadFrozen     func(json.RawMessage) (json.RawMessage, error)
	FreezeBatch    func([]ReleaseAsset) ([]ReleasePublication, error)
	VersionedDraft func(string) ([]ReleaseAsset, []ReleaseAsset, AssetRef, AssetRef, bool, error)
	LoadExecution  func(ReleaseAsset) error
}
