package platform

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Member struct {
	authorization     AuthorizationEngine
	AIContext         bool              `json:"-"`
	Markings          []string          `json:"markings,omitempty"`
	Declassifications []string          `json:"declassifications,omitempty"`
	ID                string            `json:"id"`
	Tenant            string            `json:"tenant"`
	Roles             map[string]string `json:"roles"`
	Grants            []Grant           `json:"grants,omitempty"`
	Status            string            `json:"status,omitempty"`
	Scopes            []string          `json:"scopes,omitempty"`
	Agent             bool              `json:"agent,omitempty"`
	Language          string            `json:"language,omitempty"`
	Timezone          string            `json:"timezone,omitempty"`
}

type Grant struct {
	App       string    `json:"app"`
	Role      string    `json:"role"`
	Unit      string    `json:"unit,omitempty"`
	Structure string    `json:"structure,omitempty"`
	From      string    `json:"from,omitempty"`
	Until     string    `json:"until,omitempty"`
	By        string    `json:"by,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	At        time.Time `json:"at,omitzero"`
}

func (g Grant) Active(day string) bool {
	return (g.From == "" || g.From <= day) && (g.Until == "" || day < g.Until)
}

func (m Member) RolesIn(app string) []string {
	var out []string
	if r := m.Roles[app]; r != "" {
		out = append(out, r)
	}
	for _, g := range m.Grants {
		if g.App == app && !slices.Contains(out, g.Role) {
			out = append(out, g.Role)
		}
	}
	return out
}

func (m Member) Holds(app, role string) bool { return slices.Contains(m.RolesIn(app), role) }

func (m Member) May(app, permission string, roles []string) AuthzDecision {
	return m.authorization.Decide(AuthorizationInput{Subject: AuthorizationSubject{ID: m.ID, App: app, Roles: m.RolesIn(app), Agent: m.Agent}, Permission: permission, Allowed: roles})
}

func (m Member) AuthorizationEngine() AuthorizationEngine { return m.authorization }

func (m Member) WithAuthorization(engine AuthorizationEngine) Member {
	m.authorization = engine
	return m
}

func (m Member) Active() bool { return m.Status != MemberSuspended && m.Status != MemberLeft }

func (m Member) InScope(permission string) bool {
	return m.Scopes == nil || slices.ContainsFunc(m.Scopes, func(p string) bool {
		return p == permission || strings.HasSuffix(p, "*") && strings.HasPrefix(permission, strings.TrimSuffix(p, "*"))
	})
}

const (
	MemberInvited   = "invited"
	MemberSuspended = "suspended"
	MemberLeft      = "left"
)

func (m Member) Location() *time.Location {
	if m.Timezone != "" {
		if loc, err := time.LoadLocation(m.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

func (m Member) Today(now time.Time) string { return now.In(m.Location()).Format(time.DateOnly) }

type Caller struct {
	Member
	App              string
	Replaying        bool
	Automation       bool
	sourceProjection bool
	restoreSource    bool
	inputRefused     func(*kernel.Error, []FieldIssue)
	rt               Runtime
	creationDepth    int
	transactionInput *pb.Submission
	agentRun         string
}

func (c Caller) WithSourceProjection(restore bool) Caller {
	c.Automation, c.sourceProjection, c.restoreSource = true, true, restore
	return c
}

func (c Caller) AgentRun() string { return c.agentRun }

func (c Caller) TransactionInput(receipt *pb.ChangeRecord) *pb.Submission {
	input := c.transactionInput
	if input == nil {
		input = receipt.GetSubmission()
	}
	return proto.Clone(input).(*pb.Submission)
}

func (c Caller) WithTransactionInput(input *pb.Submission) Caller {
	c.transactionInput = proto.Clone(input).(*pb.Submission)
	return c
}

func (c Caller) WithInputDiagnostics(report func(*kernel.Error, []FieldIssue)) Caller {
	c.inputRefused = report
	return c
}

func (c Caller) RolesHere() []string { return c.RolesIn(c.App) }

func (c Caller) Staging() bool {
	_, ok := c.rt.(StagedRuntime)
	return ok
}

type DecisionFailureRuntime interface{ FailDecision(*kernel.Error) }

type DecisionDeclarationRuntime interface {
	DecisionCatalog(*Ledger) *Catalog
}

func (c Caller) FailDecision(err *kernel.Error) {
	if err == nil {
		return
	}
	runtime, ok := c.rt.(DecisionFailureRuntime)
	if !ok || c.Replaying || !c.Staging() {
		panic("decision failure requires private staging")
	}
	runtime.FailDecision(err)
}

type AcceptedPublisher interface {
	AcceptedPublicationSchemas() []string
	ValidateAcceptedPublication(schema string, image []byte) error
	ApplyAcceptedPublication(schema string, image []byte) error
}

type AcceptedStateApp interface {
	App
	ForkAcceptedState() (App, error)
	AcceptedState() (json.RawMessage, error)
	ValidateAcceptedState(json.RawMessage) error
	ApplyAcceptedState(json.RawMessage) error
}

type AcceptedHistoryApp interface {
	UseAcceptedHistory(load func() (json.RawMessage, error))
}

type DecisionForkApp interface {
	App
	ForkDecision() (App, error)
}

type AcceptedInputApp interface {
	App
	InputReceiver
	AcceptedInputs() []string
	DecodeAcceptedInput(json.RawMessage) (any, error)
}

type AcceptedFactApp interface {
	HasAcceptedFact(tenant, id string) bool
}

type AuthorizationEngineRuntime interface{ AuthorizationEngine() AuthorizationEngine }

func NewCaller(rt Runtime, m Member, app string, replaying, automation bool) Caller {
	if provider, ok := rt.(AuthorizationEngineRuntime); ok {
		m = m.WithAuthorization(provider.AuthorizationEngine())
	}
	return Caller{Member: m, App: app, Replaying: replaying, Automation: automation, rt: rt}
}

func RouteCaller(c Caller, app string) Caller {
	c.App = app
	return c
}

func ActingCaller(parent Caller, m Member, app string, automation bool) Caller {
	return NewCaller(parent.rt, m, app, parent.Replaying, automation)
}

func AgentCaller(parent Caller, m Member, app, run string) Caller {
	c := ActingCaller(parent, m, app, true)
	if m.Agent {
		c.agentRun = run
	}
	return c
}

func Attempt(c Caller, decide func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	if c.rt == nil {
		return decide()
	}
	return c.rt.Attempt(decide)
}

func MarkSeen(c Caller, keys ...string) {
	if c.rt != nil {
		c.rt.Seen(c, keys...)
	}
}

func ProbeDecision(c Caller, decide func(Caller) *kernel.Error) *kernel.Error {
	if c.rt == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return c.rt.ProbeDecision(c, decide)
}

func Decide(c Caller, app App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if c.Simulating() {
		if action, ok := app.Manifest().Actions.Action(s.GetSchema().GetName()); ok && action.Adoption != nil {
			return nil, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Proposal adoption does not execute in isolated trials")
		}
	}
	c = RouteCaller(c, app.Manifest().ID)
	if c.rt == nil {
		return app.Submit(c, s, at)
	}
	return c.rt.Decide(c, app, s, at)
}

type DecisionRuntime interface {
	Decide(Caller, App, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	Attempt(func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error)
	ProbeDecision(Caller, func(Caller) *kernel.Error) *kernel.Error
}

type ReadRuntime interface {
	ReadQuery(Caller, string, string, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	ReadQueryVersion(Caller, string, string, int, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	ReadRecord(Caller, string, string, time.Time) (json.RawMessage, *kernel.Error)
	ReadRecordPath(Caller, string, string, []string, time.Time) (json.RawMessage, []string, *kernel.Error)
}

type RuleReadRuntime interface {
	ReadRuleQuery(Caller, RuleCollection, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
}

type OperationRuntime interface {
	RequestOperation(Caller, *pb.ChangeRecord, OperationRequest) (OperationCall, *kernel.Error)
	OperationResult(Caller, string) (OperationResult, *kernel.Error)
	CancelOperation(Caller, *pb.ChangeRecord, string) *kernel.Error
	RequestCompilation(Caller, *pb.ChangeRecord, string, string, CodeBuildRequest) (OperationCall, *kernel.Error)
}

type FunctionRuntime interface {
	RequestFunction(Caller, *pb.ChangeRecord, FunctionRequest) (FunctionCall, *kernel.Error)
}

type ApprovalRuntime interface {
	RequestApproval(Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type NotificationRuntime interface {
	Notify(Caller, Notification, time.Time, []Recipient) []string
	Seen(Caller, ...string)
}

type InputDiagnosticRuntime interface {
	InputRefused(*kernel.Error, []FieldIssue)
}

type ControlRuntime interface {
	ControlDecision(Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *kernel.Error)
}

func ControlDecision(c Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.rt == nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Host controls require a private decision")
	}
	return c.rt.ControlDecision(c, s, at)
}

type StagedRuntime interface {
	Runtime
	HoldChanges(*Ledger) (release func(), refusal *kernel.Error)
	DraftChanges(*Ledger, func() ChangeLog) ChangeLog
}

type Runtime interface {
	DecisionRuntime
	ReadRuntime
	OperationRuntime
	FunctionRuntime
	ApprovalRuntime
	NotificationRuntime
	InputDiagnosticRuntime
	ControlRuntime
	Publish(c Caller, record *pb.ChangeRecord)
	Probe(c Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error
	Request(c Caller, r *pb.ChangeRecord, q Request)
	Query(c Caller, protocol, read string, provider ...string) ([]ProviderResult, *kernel.Error)
	BoundProvider(c Caller, protocol string) (string, *kernel.Error)
	Setting(c Caller, name string) string
	Emit(c Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error)
	Units(c Caller, structure string, now time.Time) []string
	Element(c Caller, id string, now time.Time) (ElementInfo, bool)
	Related(c Caller, element, stereotype string, outgoing bool, now time.Time) []string
	Links(c Caller, entity string) []string
	Link(c Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error
	Deliver(c Caller, dataClass, from, to string, now time.Time) *kernel.Error
	Put(c Caller, r *pb.ChangeRecord, entity any) *kernel.Error
	Get(c Caller, t reflect.Type, id string) (any, bool)
	Find(c Caller, t reflect.Type, q Query) ([]any, int, *kernel.Error)
	Check(c Caller, entity any) *kernel.Error
	Assign(c Caller, r *pb.ChangeRecord, a Assignment) *kernel.Error
	Probing() bool
	Readable(c Caller, ref string) bool
	Next(c Caller, r *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error)
}

type Manifest struct {
	ID            string
	Title         string
	Version       string
	Actions       *Catalog
	Reads         []string
	ReadInputs    map[string][]Field
	ReadSources   map[string]string
	Inputs        map[string]bool
	Subscribes    []string
	Provides      []Provision
	Consumes      []Consumption
	Everyone      []string
	Jobs          []Job
	Retry         *Retry
	Settings      []Setting
	Emits         []EffectKind
	Entities      []Entity
	Pages         []Page
	LinkTypes     []LinkType
	PropertyTypes []PropertyType
	ValueTypes    []ValueType
	Interfaces    []Interface
	Queries       []NamedQuery
	Functions     []AIFunction
	Operations    []Operation
	Observations  []ObservationDefinition
	Flows         []Flow
	Agents        []Agent
	Sequences     []Sequence
	Roles         []string
	Languages     Languages
}

func (m Manifest) AllRoles() []string {
	out := m.Actions.Roles()
	for _, r := range m.Roles {
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

type App interface {
	AcceptedLedger() *Ledger
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type Reader interface {
	Read(c Caller, name string) (any, *kernel.Error)
}

type ParameterizedReader interface {
	ReadInput(c Caller, name string, inputs json.RawMessage) (any, *kernel.Error)
}

type InputReceiver interface {
	Input(c Caller, name string, body []byte, now time.Time) (any, *kernel.Error)
}

type Event struct {
	App     string
	Record  *pb.ChangeRecord
	Changed []string
}

type Subscriber interface {
	Handle(c Caller, e Event) *kernel.Error
}

type Runner interface {
	Run(c Caller, job string, now time.Time) *kernel.Error
}

type Answerer interface {
	Answer(c Caller, e Effect, o Outcome, now time.Time) *kernel.Error
}

func notFound() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }

var slot = regexp.MustCompile(`\{\w+\}`)

func Refuse(code pb.ErrorCode, text string, values ...any) *kernel.Error {
	i := 0
	message := slot.ReplaceAllStringFunc(text, func(p string) string {
		if i >= len(values) {
			return p
		}
		i++
		return fmt.Sprint(values[i-1])
	})
	return &kernel.Error{Code: code, Message: message}
}

const (
	actionOutsideAcceptedBoundary = "This action uses an effect outside the accepted-result boundary"
	inputOutsideAcceptedBoundary  = "This input uses an effect outside the accepted-result boundary"
)

func RefuseOutsideAcceptedBoundary() *kernel.Error {
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: actionOutsideAcceptedBoundary}
}

func RefuseInputOutsideAcceptedBoundary() *kernel.Error {
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: inputOutsideAcceptedBoundary}
}

func IsOutsideAcceptedBoundary(err *kernel.Error) bool {
	return err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT &&
		(err.Message == actionOutsideAcceptedBoundary || err.Message == inputOutsideAcceptedBoundary)
}

type CompositionReady interface {
	Ready() error
}

type IdentityDirectory interface {
	App
	MemberByID(id string) (Member, bool)
	MemberBySubject(subject string) (Member, bool)
	Holding(app, role string) []string
	AgentMembers() []Member
	UnitGrantUsage(unit, until string) []UnitGrantUsage
	Language(member string) string
}

type UnitGrantUsage struct {
	Member  string
	Current bool
}

type AppCatalog interface {
	Apps() []App
	App(id string) App
	AppIDs() []string
	Manifest(app string) Manifest
}

type ConsoleHost interface {
	PrepareManagedPackage(Caller, PackageDescriptor, []ReleaseAsset, bool, bool, bool) (func(*pb.ChangeRecord), *kernel.Error)
	AppCatalog
	CodeReleaseAssets() ([]ReleaseAsset, error)
	ConsoleSetting(name string) string
	Languages() []string
	TenantSuspended() bool
	GovernanceRead(Caller, string) (any, *kernel.Error)
	ConnectorHeartbeat(Caller, time.Time) *kernel.Error
	PutPackageArtifact(key string, raw []byte) error
	GetPackageArtifact(key string) ([]byte, error)
	BaseModules() []CapabilityModule
	ProductVersions() []ProductVersionView
}

type ConsoleAttached interface{ AttachConsole(ConsoleHost) }

type Listener interface {
	Interested(names []string, e Event) bool
	Listen(c Caller, e Event, names []string, now time.Time) *kernel.Error
}

type Observer interface {
	Observe(e Event, events []string)
}

type AcceptedObserver interface {
	Observer
	PlanObserved(c Caller, e Event, events []string) (json.RawMessage, error)
	ValidateObserved(json.RawMessage) error
	ApplyObserved(json.RawMessage) error
}

type Linker interface {
	Links(c Caller, entity string) []string
	Link(c Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error
}

type AuthzRequest struct {
	Principal string   `json:"principal"`
	Tenant    string   `json:"tenant"`
	Kind      string   `json:"kind" enum:"read,write,create,archive,action,derive,export,protocol"`
	Type      string   `json:"type,omitempty"`
	Record    string   `json:"record,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Action    string   `json:"action,omitempty"`
	From      string   `json:"from,omitempty"`
}

type AuthzDecision = Verdict

type AuthzLayer = Layer

type AuthzCollection struct {
	Decision AuthzDecision  `json:"decision"`
	Elements []AuthzElement `json:"elements"`
	Filtered *AuthzDecision `json:"filtered,omitempty"`
}

type AuthzElement struct {
	Record   string        `json:"record"`
	Decision AuthzDecision `json:"decision"`
}

type AuthorizationRuntime interface {
	Authorize(Caller, AuthzRequest, time.Time) AuthzDecision
}

type AuthorizationRecorder interface {
	RecordAuthorization(Caller, AuthzRequest, AuthzDecision)
}

func (c Caller) AuthorizationRecorder() AuthorizationRecorder {
	if recorder, ok := c.rt.(AuthorizationRecorder); ok {
		return recorder
	}
	return nil
}

func (c Caller) Authorize(request AuthzRequest, at time.Time) AuthzDecision {
	if r, ok := c.rt.(AuthorizationRuntime); ok {
		return r.Authorize(c, request, at)
	}
	return AuthzDecision{Rule: "none", Reason: "Authorization requires the owner runtime", Layers: []AuthzLayer{{Name: "principal", Reason: "Authorization requires the owner runtime"}}}
}
