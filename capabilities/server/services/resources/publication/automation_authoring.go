package publication

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type AutomationEnvironment interface {
	Application() platform.Service
	Host() protocol.BuildHost
	Ledger() platform.Authority
	AppID() string
	Version() string
	Roles() []string
	Objects(platform.Caller) ([]platform.RecordType, error)
	InstalledObject(string) (platform.RecordType, bool)
	Entity(string) (platform.RecordTypeInfo, bool)
	Query(string, int) (platform.NamedQuery, int, bool)
	ValidateQuery(platform.NamedQuery) error
	CheckIntake(platform.Caller, protocol.Process) *platform.Error
	FunctionCallAction() string
	FunctionResult(platform.Caller, string) (protocol.FunctionOutcome, bool)
	JointMode() bool
}

type AutomationAuthoring interface {
	PrepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	Inventory() ([]protocol.Process, error)
	DraftAsset(record protocol.Process, sourceVersion string) (platform.ReleaseAsset, error)
	Check(c platform.Caller, p protocol.Process) *platform.Error
	ProcessFromAsset(asset platform.ReleaseAsset) (protocol.Process, error)
	FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error)
	Run(c platform.Caller, now time.Time) *platform.Error
	HandlesPublication(schema string) bool
	ValidateImage(schema string, image []byte) error
	ApplyImage(schema string, image []byte) error
	Restore() error
	CheckDraft(c platform.Caller, record protocol.Process, source platform.RecordType, models []platform.RecordType, member platform.Member, validate bool) (protocol.Process, platform.AssetRef, platform.AssetRef, bool, error)
	CheckObjectDefinition(c platform.Caller, entity, previous platform.RecordType, changed, compatible bool) error
	ProcessAssets(processes []protocol.Process, sourceVersion string) ([]platform.ReleaseAsset, error)
	DecodeInstallation(schema string, image json.RawMessage) (protocol.AssetInstallation, error)
	ProcessActions() []platform.Action
	ProcessEntity() platform.RecordType
	Decide(authority platform.Authority, c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, bool)
}
