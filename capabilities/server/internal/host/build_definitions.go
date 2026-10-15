package host

import (
	"time"

	"platformkernel/kernel"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

// ConstructionDrafts is the authoring consumer of the existing asset owner.
// Candidate bytes and lifecycle acceptance still belong to AssetRegistry.
type ConstructionDrafts interface {
	platform.App
	CompleteBlueprintAssets([]platform.ReleaseAsset) ([]platform.ReleaseAsset, error)
	RootDrafts([]platform.AssetRef) ([]build.JointDraftRef, error)
	ApplicationDrafts(string) ([]build.JointDraftRef, error)
	ReferencedDrafts(platform.AssetKind, string) ([]build.JointDraftRef, error)
	DraftReleaseAssetsMulti([]build.JointDraftRef, platform.Member) (build.JointDraftAssets, error)
	CandidateActions(platform.ReleaseAsset) ([]platform.Action, error)
	CheckProcess(build.Process) *kernel.Error
}

// ConstructionDraftReader converts a copied, already authorized object draft
// to the owner's current expression without changing retained publications.
type ConstructionDraftReader interface {
	CanonicalObject(build.Object) (build.Object, error)
}

// IntegrationDefinitions resolves accepted typed configuration. It owns no
// transport or host lock; consumers capture values before outside I/O.
type IntegrationDefinitions interface {
	ExecutionSource(build.Source) (build.Source, error)
	ExecutionConnection(build.Connection) (build.Connection, error)
	ExecutionDataset(build.Dataset) (build.Dataset, error)
	ExecutionPipeline(build.Pipeline) (build.Pipeline, error)
	ExecutionWriteback(build.Writeback) (build.Writeback, error)
	ExecutionMatching(build.Match) (build.Match, error)
}

type ConstructionSandbox interface {
	platform.App
	Reinstall() error
	ProcessFromAsset(platform.ReleaseAsset) (build.Process, error)
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
	ExecutionObservation(build.Observation) (platform.ObservationDefinition, error)
}
