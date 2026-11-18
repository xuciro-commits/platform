package publication

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"time"
)

type FunctionEnvironment interface {
	Host() protocol.BuildHost
	Ledger() platform.Authority
	Resolver() Resolver
	App() string
	Version() string
	CallRoles() []string
	AllRoles() []string
	AddActions(...platform.Action)
	InstalledObject(string) (platform.Entity, bool)
	JointEntity(string) (platform.EntityInfo, bool)
	JointDeclaration(platform.AssetRef) (platform.ReleaseAsset, bool)
	DeclaresRole(string) bool
	CheckName(string, string) error
	ConditionLookup(string) (platform.EntityInfo, bool)
	CheckRuleCollections(platform.RuleReadSpec, string, func(string) (platform.EntityInfo, bool)) error
}

type FunctionAuthoring interface {
	Actions() []platform.Action
	AnswerCompilation(c platform.Caller, app platform.App, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error
	ApplyImage(raw []byte) error
	Assets() ([]platform.ReleaseAsset, error)
	CallEntity() platform.Entity
	CodeActions() []platform.Action
	CodeEntity() platform.Entity
	CodeReleaseAssets() ([]platform.ReleaseAsset, error)
	Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error)
	ComputePublication() Hooks
	DecideCodeBuild(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool)
	DecideCompute(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool)
	Declarations() []platform.AIFunction
	Definition(name string, version int) (platform.AIFunction, int, bool)
	Draft(asset platform.ReleaseAsset) (string, any, error)
	DraftAssets(id string, releaseAssets func() ([]platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error)
	Entity() platform.Entity
	Fork(env FunctionEnvironment) FunctionAuthoring
	InstallAsset(asset platform.ReleaseAsset) error
	InstallOperationAsset(asset platform.ReleaseAsset) error
	Inventory() ([]protocol.Function, error)
	OperationDeclarations() []platform.Operation
	OperationDefinition(name string, version int) (platform.Operation, int, bool)
	OperationDraft(asset platform.ReleaseAsset) (string, any, error)
	OperationReleaseAsset(name, version string) (platform.ReleaseAsset, error)
	Prepare(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	PrepareProposal(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, entity platform.Entity, now time.Time) (protocol.TransactionProposal, *platform.Error)
	Resolve(name, sourceVersion string) (platform.ReleaseAsset, error)
	Restore() error
	RestoreCodes() error
	RestoreTables() error
	Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error)
	TableEntity() platform.Entity
	TrainingCandidates(c platform.Caller, event platform.Event, images map[string]json.RawMessage) []protocol.AITrainingCandidate
	ValidateImage(raw []byte) error
}
