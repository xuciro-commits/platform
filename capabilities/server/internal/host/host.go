// Package host is what the host offers its own apps beyond the app API.
// The platform's apps import platformserver/platform and this
// package, never the host runtime. Business apps outside this module cannot
// import it. The host detects an app's roles by the interfaces it implements;
// it keeps no field for any particular app.
package host

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
	"time"
)

// Host is the composition aggregate used only by Attach. Owners receive their
// named consumer roles and cannot retain or request this aggregate.
type Host interface {
	platform.SystemDefinitionHost
	BuildHost
	FileHost
	KnowledgeHost
	RelationsHost
	FlowHost
	WorkHost
	AgentHost
	ConsoleHost
}

// FlowBinding exposes hashes and asset paths, never private definition bytes.
// Release is empty for development without a matching activated flow asset.
type FlowBinding struct {
	Existing     bool
	Applications []platform.AssetBinding
	Dependencies string
	Release      string
	Assets       []platform.AssetRef
}

// Listener is a platform app delivered other apps' events as owned work, with
// every name an event goes by (its action and the protocol events it is).
type Listener interface {
	Interested(names []string, e platform.Event) bool
	Listen(c platform.Caller, e platform.Event, names []string, now time.Time) *kernel.Error
}

// Processes is the flow app: it takes the flows every app
// declares, pins the versions instances took for replay, and hears an agent
// step's run end.
type Processes interface {
	Declare(a platform.App) error
	// Install registers a flow an app composed at runtime as its next version;
	// Validate checks one without registering it (#132).
	Install(a platform.App, fl platform.Flow) error
	// HasPublishedFlow checks published membership against the original flow owner.
	HasPublishedFlow(id string) bool
	// InstallationDraft isolates declarations for a release trial, without running instances.
	InstallationDraft() Processes
	Validate(a platform.App, fl platform.Flow) error
	// HasRunningDependency protects a composed flow's source and transitive
	// object dependencies while its instances are running, waiting, compensating or stuck.
	HasRunningDependency(typ string) (bool, error)
	HasUnboundRunningDependency(typ string) (bool, error)
	// Check refuses to start when a running instance needs a version the code no longer declares.
	Check() error
	// Versions are those new instances took during the input being handled
	// (then forgotten), journaled with it; Pin gives a replay those of its entry.
	Versions() map[string]int
	Pin(versions map[string]int)
	RunEnded(c platform.Caller, run RunEnd, now time.Time)
}

// Runs are agent runs a flow's agent step starts and signals.
type Runs interface {
	// Start puts a new run with the decision r.
	Start(c platform.Caller, r *pb.ChangeRecord, run RunStart, now time.Time)
	Signal(c platform.Caller, r *pb.ChangeRecord, run string, s RunSignal, now time.Time)
	// Finished are the IDs of a flow instance's finished runs (at step, when
	// given), oldest first.
	Finished(c platform.Caller, flow, step string) []string
}

// RunStart is an agent run a flow's step starts: agent is "<app>.<name>".
type RunStart struct {
	ID, Agent, Goal, Ref, Flow, Step string
	Token                            int
}

// RunSignal is what people made of a run's result.
type RunSignal struct {
	At               time.Time
	Kind, By, Detail string
}

// RunEnd is how a flow's agent run ended: done, with its result, or stopped.
type RunEnd struct {
	ID, Agent, Flow, State, Result, Stopped string
	Token                                   int
}

// Tasks serves Caller.Assign for every app and closes tasks a platform app
// no longer waits for.
type Tasks interface {
	Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error
	Close(c platform.Caller, r *pb.ChangeRecord, id string)
}

// Observer sees each accepted input's events inside the input, before any
// delivery, and again in replay: the platform's own views derived from events
// (the timeline) show them in the same input. events are the protocol events
// the decision is.
type Observer interface {
	Observe(e platform.Event, events []string)
}

// AcceptedObserver freezes an event projection and stages its notifications.
// Applying it is a pure projection update, not a call to today's Observe code.
type AcceptedObserver interface {
	Observer
	PlanObserved(c platform.Caller, e platform.Event, events []string) (json.RawMessage, error)
	ValidateObserved(json.RawMessage) error
	ApplyObserved(json.RawMessage) error
}

// Linker serves Caller.Link and Caller.Links for every app.
type Linker interface {
	Links(c platform.Caller, entity string) []string
	Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error
}

// Directory answers who belongs where in the organisation, for
// Caller.Units, approvals along the organisation and notifications to a unit's role.
type Directory interface {
	// Units are the units party ("member:<id>") belongs to on day, and those
	// below them in structure ("" for the units themselves).
	Units(party, structure string, day platform.Date) []string
	// Below expands units only through active placements of this structure.
	Below(units []string, structure string, day platform.Date) []string
	// Holders are the members with role (any, when empty) in unit or a unit above it in structure on day.
	Holders(structure, unit, role string, day platform.Date) []string
	// Calendar is the working calendar of party ("member:<id>"; "" for the
	// tenant's) on day.
	Calendar(party string, day platform.Date) platform.Calendar
	// Element is an element of the enterprise model live on day.
	Element(id string, day platform.Date) (platform.ElementInfo, bool)
	// Related are the elements joined to element by a relationship stereotype
	// on day: those it points at (outgoing) or those pointing at it.
	Related(element, stereotype string, outgoing bool, day platform.Date) []string
}

// internal/host/agents.go consolidated into this owner

// AgentObservation is one original authorized read with its dependency refs.
// It carries immutable response bytes, not a second query or state engine.
type AgentObservation struct {
	Body    json.RawMessage
	Sources []string
}

// DynamicAgentDefinitions owns the publication version of controlled agents.
// The agent runtime does not depend on a concrete builder implementation.
type DynamicAgentDefinitions interface {
	platform.App
	AgentStamp() string
	AgentVersion(name string) int
}

// AgentHost is the execution and read context actually consumed by Agents.
// The owner never receives the tenant's component fields or record-store locks.
type AgentHost interface {
	ApplicationBindings(platform.AssetRef, string, string) ([]platform.AssetBinding, error)
	AppCatalog
	Automation(platform.Caller, string) platform.Caller
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Member(string) (platform.Member, bool)
	OwnerOf(string) (string, bool)
	Record(string) (any, bool)
	Entity(string) (platform.EntityInfo, bool)
	ActiveRelease() string
	AgentTasks() TaskClosure
	AgentFlows() AgentFlowCompletion
	ProtocolActions(string) []platform.Action
	ProtocolProvider(string) (platform.App, string, bool)
	InvokeDecision(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)
	AgentContext(*platform.Member, string, string, time.Time) (AgentObservation, *kernel.Error)
	AgentSearch(*platform.Member, string, time.Time) AgentObservation
	AgentQuery(platform.Member, platform.NamedQuery, string, time.Time) (AgentObservation, *kernel.Error)
	AgentRead(platform.Member, string, ...json.RawMessage) (AgentObservation, *kernel.Error)
	AgentSources(any) []string
	AgentKnowledge(*platform.Member, string, string, int, time.Time) json.RawMessage
	AgentAppReader(string) *platform.Member
	AgentMeaning(string) string
	AgentGlossary(string) string
	AgentLanguage(string) string
	AgentSuspended(string) bool
	ExternalAgentMembers() []platform.Member
	AgentDecisionMembers() []string
	AgentUsage() []platform.AIUsage
	BeginAgentRead() func()
	ScopeAgentRun(string) func()
	ResolveAgentModel(string, platform.ModelOptions) (platform.AIModelBinding, platform.AIProvider, *kernel.Error)
	AgentModel(string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	ReserveAgentModel(platform.Member, platform.AIModel, time.Time) string
	AgentProviderAvailable(string, time.Time) bool
	CallAgentModel(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	MeterAgentUsage(platform.Member, platform.AIUsage) *kernel.Error
	ReplayAgentUsage(platform.AIUsage)
}

type AttachedAgent interface{ AttachAgent(AgentHost) }

// Candidate declarations are compiled by their original owner. Evaluations
// retain this immutable selection while all reads still use current grants.
type AgentEvaluationDefinitions interface {
	AgentEvaluationApp(candidate, agent string) (platform.App, string, error)
}

// The agent owner closes its own waiting task and reports a completed run;
// it cannot install, pin or redeclare a flow through either collaborator.
type TaskClosure interface {
	Close(platform.Caller, *pb.ChangeRecord, string)
}
type AgentFlowCompletion interface {
	RunEnded(platform.Caller, RunEnd, time.Time)
}

// internal/host/blueprint.go consolidated into this owner

// BlueprintCatalog exposes only the installed Package owner's sealed,
// licensed definition set. Consumers cannot change installation or grants.
type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error)
}

// internal/host/books.go consolidated into this owner

// BooksChecks validates accepted-effect preparations through the actual books
// owner; the host neither inspects its private chart nor recreates its policy.
type BooksChecks interface {
	CheckPosting(platform.Caller, platform.Journal) (platform.Outcome, *kernel.Error)
	CheckReversal(platform.Caller, string, json.RawMessage, time.Time) (platform.Outcome, *kernel.Error)
	HistoricalPostingMessage(string) string
}

// internal/host/build_definitions.go consolidated into this owner

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
	ConnectionConfiguration(construction.Connection) (construction.Connection, error)
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
	BeginAssetBootstrap([]platform.ReleaseAsset) (func(), error)
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

// internal/host/confirmation.go consolidated into this owner

// The caller provides the private transport; the attached owner also reads
// current reviewer membership. There is no independent decision or scheduler.
type ConfirmationHost interface {
	platform.ConfirmationTransport
	Holding(string, string) []string
}

// internal/host/consumer.go consolidated into this owner

// Consumer roles expose only the host methods each compiled owner uses.

type BuildHost interface {
	SystemDefinitionReadOnly(platform.AssetRef) bool
	Action(schema string) (app string, a platform.Action, ok bool)
	Automation(parent platform.Caller, app string) platform.Caller
	BindInterfaceQuery(platform.NamedQuery, ...platform.EntityInfo) (platform.NamedQuery, error)
	Caller(parent platform.Caller, m platform.Member, app string) platform.Caller
	Declares(name string) bool
	Entity(typ string) (platform.EntityInfo, bool)
	EntitySource(typ string) (platform.AssetBinding, bool)
	Function(app, name string, version int) (platform.AIFunction, int, bool)
	Install(c platform.Caller, e platform.Entity, actions []platform.Action, pages ...platform.Page) error
	InstallApplication(c platform.Caller, a platform.Application) error
	InstallFunction(c platform.Caller, f platform.AIFunction, version int) error
	InstallLinkType(c platform.Caller, l platform.LinkType, version int) error
	InstallOperation(c platform.Caller, o platform.Operation, version int) error
	InstallPage(c platform.Caller, p platform.Page) error
	InstallPropertyType(c platform.Caller, p platform.PropertyType, version int) error
	InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error
	Interfaces() []platform.Interface
	Member(id string) (platform.Member, bool)
	Operation(app, name string, version int) (platform.Operation, int, bool)
	RuleQuery(app, name string, version int) (platform.NamedQuery, int, bool)
	BuildProcesses() ProcessDefinitions
	Record(ref string) (any, bool)
	ResolvePropertyType(binding platform.AssetBinding) (platform.PropertyType, bool)
	ValidateInstallApplication(a platform.Application) error
	ValidateInstallDependents(c platform.Caller, e platform.Entity, actions []platform.Action, generatedPage string, pages ...platform.Page) error
	ValidateInstallFunction(f platform.AIFunction) error
	ValidateInstallLinkType(l platform.LinkType) error
	ValidateInstallOperation(c platform.Caller, o platform.Operation) error
	ValidateInstallPage(p platform.Page) error
	ValidateInstallPropertyType(p platform.PropertyType) error
	ValidateInstallQuery(q platform.NamedQuery) error
	ValidateCodeAsset(platform.ReleaseAsset) error
}

type RecordSetHost interface {
	ValidateRecordSet(platform.EntityInfo, platform.RecordSet) error
}

// ModelPageHost checks a publisher's model reads through the original member
// projection. It adds no installation or business-decision authority.
type ModelPageHost interface {
	CheckPageModel(platform.Caller, platform.Page) error
	ModelQueryBinding(platform.AssetRef) (platform.AssetBinding, bool)
}

type FileHost interface {
	Readable(m platform.Member, ref string, now time.Time) bool
	Stored(tenant, hash string) bool
}

type KnowledgeHost interface {
	Declares(name string) bool
	Automation(parent platform.Caller, app string) platform.Caller
}

type RelationsHost interface {
	OwnerOf(dataClass string) (app string, ok bool)
	ProtocolEvent(name string) (platform.ProtocolEvent, bool)
	As(c platform.Caller, app string) platform.Caller
	Automation(parent platform.Caller, app string) platform.Caller
	Member(id string) (platform.Member, bool)
	Readable(m platform.Member, ref string, now time.Time) bool
}

type AttachedBuild interface{ AttachBuild(BuildHost) }
type AttachedFiles interface{ AttachFiles(FileHost) }
type AttachedKnowledge interface{ AttachKnowledge(KnowledgeHost) }
type AttachedRelations interface{ AttachRelations(RelationsHost) }

// Attach is composition plumbing. Each owner receives its narrow named role.
func Attach(app platform.App, h Host) {
	if owner, ok := app.(platform.AttachedSystemDefinitions); ok {
		owner.AttachSystemDefinitions(h)
	}
	if owner, ok := app.(AttachedAgent); ok {
		owner.AttachAgent(h)
	}
	if owner, ok := app.(ConsoleAttached); ok {
		owner.AttachConsole(h)
	}
	if owner, ok := app.(AttachedBuild); ok {
		owner.AttachBuild(h)
	}
	if owner, ok := app.(AttachedFiles); ok {
		owner.AttachFiles(h)
	}
	if owner, ok := app.(AttachedKnowledge); ok {
		owner.AttachKnowledge(h)
	}
	if owner, ok := app.(AttachedRelations); ok {
		owner.AttachRelations(h)
	}
	if owner, ok := app.(AttachedFlow); ok {
		owner.AttachFlow(h)
	}
	if owner, ok := app.(AttachedWork); ok {
		owner.AttachWork(h)
	}

}

// Glossary supplies only read-only terms to search and translation consumers.
type Glossary interface {
	Terms(app string) []platform.KnowledgeTerm
	TermsFor(declaration string) []string
}

// ProcessDefinitions is the declaration/compatibility surface used by Build.
type ProcessDefinitions interface {
	Install(platform.App, platform.Flow) error
	Validate(platform.App, platform.Flow) error
	HasRunningDependency(string) (bool, error)
	HasUnboundRunningDependency(string) (bool, error)
}

// internal/host/credentials.go consolidated into this owner

// Credentials is the original governance owner's encrypted state, read without its directory lock.
type Credentials interface {
	Credential(string) (CredentialState, bool)
}
type CredentialState struct {
	platform.CredentialInfo
	SealedVersion int    `json:"sealedVersion,omitempty"`
	Sealed        string `json:"sealed,omitempty"`
}

// internal/host/dispatch.go consolidated into this owner

type DispatchHost interface {
	platform.DispatchTransport
	Holding(string, string) []string
}

// internal/host/enterprise.go consolidated into this owner

// EnterpriseQueries is the typed member read capability, separate from the
// directory's private model mutation and metamodel storage.
type EnterpriseQueries interface {
	Query(stereotypes []string, kind, words, alive string, limit int, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error)
	Resolve(ids []string, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error)
}

// internal/host/flow.go consolidated into this owner

// FlowHost is the runtime surface consumed by the original Flow owner.
// Declarations, member reads and execution collaborators remain explicit;
// an instance cannot access unrelated installation or host management APIs.
type FlowHost interface {
	Automation(platform.Caller, string) platform.Caller
	Member(string) (platform.Member, bool)
	Record(string) (any, bool)
	Declares(string) bool
	Calendar(string, platform.Date) platform.Calendar
	Tasks() Tasks
	Runs() Runs
	BindFlow(string, string, int, FlowBinding) (FlowBinding, error)
	FlowExecution(platform.Caller, string, string, int, FlowBinding) (FlowHost, error)
	Invoke(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *kernel.Error)
	Action(string) (string, platform.Action, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	OwnerOf(string) (string, bool)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type AttachedFlow interface{ AttachFlow(FlowHost) }

// internal/host/identity.go consolidated into this owner

// CompositionReady runs only after every application and its declarations
// have been installed successfully. Attach supplies the host earlier; Ready
// must not publish access configuration while composition is incomplete.
type CompositionReady interface {
	Ready() error
}

// IdentityDirectory owns the distinction between an authenticated subject and
// a durable member identity. Consumers never inspect its maps or take its lock.
// The private accepted-state owner implements this same role for staged reads.
type IdentityDirectory interface {
	platform.App
	MemberByID(id string) (platform.Member, bool)
	MemberBySubject(subject string) (platform.Member, bool)
	Holding(app, role string) []string
	AgentMembers() []platform.Member
	UnitGrantUsage(unit, until string) []UnitGrantUsage
	Language(member string) string
}

// UnitGrantUsage names a member whose current or retained grants use a unit.
type UnitGrantUsage struct {
	Member  string
	Current bool
}

// AppCatalog is the compiled declaration set used by governance and agents.
// Reading it does not grant access to application records or change ownership.
type AppCatalog interface {
	Apps() []platform.App
	App(id string) platform.App
	AppIDs() []string                      // cached: governance reads it per request
	Manifest(app string) platform.Manifest // cached: governance reads it per request
}

// ConsoleHost supplies only tenant governance projections and artifact storage.
// Member/session/access decisions remain in the identity owner.
type ConsoleHost interface {
	AppCatalog
	CodeReleaseAssets() ([]platform.ReleaseAsset, error)
	ConsoleSetting(name string) string
	Languages() []string
	TenantSuspended() bool
	GovernanceRead(platform.Caller, string) (any, *kernel.Error)
	ConnectorHeartbeat(platform.Caller, time.Time) *kernel.Error
	PutPackageArtifact(key string, raw []byte) error
	GetPackageArtifact(key string) ([]byte, error)
	BaseModules() []platform.CapabilityModule
}

type ConsoleAttached interface{ AttachConsole(ConsoleHost) }

// internal/host/system.go consolidated into this owner

// SystemBuildRequest is one approved system change handed to the builder: the
// frozen file set, already reduced to path and content by the owning app.
type SystemBuildRequest struct {
	Change       string
	Base         int
	Source       string
	SourceDigest string
	Files        map[string]string
	Deleted      []string
}

// SystemBuildResult is the evidence one build leaves: what it produced, with
// which toolchain, and why it failed when it did.
type SystemBuildResult struct {
	OK          bool
	Work        string
	Binary      string
	Web         string
	Toolchain   string
	Diagnostics string
	// Manifest is the digest of every file the change froze, so a release can
	// be exported and checked against what was built rather than rebuilt.
	Manifest     map[string]string
	Source       string
	SourceDigest string
	Duration     time.Duration
}

// internal/host/transaction.go consolidated into this owner

type TransactionHost interface {
	CheckTxnCall(platform.TxnCall) error
}

// internal/host/work.go consolidated into this owner

// WorkHost provides the original approval/task owner's actual collaborators.
type WorkHost interface {
	Action(string) (string, platform.Action, bool)
	Member(string) (platform.Member, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Automation(platform.Caller, string) platform.Caller
	Holding(string, string) []string
	Calendar(string, platform.Date) platform.Calendar
	Units(string, string, platform.Date) []string
	Holders(string, string, string, platform.Date) []string
	Recipients(platform.Caller, time.Time, []platform.Recipient) []string
	Attempt(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	ActiveRelease() string
	ApprovalRelease(string, string, string) (string, *kernel.Error)
	ApprovalExecution(platform.Caller, platform.ApprovalRequest) (WorkHost, *kernel.Error)
	Seen(platform.Caller, ...string)
}

type AttachedWork interface{ AttachWork(WorkHost) }
