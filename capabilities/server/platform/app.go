package platform

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Member struct {
	authorization     AuthorizationEngine
	AIContext, AskOK  bool              `json:"-"`
	AICaller          json.RawMessage   `json:"aiCaller,omitempty"`
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

func (m Member) RolesIn(scope string) []string {
	apps := []string{scope}
	if space, _, typed := strings.Cut(scope, "."); typed {
		apps = slices.Compact([]string{OwnerOfType(scope), space})
	}
	var out []string
	for i, app := range apps {
		held := len(out)
		roles := []string{m.Roles[app]}
		for _, g := range m.Grants {
			if g.App == app {
				roles = append(roles, g.Role)
			}
		}
		for _, role := range roles {
			if role != "" && !slices.Contains(out, role) {
				out = append(out, role)
			}
		}
		if i == 1 && len(out) > held && !slices.Contains(out, "user") {
			out = append(out, "user")
		}
	}
	return out
}

func (m Member) RolesFor(app, name string) []string {
	if strings.Contains(name, ".") && !strings.ContainsAny(name, " /") && OwnerOfType(name) == app {
		return m.RolesIn(name)
	}
	return m.RolesIn(app)
}

func (m Member) Holds(app, role string) bool { return slices.Contains(m.RolesIn(app), role) }

func (m Member) May(app, permission string, roles []string) AuthzDecision {
	return m.authorization.Decide(AuthorizationInput{Subject: AuthorizationSubject{ID: m.ID, App: app, Roles: m.RolesFor(app, permission), Agent: m.Agent}, Permission: permission, Allowed: roles})
}

func (m Member) AuthorizationEngine() AuthorizationEngine { return m.authorization }

func (m Member) WithAuthorization(engine AuthorizationEngine) Member {
	m.authorization = engine
	return m
}

func (m Member) Active() bool { return m.Status != MemberSuspended && m.Status != MemberLeft }
func (m Member) IsAI() bool   { return m.Agent || len(m.AICaller) > 0 }

func (m Member) InScope(permission string) bool {
	return m.Scopes == nil || slices.ContainsFunc(m.Scopes, func(p string) bool {
		return p == permission || strings.HasSuffix(p, "*") && strings.HasPrefix(permission, strings.TrimSuffix(p, "*"))
	})
}

const MemberInvited, MemberSuspended, MemberLeft = "invited", "suspended", "left"

func (m Member) Location() *time.Location {
	if loc, err := time.LoadLocation(m.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

func (m Member) Today(now time.Time) string { return now.In(m.Location()).Format(time.DateOnly) }

type Caller struct {
	Member
	App                                                    string
	Replaying, Automation, sourceProjection, restoreSource bool
	inputRefused                                           func(*Error, []FieldIssue)
	rt                                                     Runtime
	creationDepth                                          int
	transactionInput                                       *pb.Submission
	agentRun                                               string
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

func (c Caller) RolesHere() []string { return c.RolesIn(c.App) }

type StagedDecision interface {
	HoldChanges(Authority) (release func(), refusal *Error)
	DraftChanges(Authority, func() ChangeLog) ChangeLog
	DecisionCatalog(Authority) *Catalog
	FailDecision(*Error)
	RecordAuthorization(Caller, AuthzRequest, AuthzDecision)
	BusinessRefused(*Error, BusinessRefusalFact)
	CaptureBusinessRefusal(Caller, *pb.ChangeRecord, BusinessRefusalFact) *Error
	RefuseBusinessCondition(Caller, string, string, any, map[string]any, time.Time) *Error
	PendingRefusalEscalations(Caller, time.Time) ([]RefusalEscalationRequest, *Error)
	PrepareBusinessVersions(Caller, any, time.Time) *Error
	ConfirmationTransport() ConfirmationTransport
	ConfirmationCompletion(Caller, string, string, string) bool
	DispatchTransport() DispatchTransport
	ReadDecisionRecordSet(Caller, string, string, string, json.RawMessage, time.Time) (RecordSetResult, *Error)
	ObservationContext(Caller) *ObservationContext
	TxnTransport() TxnTransport
	CaptureTxnIteration(Caller, *pb.ChangeRecord, TxnIteration) *Error
	PutOwnedRelation(Caller, *pb.ChangeRecord, TxnLink, bool, time.Time) *Error
	MigrateOwnedRelations(Caller, *pb.ChangeRecord, AssetBinding, time.Time) *Error
	Probing() bool
}

func (c Caller) StagedDecision() StagedDecision {
	if c.rt == nil {
		return nil
	}
	return c.rt.StagedDecision()
}

func (c Caller) Staging() bool { return c.StagedDecision() != nil }

func (c Caller) Probing() bool {
	decision := c.StagedDecision()
	return decision != nil && decision.Probing()
}

func (c Caller) ReadScoped() bool { return c.rt != nil && c.rt.ReadScoped() }

func (c Caller) FailDecision(err *Error) {
	if err == nil {
		return
	}
	decision := c.StagedDecision()
	if decision == nil || c.Replaying {
		panic("decision failure requires private staging")
	}
	decision.FailDecision(err)
}

type AcceptedPublisher interface {
	AcceptedPublicationSchemas() []string
	ValidateAcceptedPublication(schema string, image []byte) error
	ApplyAcceptedPublication(schema string, image []byte) error
}

type AcceptedStateApp interface {
	Service
	AcceptedState() (json.RawMessage, error)
	ValidateAcceptedState(json.RawMessage) error
	ApplyAcceptedState(json.RawMessage) error
}

type AcceptedInputApp interface {
	Service
	InputReceiver
	AcceptedInputs() []string
	DecodeAcceptedInput(json.RawMessage) (any, error)
}

func NewCaller(rt Runtime, m Member, app string, replaying, automation bool) Caller {
	if rt != nil {
		m = m.WithAuthorization(rt.AuthorizationEngine())
	}
	return Caller{Member: m, App: app, Replaying: replaying, Automation: automation, rt: rt}
}

func RouteCaller(c Caller, app string) Caller { c.App = app; return c }

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

func Attempt(c Caller, decide func() (*pb.ChangeRecord, *Error)) (*pb.ChangeRecord, *Error) {
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

func ProbeDecision(c Caller, decide func(Caller) *Error) *Error {
	if c.rt == nil {
		return &Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return c.rt.ProbeDecision(c, decide)
}

func Decide(c Caller, app Service, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *Error) {
	if action, ok := app.Manifest().Actions.Action(s.GetSchema().GetName()); ok && action.Ask && !c.Automation && c.IsAI() && !c.AskOK {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI callers must obtain inbox approval before running this action")
	}
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

func ControlDecision(c Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *Error) {
	if c.rt == nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Host controls require a private decision")
	}
	return c.rt.ControlDecision(c, s, at)
}

type Manifest struct {
	ID, Title, Version string
	Actions            *Catalog
	Reads              []string
	ReadInputs         map[string][]Field
	ReadSources        map[string]string
	Inputs             map[string]bool
	Subscribes         []string
	Provides           []Provision
	Consumes           []Consumption
	Everyone           []string
	Jobs               []Job
	Retry              *Retry
	Settings           []Setting
	Emits              []EffectKind
	Entities           []RecordType
	Pages              []Page
	LinkTypes          []LinkType
	PropertyTypes      []PropertyType
	ValueTypes         []ValueType
	Interfaces         []Interface
	Queries            []NamedQuery
	Functions          []AIFunction
	Operations         []Operation
	Observations       []ObservationDefinition
	Flows              []Flow
	Agents             []Agent
	Sequences          []Sequence
	Roles              []string
	Languages          Languages
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

type Service interface {
	Authority() Authority
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *Error)
	Fork(authority Authority) (Service, error)
	Snapshot() (json.RawMessage, error)
	Restore(json.RawMessage) error
	SnapshotFormat() string
}

type Reader interface {
	Read(c Caller, name string) (any, *Error)
}

type InputReceiver interface {
	Input(c Caller, name string, body []byte, now time.Time) (any, *Error)
}

type Event struct {
	App     string
	Record  *pb.ChangeRecord
	Changed []string
}

type Subscriber interface {
	Handle(c Caller, e Event) *Error
}

type Runner interface {
	Run(c Caller, job string, now time.Time) *Error
}

type Answerer interface {
	Answer(c Caller, e Effect, o Outcome, now time.Time) *Error
}

func notFound() *Error { return &Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }

var slot = regexp.MustCompile(`\{\w+\}`)

func Refuse(code pb.ErrorCode, text string, values ...any) *Error {
	i := 0
	message := slot.ReplaceAllStringFunc(text, func(p string) string {
		if i >= len(values) {
			return p
		}
		i++
		return fmt.Sprint(values[i-1])
	})
	return &Error{Code: code, Message: message}
}

const (
	actionOutsideAcceptedBoundary = "This action uses an effect outside the accepted-result boundary"
	inputOutsideAcceptedBoundary  = "This input uses an effect outside the accepted-result boundary"
)

func RefuseOutsideAcceptedBoundary() *Error {
	return &Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: actionOutsideAcceptedBoundary}
}

func RefuseInputOutsideAcceptedBoundary() *Error {
	return &Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: inputOutsideAcceptedBoundary}
}

func IsOutsideAcceptedBoundary(err *Error) bool {
	return err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT &&
		(err.Message == actionOutsideAcceptedBoundary || err.Message == inputOutsideAcceptedBoundary)
}

type CompositionReady interface {
	Ready() error
}

type IdentityDirectory interface {
	Service
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
	Apps() []Service
	App(id string) Service
	AppIDs() []string
	Manifest(app string) Manifest
}

type Listener interface {
	Interested(names []string, e Event) bool
	Listen(c Caller, e Event, names []string, now time.Time) *Error
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
	Link(c Caller, from, to *pb.EntityRef, key string, now time.Time) *Error
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

func (c Caller) Authorize(request AuthzRequest, at time.Time) AuthzDecision {
	if c.rt != nil {
		return c.rt.Authorize(c, request, at)
	}
	return AuthzDecision{Rule: "none", Reason: "Authorization requires the owner runtime", Layers: []AuthzLayer{{Name: "principal", Reason: "Authorization requires the owner runtime"}}}
}
