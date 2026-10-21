// Package platform is what an app sees of the platform (#104 G1): its manifest
// and declarations, the caller of each input with what the host does for it,
// and the ledger that receives its decisions. The host runtime (package
// platformserver: tenants, hosts, the journal, deployment) implements Runtime;
// an app imports this package only, so it cannot reach the runtime.
package platform

import (
	"encoding/json"
	"fmt"
	"platformserver/platform/authz"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Member is a person, service or AI agent of a tenant. Grants are
// what they hold: roles in apps, each optionally bounded to a
// unit and a time. Roles is derived from the active grants by the host — the
// primary role per app (the app's first declared role the member holds) —
// for the many places that ask for one; RolesIn answers with all of them.
// What a rule scopes by is the member's organisation.
type Member struct {
	AIContext bool              `json:"-"` // trusted read narrowing, never a grant or request field
	ID        string            `json:"id"`
	Tenant    string            `json:"tenant"`
	Roles     map[string]string `json:"roles"`
	Grants    []Grant           `json:"grants,omitempty"`
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
	return authz.Decide(authz.Request{Subject: authz.Subject{ID: m.ID, App: app, Roles: m.RolesIn(app), Agent: m.Agent}, Permission: permission, Allowed: roles})
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
	App          string
	Replaying    bool
	Automation   bool
	inputRefused func(*kernel.Error, []FieldIssue)
	rt           Runtime
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

// NewCaller is how the host runtime hands an app its caller.
func NewCaller(rt Runtime, m Member, app string, replaying, automation bool) Caller {
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
	DraftChanges(*Ledger, func() *kernel.ChangeLog) *kernel.ChangeLog
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
