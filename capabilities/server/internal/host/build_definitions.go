package host

import (
	"time"

	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// ConstructionDrafts is the authoring consumer of the existing asset owner.
// Candidate bytes and lifecycle acceptance still belong to AssetRegistry.
type ConstructionDrafts interface {
	platform.App
	CompleteBlueprintAssets([]platform.ReleaseAsset) ([]platform.ReleaseAsset, error)
	RootDrafts([]platform.AssetRef) ([]construction.JointDraftRef, error)
	ApplicationDrafts(string) ([]construction.JointDraftRef, error)
	ReferencedDrafts(platform.AssetKind, string) ([]construction.JointDraftRef, error)
	SavedDraftChanges(platform.Caller, string, string, string) ([]construction.DraftChange, error)
	SavedObjectActions(platform.Caller) ([]platform.Action, error)
	DraftReleaseAssetsMulti([]construction.JointDraftRef, platform.Member) (construction.JointDraftAssets, error)
	CandidateActions(platform.ReleaseAsset) ([]platform.Action, error)
	CheckProcess(platform.Caller, construction.Process) *kernel.Error
	CheckPipeline(platform.Caller, construction.Pipeline) *kernel.Error
}

// ConstructionDraftReader converts a copied, already authorized object draft
// to the owner's current expression without changing retained publications.
type ConstructionDraftReader interface {
	CanonicalObject(construction.Object) (construction.Object, error)
}

// IntegrationDefinitions resolves accepted typed configuration. It owns no
// transport or host lock; consumers capture values before outside I/O.
type IntegrationDefinitions interface {
	ExecutionSource(construction.Source) (construction.Source, error)
	ExecutionConnection(construction.Connection) (construction.Connection, error)
	ExecutionDataset(construction.Dataset) (construction.Dataset, error)
	ExecutionPipeline(construction.Pipeline) (construction.Pipeline, error)
	ExecutionWriteback(construction.Writeback) (construction.Writeback, error)
	ExecutionMatching(construction.Match) (construction.Match, error)
}

type ConstructionSandbox interface {
	platform.App
	AssetDefinitionReader
	Reinstall() error
	ProcessFromAsset(platform.ReleaseAsset) (construction.Process, error)
	InstallPropertyTypeAsset(platform.ReleaseAsset) error
	InstallFunctionAsset(platform.ReleaseAsset) error
	InstallAgentAsset(platform.ReleaseAsset) error
	InstallOperationAsset(platform.ReleaseAsset) error
	InstallLinkTypeAsset(platform.ReleaseAsset) error
	InstallQueryAsset(platform.ReleaseAsset) error
}

type LinkPublicationReader interface {
	LinkPublication([]byte) (platform.LinkType, error)
}

type FlowReleaseOwner interface {
	FlowReleaseAsset(string, int) (platform.ReleaseAsset, error)
}

// ReleaseExecutionOwner compiles retained bytes with the original owner and
// ledger. It neither installs them live nor creates a second decision engine.
type ReleaseExecutionOwner interface {
	ForkReleaseExecution([]platform.ReleaseAsset) (platform.App, error)
}

type OperationDefinitions interface {
	OperationDefinition(string, int) (platform.Operation, int, bool)
}

type AlertDefinitions interface {
	AlertRules(string) ([]construction.Alert, error)
}
type FlowDefinitionPresence interface {
	HasDefinition(string, string, int) bool
}
type OperationCompletion interface {
	OperationEnded(platform.Caller, string, time.Time) *kernel.Error
}

// NativeFlowAssets projects the original compiled Flow owner; code callbacks
// remain in their immutable composition and never become tenant scripts.
type NativeFlowAssets interface {
	NativeFlowAsset(platform.Manifest, string, int, []platform.AssetRef) (platform.ReleaseAsset, error)
}

// ObservationDefinitions retains only the source declaration. External points
// and summaries are read transiently by the host transport.
type ObservationDefinitions interface {
	ExecutionObservation(construction.Observation) (platform.ObservationDefinition, error)
}

// AssetDefinitionReader projects the original controlled owner's accepted
// metadata. The host retains member filtering and the publication identity.
type AssetDefinitionReader interface {
	DefinitionMetadataKind(platform.AssetKind) bool
	DefinitionDescriptor(platform.ReleaseAsset) (platform.Definition, error)
}
