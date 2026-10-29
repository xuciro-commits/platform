package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io/fs"
	"path"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform/authz"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Member is a person, service or AI agent of a tenant. Grants are
// what they hold: roles in apps, each optionally bounded to a
// unit and a time. Roles is derived from the active grants by the host — the
// primary role per app (the app's first declared role the member holds) —
// for the many places that ask for one; RolesIn answers with all of them.
// What a rule scopes by is the member's organisation.
type Member struct {
	authorization authz.Engine
	AIContext     bool              `json:"-"` // trusted read narrowing, never a grant or request field
	ID            string            `json:"id"`
	Tenant        string            `json:"tenant"`
	Roles         map[string]string `json:"roles"`
	Grants        []Grant           `json:"grants,omitempty"`
	// Status is the member's standing: "" active, invited
	// (added, not yet signed in), suspended (holds nothing until resumed),
	// left (offboarded: no subjects, no grants; the record stays for history).
	Status string `json:"status,omitempty"`
	// Scopes bound a caller signed in with a personal token:
	// permission patterns (exact, or a prefix ending in *); nil is unbounded.
	Scopes []string `json:"scopes,omitempty"`
	// Agent marks an AI agent: an irreversible effect it causes waits for a
	// person's approval, and it cannot give one.
	Agent bool `json:"agent,omitempty"`
	// Language is the member's own choice, such as
	// "zh-CN", derived from their profile when the host hands a member to an
	// app; empty: the tenant's default, else what the browser asks for.
	Language string `json:"language,omitempty"`
	// Timezone is the IANA zone the member works in, derived
	// from their profile over the tenant's default when the host hands a
	// member to an app; empty: UTC. Every "today" a decision defaults for
	// the member is this zone's day.
	Timezone string `json:"timezone,omitempty"`
}

// Grant is one role a member holds in an app: everywhere in
// the app, or within a unit of a structure; from a day, until a day; given by
// someone for a reason. Dates are YYYY-MM-DD, inclusive from, exclusive until.
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

// Active reports whether the grant holds on day (YYYY-MM-DD).
func (g Grant) Active(day string) bool {
	return (g.From == "" || g.From <= day) && (g.Until == "" || day < g.Until)
}

// RolesIn are every role the member holds in app, the primary first.
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

// Holds reports whether the member holds role in app, among any of their roles.
func (m Member) Holds(app, role string) bool { return slices.Contains(m.RolesIn(app), role) }

// May asks the engine whether the member may exercise a
// permission of app that names roles: any role they hold there suffices.
func (m Member) May(app, permission string, roles []string) authz.Verdict {
	engine := m.authorization
	if engine == nil {
		engine = authz.Standard{}
	}
	return engine.Decide(authz.Request{Subject: authz.Subject{ID: m.ID, App: app, Roles: m.RolesIn(app), Agent: m.Agent}, Permission: permission, Allowed: roles})
}

func (m Member) WithAuthorization(engine authz.Engine) Member {
	m.authorization = engine
	return m
}

// Active reports whether the member may act at all: not suspended, not left.
func (m Member) Active() bool { return m.Status != MemberSuspended && m.Status != MemberLeft }

// InScope reports whether the caller's token scopes, if any, cover permission.
func (m Member) InScope(permission string) bool {
	return m.Scopes == nil || slices.ContainsFunc(m.Scopes, func(p string) bool {
		return p == permission || strings.HasSuffix(p, "*") && strings.HasPrefix(permission, strings.TrimSuffix(p, "*"))
	})
}

// Member standings.
const (
	MemberInvited   = "invited"
	MemberSuspended = "suspended"
	MemberLeft      = "left"
)

// Location is the member's timezone, UTC when unknown.
func (m Member) Location() *time.Location {
	if m.Timezone != "" {
		if loc, err := time.LoadLocation(m.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

// Today is now's date where the member is, as a decision defaults it.
func (m Member) Today(now time.Time) string { return now.In(m.Location()).Format(time.DateOnly) }

// Caller is a member as one app sees it. Replaying marks the journal replay:
// who could act was decided when the input was accepted. Automation
// marks an app acting on its own (a subscribed event, a job, an effect's
// answer), as member "app:<id>"; its manifest is its grant.
type Caller struct {
	Member
	App              string
	Replaying        bool
	Automation       bool
	inputRefused     func(*kernel.Error, []FieldIssue)
	rt               Runtime
	creationDepth    int
	transactionInput *pb.Submission
	agentRun         string
}

// AgentRun is the invocation bound by the agent owner, never a client-supplied
// correlation label. Changing the acting principal drops this binding.
func (c Caller) AgentRun() string { return c.agentRun }

// TransactionInput is invocation context; writes retain their original receipt.
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

// WithInputDiagnostics observes only this call's refused input checks.
func (c Caller) WithInputDiagnostics(report func(*kernel.Error, []FieldIssue)) Caller {
	c.inputRefused = report
	return c
}

// RolesHere are every role the member holds in the app being called.
func (c Caller) RolesHere() []string { return c.RolesIn(c.App) }

// Staging reports that this decision is still private. An app may validate
// a definition now, but must not install it until the accepted result commits.
func (c Caller) Staging() bool {
	_, ok := c.rt.(StagedRuntime)
	return ok
}

type DecisionFailureRuntime interface{ FailDecision(*kernel.Error) }

// DecisionDeclarationRuntime selects immutable owner declarations only for
// a host-validated retained execution. Ledger and decision semantics stay shared.
type DecisionDeclarationRuntime interface {
	DecisionCatalog(*Ledger) *Catalog
}

// FailDecision refuses an apply closure before its private result is sealed.
// It cannot change an already accepted decision or be used during recovery.
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

// AcceptedPublisher installs a definition from an already committed record
// image. This is not a second decision and must never emit external effects.
type AcceptedPublisher interface {
	AcceptedPublicationSchemas() []string
	ValidateAcceptedPublication(schema string, image []byte) error
	ApplyAcceptedPublication(schema string, image []byte) error
}

// AcceptedStateApp owns mutable authoritative state outside host records.
// ForkAcceptedState copies only that owner's decision state, retaining the
// same staged ledger identity and read-only declarations. It must not copy
// running work, locks or the tenant. Applying a saved image is pure recovery,
// not a business decision, and must never publish or dispatch an effect.
type AcceptedStateApp interface {
	App
	ForkAcceptedState() (App, error)
	AcceptedState() (json.RawMessage, error)
	ValidateAcceptedState(json.RawMessage) error
	ApplyAcceptedState(json.RawMessage) error
}

// AcceptedHistoryApp may release its persisted immutable history after the
// original accepted-state image has been saved. Only the owner decodes it.
type AcceptedHistoryApp interface {
	UseAcceptedHistory(load func() (json.RawMessage, error))
}

// DecisionForkApp isolates an owner's decision context and runtime caches while
// retaining immutable declarations. Its fork must have the same app ID and
// ledger identity. This is not an acceptance opt-in: every App is staged, and
// mutable authoritative state still requires AcceptedStateApp's complete image.
type DecisionForkApp interface {
	App
	ForkDecision() (App, error)
}

// AcceptedInputApp is required for Tenant inputs declared as recorded. Its Input
// implementation must mutate only its caller's staged Runtime or an
// AcceptedStateApp fork. DecodeAcceptedInput reconstructs the already saved
// answer on retry; it must not run business rules or external calls.
type AcceptedInputApp interface {
	App
	InputReceiver
	AcceptedInputs() []string
	DecodeAcceptedInput(json.RawMessage) (any, error)
}

// AcceptedFactApp lets an app-owned private fact log satisfy evidence checks
// in child decisions of the same connector input. The live fact log remains
// untouched until the accepted result is durable.
type AcceptedFactApp interface {
	HasAcceptedFact(tenant, id string) bool
}

// As views member from app, without a host (tests of one app).
func As(app string, m Member) Caller { return Caller{Member: m, App: app} }

type AuthorizationEngineRuntime interface{ AuthorizationEngine() authz.Engine }

// NewCaller is how the host runtime hands an app its caller.
func NewCaller(rt Runtime, m Member, app string, replaying, automation bool) Caller {
	if provider, ok := rt.(AuthorizationEngineRuntime); ok {
		m = m.WithAuthorization(provider.AuthorizationEngine())
	}
	return Caller{Member: m, App: app, Replaying: replaying, Automation: automation, rt: rt}
}

// RouteCaller changes the authority being called without replacing the
// decision's runtime. The host uses it for nested decisions; a staged parent
// must never hand its child a live runtime.
func RouteCaller(c Caller, app string) Caller {
	c.App = app
	return c
}

// ActingCaller changes the principal, but keeps the parent's transaction.
// Approval execution and platform work must not acquire a live runtime here.
func ActingCaller(parent Caller, m Member, app string, automation bool) Caller {
	return NewCaller(parent.rt, m, app, parent.Replaying, automation)
}

// AgentCaller binds a managed agent's action to its original execution.
func AgentCaller(parent Caller, m Member, app, run string) Caller {
	c := ActingCaller(parent, m, app, true)
	if m.Agent {
		c.agentRun = run
	}
	return c
}

// Attempt delegates the handling of a nested business refusal to the decision
// owner. Staged runtimes roll back private writes; legacy runtimes preserve
// historical direct execution. A caller made by As has no host savepoint.
func Attempt(c Caller, decide func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	if c.rt == nil {
		return decide()
	}
	return c.rt.Attempt(decide)
}

// MarkSeen stays within the calling decision's notification view.
func MarkSeen(c Caller, keys ...string) {
	if c.rt != nil {
		c.rt.Seen(c, keys...)
	}
}

// ProbeDecision checks rules against a private parent view, applying nothing.
// It cannot temporarily switch a shared tenant's runtime into probing mode.
func ProbeDecision(c Caller, decide func(Caller) *kernel.Error) *kernel.Error {
	if c.rt == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return c.rt.ProbeDecision(c, decide)
}

// Decide uses the caller's decision owner. A host must explicitly handle or
// refuse nested decisions; only callers made by As run without a host.
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

// DecisionRuntime owns nested decisions, handled business refusals and private
// rule checks. A staged owner keeps nested calls within its private result and
// rolls back refused attempts. A legacy owner must explicitly retain its
// direct semantics; implementing this contract alone does not promise atomic
// acceptance or savepoints.
type DecisionRuntime interface {
	Decide(Caller, App, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	Attempt(func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error)
	// ProbeDecision discards private writes and intents on refusal or panic.
	ProbeDecision(Caller, func(Caller) *kernel.Error) *kernel.Error
}

// ReadRuntime serves scoped reads from the calling decision's view. A private
// owner must not substitute live records for its uncommitted writes.
type ReadRuntime interface {
	ReadQuery(Caller, string, string, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	ReadQueryVersion(Caller, string, string, int, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	ReadRecord(Caller, string, string, time.Time) (json.RawMessage, *kernel.Error)
	ReadRecordPath(Caller, string, string, []string, time.Time) (json.RawMessage, []string, *kernel.Error)
}

// RuleReadRuntime captures retained, caller-filtered rule collections. It grants
// no writes and keeps completeness checks with the original record owner.
type RuleReadRuntime interface {
	ReadRuleQuery(Caller, RuleCollection, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
}

// OperationRuntime owns controlled computation and cancellation. A runtime
// outside an accepted decision must explicitly refuse requests.
type OperationRuntime interface {
	RequestOperation(Caller, *pb.ChangeRecord, OperationRequest) (OperationCall, *kernel.Error)
	OperationResult(Caller, string) (OperationResult, *kernel.Error)
	CancelOperation(Caller, *pb.ChangeRecord, string) *kernel.Error
	RequestCompilation(Caller, *pb.ChangeRecord, string, string, CodeBuildRequest) (OperationCall, *kernel.Error)
}

// FunctionRuntime plans model work within its enclosing accepted result.
type FunctionRuntime interface {
	RequestFunction(Caller, *pb.ChangeRecord, FunctionRequest) (FunctionCall, *kernel.Error)
}

// ApprovalRuntime stages an approval request with its parent decision.
type ApprovalRuntime interface {
	RequestApproval(Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

// NotificationRuntime owns notification changes in the current decision.
type NotificationRuntime interface {
	Notify(Caller, Notification, time.Time, []Recipient) []string
	Seen(Caller, ...string)
}

// InputDiagnosticRuntime associates field issues with this decision's refusal.
type InputDiagnosticRuntime interface {
	InputRefused(*kernel.Error, []FieldIssue)
}

// ControlRuntime validates host administration against the current decision's
// private owner. Its callback contributes to that result and cannot publish it.
type ControlRuntime interface {
	ControlDecision(Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *kernel.Error)
}

// ControlDecision keeps administration within the caller's decision owner.
func ControlDecision(c Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.rt == nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Host controls require a private decision")
	}
	return c.rt.ControlDecision(c, s, at)
}

// StagedRuntime is a complete private decision view. DraftChanges is called
// with the ledger lock held; it must return the same private log for that
// ledger throughout the decision and must never return the live log.
type StagedRuntime interface {
	Runtime
	// HoldChanges is entered before taking the ledger lock. It rejects a nested
	// receive of the same ledger, and retains a parent's private log identity
	// across nested savepoints until the returned release function runs.
	HoldChanges(*Ledger) (release func(), refusal *kernel.Error)
	DraftChanges(*Ledger, func() ChangeLog) ChangeLog
}

// Runtime is what the host does for a caller; package platformserver implements
// it. Each method acts as c, for c's app, within the input being handled.
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
	// Records of the app's entity types.
	Put(c Caller, r *pb.ChangeRecord, entity any) *kernel.Error
	Get(c Caller, t reflect.Type, id string) (any, bool)
	Find(c Caller, t reflect.Type, q Query) ([]any, int, *kernel.Error)
	Check(c Caller, entity any) *kernel.Error
	// Work: tasks for people, and whether decisions are only probed.
	Assign(c Caller, r *pb.ChangeRecord, a Assignment) *kernel.Error
	Probing() bool
	// Readable reports whether c's member may read the record ref ("<type>/<id>").
	Readable(c Caller, ref string) bool
	// Next numbers a document of an accepted decision.
	Next(c Caller, r *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error)
}

// Manifest declares an app. Names of actions, reads and inputs are
// unique within a tenant; the host routes by them.
type Manifest struct {
	ID      string
	Title   string // the app's name in the workspace's launcher
	Version string
	Actions *Catalog
	Reads   []string
	// ReadInputs declares optional parameters of named reads. The original
	// reader owns their meaning; HTTP and agent tools share this declaration.
	ReadInputs map[string][]Field
	// ReadSources declares original non-entity results, such as an owner's
	// configuration or retained definitions. Copied/generated business content
	// instead declares source/from on its fields (from names a Go ref field,
	// optionally followed by #sourceField). Registered records always
	// retain their own row, field and derivation checks.
	ReadSources map[string]string
	Inputs      map[string]bool // connector inputs; true: accepted result before live application
	// Subscribes names its own actions, or events of protocols it consumes
	// ("<protocol id>#<event>"), delivered to Handle after commit. Apps know no
	// other app: they reach each other through protocols only.
	Subscribes    []string
	Provides      []Provision   // protocols this app implements
	Consumes      []Consumption // protocols this app uses; the host binds a provider
	Everyone      []string      // reads any member may use; the app filters by caller
	Jobs          []Job         // scheduled work (Runner),
	Retry         *Retry        // how events delivered to it are retried; nil: the host's
	Settings      []Setting     // typed values administrators set in Settings
	Emits         []EffectKind  // outbound effects it sends to endpoints the tenant binds
	Entities      []Entity      // entity types whose records the host keeps
	Pages         []Page        // bounded code page descriptors over those records/actions
	LinkTypes     []LinkType
	PropertyTypes []PropertyType
	Interfaces    []Interface             // shapes several entity types share (A2)
	Queries       []NamedQuery            // named pure queries pages and agents share
	Functions     []AIFunction            // typed, bounded inference through existing model effects
	Operations    []Operation             // typed native or controlled Wasm computation
	Observations  []ObservationDefinition // external summaries and result-only business decisions
	Flows         []Flow                  // long-running processes the host runs for the app
	Agents        []Agent                 // AI agents the host runs for the app
	Sequences     []Sequence              // numbers of its documents, without gaps
	// Roles are roles that grant no action but open something else, such as
	// models; the roles its actions grant need not be listed.
	Roles []string
	// Languages translate the app's titles and descriptions.
	Languages Languages
}

// AllRoles are every role the app defines: those its actions grant and Roles.
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

// App is one app's instance in one tenant.
type App interface {
	// AcceptedLedger is the owner's authoritative ledger. All new decisions
	// are staged; an empty action catalog does not remove this obligation.
	AcceptedLedger() *Ledger
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
}

// Reader serves the named reads declared by an app's manifest.
type Reader interface {
	Read(c Caller, name string) (any, *kernel.Error)
}

// ParameterizedReader serves named reads with the manifest's declared inputs.
type ParameterizedReader interface {
	ReadInput(c Caller, name string, inputs json.RawMessage) (any, *kernel.Error)
}

// InputReceiver handles the connector inputs declared by an app's manifest.
type InputReceiver interface {
	Input(c Caller, name string, body []byte, now time.Time) (any, *kernel.Error)
}

// Event is an accepted decision, delivered to subscribers after commit.
type Event struct {
	App    string
	Record *pb.ChangeRecord
	// Changed are the records ("<type>/<id>") the decision put, whichever it named.
	Changed []string
}

// Subscriber is an app that handles the events its manifest subscribes to, as
// owned work after the input. A refusal is retried, then recorded as
// a failed delivery; the event's decision stands. Handle must depend only on
// tenant state: a replay runs it again and must reach the same outcome.
type Subscriber interface {
	Handle(c Caller, e Event) *kernel.Error
}

// Runner is an app with scheduled jobs. A run acts only through decisions and
// notifications, so a run that did neither needs no journal entry.
type Runner interface {
	Run(c Caller, job string, now time.Time) *kernel.Error
}

// Answerer is an app that hears how its effects ended: delivered with the
// receiver's answer, rejected, or failed. The answer comes from outside, so it is
// journaled with the outcome, and replay hands the app the same answer (D4): the
// app records it as an observation and decides on it; it never changes state by itself.
// A discarded effect is answered too, with the result "discarded", when a
// person discards it or its endpoint is removed (F-24).
type Answerer interface {
	Answer(c Caller, e Effect, o Outcome, now time.Time) *kernel.Error
}

func notFound() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }

var slot = regexp.MustCompile(`\{\w+\}`)

// Refuse is a refusal with why, for people (F-23): text is English with
// {placeholders} filled by values in order — "Line {line} needs an account"
// with 2 reads "Line 2 needs an account" — and the app's dictionary says the
// same text with the placeholders in another language. Only the code is
// contract; the host shows the text in the reader's language.
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
	App
	MemberByID(id string) (Member, bool)
	MemberBySubject(subject string) (Member, bool)
	Holding(app, role string) []string
	AgentMembers() []Member
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
	Apps() []App
	App(id string) App
	AppIDs() []string             // cached: governance reads it per request
	Manifest(app string) Manifest // cached: governance reads it per request
}

// ConsoleHost supplies only tenant governance projections and artifact storage.
// Member/session/access decisions remain in the identity owner.
type ConsoleHost interface {
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
}

type ConsoleAttached interface{ AttachConsole(ConsoleHost) }

type Listener interface {
	Interested(names []string, e Event) bool
	Listen(c Caller, e Event, names []string, now time.Time) *kernel.Error
}

// Processes is the flow app: it takes the flows every app
// declares, pins the versions instances took for replay, and hears an agent
// step's run end.
type Observer interface {
	Observe(e Event, events []string)
}

// AcceptedObserver freezes an event projection and stages its notifications.
// Applying it is a pure projection update, not a call to today's Observe code.
type AcceptedObserver interface {
	Observer
	PlanObserved(c Caller, e Event, events []string) (json.RawMessage, error)
	ValidateObserved(json.RawMessage) error
	ApplyObserved(json.RawMessage) error
}

// Linker serves Caller.Link and Caller.Links for every app.
type Linker interface {
	Links(c Caller, entity string) []string
	Link(c Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error
}

// Directory answers who belongs where in the organisation, for
// Caller.Units, approvals along the organisation and notifications to a unit's role.

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

type AuthzDecision = authz.Verdict
type AuthzLayer = authz.Layer

// Filtered explains a narrowing result without naming or counting hidden rows.
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

// AuthorizationRecorder belongs to the same private accepted decision as Caller.
// Capability planners use it to retain a fully resolved owner decision.
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

// Languages are an app's translations: for each language
// ("zh-CN"), the English text of its titles and descriptions mapped to their
// translation. English is the source and the fallback, so an app is
// translated one text at a time without renaming anything.
type Languages map[string]map[string]string

// LoadLanguages reads the dictionaries <dir>/<language>.json of an app, usually
// embedded with it (//go:embed i18n). A file that does not parse panics: it is
// code, and every composition's tests load it.
func LoadLanguages(fsys fs.FS, dir string) Languages {
	out := Languages{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic("languages: " + err.Error())
	}
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			panic("languages: " + err.Error())
		}
		dict := map[string]string{}
		if err := json.Unmarshal(raw, &dict); err != nil {
			panic("languages: " + e.Name() + ": " + err.Error())
		}
		out[lang] = dict
	}
	return out
}

// CheckLanguages bounds application-owned declaration dictionaries without
// changing their source texts or translations.
func (a Application) CheckLanguages() error {
	if len(a.Languages) > 32 {
		return fmt.Errorf("application translations exceed 32 languages")
	}
	entries := 0
	for lang, words := range a.Languages {
		if lang == "" || len(lang) > 35 || strings.ContainsAny(lang, "/\\") || strings.ContainsFunc(lang, unicode.IsSpace) {
			return fmt.Errorf("application translations need a language tag")
		}
		entries += len(words)
		for source, translation := range words {
			if strings.TrimSpace(source) == "" || strings.TrimSpace(translation) == "" || len(source) > 4096 || len(translation) > 4096 {
				return fmt.Errorf("application translations need nonempty texts of at most 4096 bytes")
			}
		}
	}
	if entries > 10000 {
		return fmt.Errorf("application translations exceed 10000 entries")
	}
	return nil
}

// Resources owns only live registrations and handles. Durable business writes
// and external emissions never enter this cleanup stack.
type Resources struct {
	mu        sync.Mutex
	closing   bool
	users     int
	idle      chan struct{}
	cleanups  []resourceCleanup
	closeGate chan struct{}
	failure   string
}

type ResourceState struct {
	State    string `json:"state"`
	InFlight int    `json:"inFlight"`
	Pending  int    `json:"pending"`
	Error    string `json:"error,omitempty"`
}

func (s *Resources) State() ResourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := ResourceState{State: "ready", InFlight: s.users, Pending: len(s.cleanups), Error: s.failure}
	if s.closing {
		state.State = "draining"
		if s.users == 0 && len(s.cleanups) == 0 {
			state.State = "closed"
		}
	}
	if s.failure != "" {
		state.State = "cleanup-failed"
	}
	return state
}

// ResourceLease lets an acquisition already in flight register its inverse
// during draining. Closing waits for Release before consuming the stack.
type ResourceLease struct {
	scope    *Resources
	mu       sync.Mutex
	released bool
}

func (s *Resources) Begin() (*ResourceLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, fmt.Errorf("resource scope is draining")
	}
	if s.users == 0 {
		s.idle = make(chan struct{})
	}
	s.users++
	return &ResourceLease{scope: s}, nil
}

func (l *ResourceLease) Track(name string, undo func(context.Context) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released || undo == nil {
		return fmt.Errorf("resource acquisition has ended")
	}
	l.scope.mu.Lock()
	defer l.scope.mu.Unlock()
	l.scope.cleanups = append(l.scope.cleanups, resourceCleanup{name, undo})
	return nil
}

func (l *ResourceLease) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return
	}
	l.released = true
	l.scope.mu.Lock()
	defer l.scope.mu.Unlock()
	l.scope.users--
	if l.scope.users == 0 {
		close(l.scope.idle)
	}
}

type resourceCleanup struct {
	name string
	undo func(context.Context) error
}

// Acquire pins an active resource scope until its returned release is called.
func (s *Resources) Acquire() (func(), error) {
	l, err := s.Begin()
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// Track registers the inverse at acquisition time. It never snapshots shared state.
func (s *Resources) Track(name string, undo func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || undo == nil {
		return fmt.Errorf("cannot register resource %s on an inactive scope", name)
	}
	s.cleanups = append(s.cleanups, resourceCleanup{name, undo})
	return nil
}

// Close stops new acquisitions, drains users, and waits for inverses in reverse
// acquisition order. A failed inverse and its dependencies remain for retry.
func (s *Resources) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	if s.closeGate == nil {
		s.closeGate = make(chan struct{}, 1)
	}
	gate := s.closeGate
	users, idle := s.users, s.idle
	s.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if users != 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if len(s.cleanups) == 0 {
			s.mu.Unlock()
			return nil
		}
		last := s.cleanups[len(s.cleanups)-1]
		s.mu.Unlock()
		if err := last.undo(ctx); err != nil {
			failure := errors.Join(fmt.Errorf("resource %s did not close", last.name), err)
			s.mu.Lock()
			s.failure = failure.Error()
			s.mu.Unlock()
			return failure
		}
		s.mu.Lock()
		s.cleanups = s.cleanups[:len(s.cleanups)-1]
		s.failure = ""
		s.mu.Unlock()
	}
}

func CallRead(app App, c Caller, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	if app == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	// Named reads are optional: an app implementing neither reader answers
	// NOT_FOUND without building a manifest it may not be able to build.
	parameterized, hasParameterized := app.(ParameterizedReader)
	_, hasPlain := app.(Reader)
	if !hasParameterized && !hasPlain {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if fields, declared := app.Manifest().ReadInputs[name]; declared {
		if reader, ok := parameterized, hasParameterized; ok {
			raw := json.RawMessage(`{}`)
			if len(inputs) > 0 && len(inputs[0]) > 0 {
				raw = inputs[0]
			}
			var values map[string]any
			if json.Unmarshal(raw, &values) != nil || values == nil {
				return nil, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Read inputs must be a JSON object")
			}
			if issues := InputIssues(fields, values); len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			return reader.ReadInput(c, name, raw)
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if reader, ok := app.(Reader); ok {
		return reader.Read(c, name)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

// CustomRole is a role the tenant defines in an app: the actions it may call.
type CustomRole struct {
	ID      string   `json:"id"`
	App     string   `json:"app"`
	Title   string   `json:"title"`
	Actions []string `json:"actions"`
}

// PolicyRecord is a tenant rule: deny or allow a permission (exact, or a
// prefix ending in *) where the request's attributes match, within days.
type PolicyRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Effect     string            `json:"effect"` // deny, allow
	Permission string            `json:"permission"`
	Where      map[string]string `json:"where,omitempty"` // member, app, agent ("true"), target (entity type)
	From       string            `json:"from,omitempty"`
	Until      string            `json:"until,omitempty"`
}

// Team is a named set of members who hold grants together.
type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Grants  []Grant  `json:"grants"`
}

// AccessConfig is what GET /v1/access answers.
type AccessConfig struct {
	Roles    []CustomRole   `json:"roles"`
	Policies []PolicyRecord `json:"policies"`
	Teams    []Team         `json:"teams"`
}

// ProjectEditor is one member's part in a project.
type ProjectMember struct {
	Member string `json:"member" field:"required" title:"Member" help:"A member's ID in this tenant"`
	Role   string `json:"role,omitempty" field:"required,choice:editor" title:"Part" help:"editor: may edit the assets this project names"`
}

// ProjectAsset is one asset a project covers.
type ProjectAsset struct {
	App  string `json:"app,omitempty" title:"App" help:"The app the asset belongs to; empty: any"`
	Kind string `json:"kind" field:"required,choice:object,page,query,function,process,code,propertytype,linktype" title:"Kind"`
	Name string `json:"name" field:"required" title:"Name" help:"The asset's name inside its owner app"`
}

// Project is a tenant's project: a named association of members with asset
// edit delegation. It is organisational, tenant-level, and never a second
// security boundary around records.
type BuildProject struct {
	Record
	Name    string          `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"spring-rollout"`
	Title   string          `json:"title" field:"required,search" title:"What people call it" example:"Spring rollout"`
	Members []ProjectMember `json:"members,omitempty" title:"Members"`
	Assets  []ProjectAsset  `json:"assets,omitempty" title:"Assets"`
}

type Profile struct {
	Member      string `json:"member"`
	DisplayName string `json:"displayName,omitempty"`
	GivenName   string `json:"givenName,omitempty"`
	FamilyName  string `json:"familyName,omitempty"`
	Title       string `json:"title,omitempty"` // job title
	Pronouns    string `json:"pronouns,omitempty"`
	Email       string `json:"email,omitempty"` // where notifications are mailed; empty: the user: subject's address
	Phone       string `json:"phone,omitempty"`
	// Localisation; empty inherits the tenant's.
	Language     string `json:"language,omitempty"`     // such as zh-CN
	Timezone     string `json:"timezone,omitempty"`     // IANA, such as Asia/Shanghai
	DateFormat   string `json:"dateFormat,omitempty"`   // ymd | dmy | mdy
	NumberFormat string `json:"numberFormat,omitempty"` // "1,234.56" | "1.234,56" | "1 234,56"
	WeekStart    string `json:"weekStart,omitempty"`    // monday | sunday | saturday
	// Notifications.
	InApp     *bool  `json:"inApp,omitempty"`
	Mail      *bool  `json:"mail,omitempty"`
	Digest    string `json:"digest,omitempty"`    // instant | hourly | daily
	QuietFrom string `json:"quietFrom,omitempty"` // HH:MM in the member's timezone
	QuietTo   string `json:"quietTo,omitempty"`
	// Workspace.
	HomePage string `json:"homePage,omitempty"` // an app ID or app/view the workspace opens on
	Theme    string `json:"theme,omitempty"`    // system | light | dark
	Density  string `json:"density,omitempty"`  // comfortable | compact
	// Read-only, the host's memory (not part of the decided state): when the
	// member last acted on this host since it started.
	LastSeen time.Time `json:"lastSeen,omitzero"`
}

// Account is the effective profile a member works under: their own choices
// over the tenant's defaults, with the defaults named.
type Account struct {
	Profile
	Effective struct {
		Language     string `json:"language"`
		Timezone     string `json:"timezone"`
		DateFormat   string `json:"dateFormat"`
		NumberFormat string `json:"numberFormat"`
		WeekStart    string `json:"weekStart"`
		Email        string `json:"email"`
		Digest       string `json:"digest"`
	} `json:"effective"`
	// Subjects are the identities that sign in as this member:
	// bound by the administrator who added or invited them, sorted.
	Subjects []string `json:"subjects"`
}

// TenantRecord is the tenant as its members see it (read "tenant", everyone):
// its settings, the apps it runs, and its standing on the host.
type TenantRecord struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
	Apps     []string          `json:"apps"`
	Members  int               `json:"members"`
	Status   string            `json:"status"` // active | suspended | frozen (host lifecycle)
	Since    time.Time         `json:"since,omitempty"`
}

// Reach is how a member wants to be told: in the workspace,
// by mail at which address and in which language, and when mail may go out.
type Reach struct {
	InApp, Mail bool
	Address     string
	Language    string
	Digest      string // instant | hourly | daily
	QuietFrom   string // HH:MM in Location; "" for none
	QuietTo     string
	Location    *time.Location
}

// Due is when a notice raised at now may be mailed: at once for an instant
// digest outside quiet hours; else the next hour or the next morning, pushed
// past the quiet window. The effect queue sends it then; nothing is lost.
func (p Reach) Due(now time.Time) time.Time {
	loc := p.Location
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	due := local
	switch p.Digest {
	case "hourly":
		due = local.Truncate(time.Hour).Add(time.Hour)
	case "daily":
		due = time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, loc)
		if !due.After(local) {
			due = due.AddDate(0, 0, 1)
		}
	}
	if p.QuietFrom != "" && p.QuietTo != "" {
		from, errFrom := time.Parse("15:04", p.QuietFrom)
		to, errTo := time.Parse("15:04", p.QuietTo)
		if errFrom == nil && errTo == nil {
			at := func(t time.Time, h time.Time) time.Time {
				return time.Date(t.Year(), t.Month(), t.Day(), h.Hour(), h.Minute(), 0, 0, loc)
			}
			start, end := at(due, from), at(due, to)
			if !end.After(start) { // over midnight: 22:00–08:00
				if due.Before(end) {
					due = end
				} else if !due.Before(start) {
					due = end.AddDate(0, 0, 1)
				}
			} else if !due.Before(start) && due.Before(end) {
				due = end
			}
		}
	}
	return due.UTC()
}

// TokenView is a token as its owner sees it.
type TokenView struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Scopes   []string  `json:"scopes"`
	Until    string    `json:"until,omitempty"`
	Issued   time.Time `json:"issued"`
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Expired  bool      `json:"expired"`
}

// Session is the host's memory of a credential: when first and last seen,
// from what, and whether it is the one asking.
type Session struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // sign-in, token
	Token   string    `json:"token,omitempty"`
	Agent   string    `json:"agent,omitempty"` // user agent summary
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Current bool      `json:"current"`
	// Ended is when the host stopped honouring the credential: ended by the
	// member, by suspension, or by the tenant's session hours. An ended
	// session stays listed for a day so the list shows what just happened
	// rather than silently shrinking.
	Ended time.Time `json:"ended,omitzero"`
}

// Seat is a directory entry as configured: the subjects that sign in as the member.
type Seat struct {
	Subjects []string `json:"subjects"` // "user:<email>", "client:<id>", or a development token's subject
	Member
	Units []Membership `json:"units,omitempty"` // the member's starting memberships; Party is filled in
}

// Identity is a development identity: the token that signs in as a member.
type LoginIdentity struct {
	Token  string            `json:"token"`
	Tenant string            `json:"tenant"`
	Member string            `json:"member"`
	Roles  map[string]string `json:"roles"`
}

// MemberView is a member with the subjects that sign in as it and its profile.
type MemberView struct {
	Member
	Subjects []string `json:"subjects"`
	Profile  Profile  `json:"profile"`
}

// IdentitySessions is the authentication consumer of governance-owned state.
type IdentitySessions interface {
	App
	MemberBySubject(string) (Member, bool)
	MemberByToken(string, time.Time) (Member, bool)
	NoticeSession(string, string, string, string, time.Time) bool
	SeenMember(string, time.Time)
	JoinsSubject(string) bool
	JoinableSubject(string) bool
	JoinID(string) string
	SecondFactorRequired() bool
	Minted(string, string, time.Time) (string, bool)
	EndOtherSessions(string, string, time.Time) int
	Sessions(string, string, time.Time) []Session
	Tokens(string, time.Time) []TokenView
}

// IdentityProfile exposes current profile projections without directory state.
type IdentityProfile interface {
	App
	Account(string) Account
	TenantRecord() TenantRecord
	Location(string) *time.Location
	Language(string) string
	Reach(string) Reach
}

type HostIdentities interface {
	App
	Identities() []LoginIdentity
}

// Recovery and package composition are separate from sign-in consumers.
type IdentityRecovery interface{ CloseTokenSecretWindows() }

type IdentityCounts interface{ MemberCount() int }

type ProjectAccess interface {
	EditsProjectAsset(string, ProjectAsset) bool
}

type AuthorizationState struct {
	Roles    map[string]*CustomRole
	Policies map[string]*PolicyRecord
	Teams    map[string]*Team
	Projects map[string]*BuildProject
}

func AccessActions() []Action {
	admin := []string{"admin"}
	return []Action{
		{Schema: SchemaRoleSave, Target: RoleType, Capability: "members", Title: "Define role",
			Description: "Define or change a role of this tenant in an app: the actions it may call. Members hold it like any role.",
			Payload: []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "title", Type: "string"},
				{Name: "actions", Type: "json", Required: true, Description: "Action schemas of the app"}}, Roles: admin},
		{Schema: SchemaRoleRemove, Target: RoleType, Capability: "members", Title: "Remove role",
			Description: "Remove a role this tenant defined; grants of it no longer allow anything.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaPolicySave, Target: PolicyType, Capability: "members", Title: "Set policy",
			Description: "Deny or allow a permission under conditions; a deny wins over every role.",
			Payload: []Field{{Name: "title", Type: "string"}, {Name: "effect", Type: "string", Required: true, Choices: []string{"deny", "allow"}},
				{Name: "permission", Type: "string", Required: true, Description: "An action schema, or a prefix ending in *"},
				{Name: "where", Type: "json", Description: "Conditions: member, app, agent, target"},
				{Name: "from", Type: "string", Description: "In force from this day (YYYY-MM-DD)"}, {Name: "until", Type: "string", Description: "Until this day, exclusive"}}, Roles: admin},
		{Schema: SchemaPolicyDrop, Target: PolicyType, Capability: "members", Title: "Remove policy", Description: "Remove a policy.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaTeamSave, Target: TeamType, Capability: "members", Title: "Save team",
			Description: "A team: members who hold the team's grants together, for as long as they belong to it.",
			Payload: []Field{{Name: "name", Type: "string", Required: true}, {Name: "members", Type: "json", Required: true, Description: "Member IDs"},
				{Name: "grants", Type: "json", Description: "Grants every member holds: app, role, unit, structure, until"}}, Roles: admin},
		{Schema: SchemaTeamRemove, Target: TeamType, Capability: "members", Title: "Remove team", Description: "Remove a team; its grants end.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaDelegate, Target: "platform.member", Capability: "members", Title: "Delegate my roles",
			Description: "Give another member the roles you hold in an app, until a day: for an absence or a handover. Only what you hold yourself.",
			Payload: []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "until", Type: "string", Required: true, Description: "Until this day, exclusive (YYYY-MM-DD)"},
				{Name: "reason", Type: "string"}}, Roles: []string{AnyMember}},
	}
}

// ProjectActions are the decisions an administrator makes about projects.
func ProjectActions() []Action {
	admin := []string{"admin"}
	return []Action{
		{Schema: SchemaProjectSave, Target: ProjectType, Capability: "projects", Title: "Save project",
			Description: "Create or change a project and the members and assets it covers.",
			Payload: []Field{
				{Name: "name", Type: "string", Required: true},
				{Name: "title", Type: "string", Required: true},
				{Name: "members", Type: "json", Description: "member IDs with their part in the project"},
				{Name: "assets", Type: "json", Description: "app, kind and name of every asset the project covers"},
			}, Roles: admin},
		{Schema: SchemaProjectArchive, Target: ProjectType, Capability: "projects", Title: "Archive project",
			Description: "Archive a project: its members stop editing its assets; history stays.", Payload: []Field{}, Roles: admin},
	}
}

const (
	RoleType         = "platform.role"
	SchemaRoleSave   = "platform.role.save"
	SchemaRoleRemove = "platform.role.remove"
	PolicyType       = "platform.policy"
	SchemaPolicySave = "platform.policy.save"
	SchemaPolicyDrop = "platform.policy.remove"
	TeamType         = "platform.team"
	SchemaTeamSave   = "platform.team.save"
	SchemaTeamRemove = "platform.team.remove"
	SchemaDelegate   = "platform.member.delegate"
)
const Auditor = "auditor"
const (
	ProjectType          = "platform.project"
	SchemaProjectSave    = ProjectType + ".save"
	SchemaProjectArchive = ProjectType + ".archive"
	// ProjectEditorRole is the part a member holds inside a project.
	ProjectEditorRole = "editor"
)

// Auditor read surface: the administrator reads an auditor may use.
var auditorReads = []string{"audit", "deliveries", "work", "connectors", "settings",
	"endpoints", "effects", "health", "personal-reads", "permissions", "access", "members", "projects", "packages", "notifications"}

func AuditorMayRead(name string) bool {
	for _, r := range auditorReads {
		if r == name {
			return true
		}
	}
	return false
}

type ConsolePolicies interface {
	Attach(AppCatalog)
	SnapshotState() AuthorizationState
	Restore(AuthorizationState)
	PrepareAccess(Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *kernel.Error)
	PrepareProject(Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *kernel.Error)
	TeamGrants(string) []Grant
	Access() AccessConfig
	ProjectViews() []BuildProject
	EditsProjectAsset(string, ProjectAsset) bool
	ProjectGrants(string, bool) []Grant
	ApplyAccess()
}
