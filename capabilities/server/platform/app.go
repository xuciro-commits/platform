package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	"google.golang.org/protobuf/proto"
)

type Member struct {
	authorization authz.Engine
	AIContext     bool              `json:"-"`
	ID            string            `json:"id"`
	Tenant        string            `json:"tenant"`
	Roles         map[string]string `json:"roles"`
	Grants        []Grant           `json:"grants,omitempty"`
	Status        string            `json:"status,omitempty"`
	Scopes        []string          `json:"scopes,omitempty"`
	Agent         bool              `json:"agent,omitempty"`
	Language      string            `json:"language,omitempty"`
	Timezone      string            `json:"timezone,omitempty"`
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
	inputRefused     func(*kernel.Error, []FieldIssue)
	rt               Runtime
	creationDepth    int
	transactionInput *pb.Submission
	agentRun         string
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

func As(app string, m Member) Caller { return Caller{Member: m, App: app} }

type AuthorizationEngineRuntime interface{ AuthorizationEngine() authz.Engine }

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

type AuthzDecision = authz.Verdict
type AuthzLayer = authz.Layer

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

type Languages map[string]map[string]string

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

type Resources struct {
	mu        sync.Mutex
	closing   bool
	cleanups  []resourceCleanup
	closeGate chan struct{}
}

type resourceCleanup struct {
	name string
	undo func(context.Context) error
}

func (s *Resources) Track(name string, undo func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || undo == nil {
		return fmt.Errorf("cannot register resource %s on an inactive scope", name)
	}
	s.cleanups = append(s.cleanups, resourceCleanup{name, undo})
	return nil
}

func (s *Resources) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	if s.closeGate == nil {
		s.closeGate = make(chan struct{}, 1)
	}
	gate := s.closeGate
	s.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
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
			return errors.Join(fmt.Errorf("resource %s did not close", last.name), err)
		}
		s.mu.Lock()
		s.cleanups = s.cleanups[:len(s.cleanups)-1]
		s.mu.Unlock()
	}
}

func CallRead(app App, c Caller, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	if app == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
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

type CustomRole struct {
	ID      string   `json:"id"`
	App     string   `json:"app"`
	Title   string   `json:"title"`
	Actions []string `json:"actions"`
}

type PolicyRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Effect     string            `json:"effect"`
	Permission string            `json:"permission"`
	Where      map[string]string `json:"where,omitempty"`
	From       string            `json:"from,omitempty"`
	Until      string            `json:"until,omitempty"`
}

type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Grants  []Grant  `json:"grants"`
}

type AccessConfig struct {
	Roles    []CustomRole   `json:"roles"`
	Policies []PolicyRecord `json:"policies"`
	Teams    []Team         `json:"teams"`
}

type ProjectMember struct {
	Member string `json:"member" field:"required" title:"Member" help:"A member's ID in this tenant"`
	Role   string `json:"role,omitempty" field:"required,choice:editor" title:"Part" help:"editor: may edit the assets this project names"`
}

type ProjectAsset struct {
	App  string `json:"app,omitempty" title:"App" help:"The app the asset belongs to; empty: any"`
	Kind string `json:"kind" field:"required,choice:object,page,query,function,process,code,propertytype,linktype" title:"Kind"`
	Name string `json:"name" field:"required" title:"Name" help:"The asset's name inside its owner app"`
}

type BuildProject struct {
	Record
	Name    string          `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"spring-rollout"`
	Title   string          `json:"title" field:"required,search" title:"What people call it" example:"Spring rollout"`
	Members []ProjectMember `json:"members,omitempty" title:"Members"`
	Assets  []ProjectAsset  `json:"assets,omitempty" title:"Assets"`
}

type Profile struct {
	Member       string    `json:"member"`
	DisplayName  string    `json:"displayName,omitempty"`
	GivenName    string    `json:"givenName,omitempty"`
	FamilyName   string    `json:"familyName,omitempty"`
	Title        string    `json:"title,omitempty"`
	Pronouns     string    `json:"pronouns,omitempty"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	Language     string    `json:"language,omitempty"`
	Timezone     string    `json:"timezone,omitempty"`
	DateFormat   string    `json:"dateFormat,omitempty"`
	NumberFormat string    `json:"numberFormat,omitempty"`
	WeekStart    string    `json:"weekStart,omitempty"`
	InApp        *bool     `json:"inApp,omitempty"`
	Mail         *bool     `json:"mail,omitempty"`
	Digest       string    `json:"digest,omitempty"`
	QuietFrom    string    `json:"quietFrom,omitempty"`
	QuietTo      string    `json:"quietTo,omitempty"`
	HomePage     string    `json:"homePage,omitempty"`
	Theme        string    `json:"theme,omitempty"`
	Density      string    `json:"density,omitempty"`
	LastSeen     time.Time `json:"lastSeen,omitzero"`
}

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
	Subjects []string `json:"subjects"`
}

type TenantRecord struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
	Apps     []string          `json:"apps"`
	Members  int               `json:"members"`
	Status   string            `json:"status"`
	Since    time.Time         `json:"since,omitempty"`
}

type Reach struct {
	InApp, Mail bool
	Address     string
	Language    string
	Digest      string
	QuietFrom   string
	QuietTo     string
	Location    *time.Location
}

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
			if !end.After(start) {
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

type TokenView struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Scopes   []string  `json:"scopes"`
	Until    string    `json:"until,omitempty"`
	Issued   time.Time `json:"issued"`
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Expired  bool      `json:"expired"`
}

type Session struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Token   string    `json:"token,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Current bool      `json:"current"`
	Ended   time.Time `json:"ended,omitzero"`
}

type Seat struct {
	Subjects []string `json:"subjects"`
	Member
	Units []Membership `json:"units,omitempty"`
}

type LoginIdentity struct {
	Token  string            `json:"token"`
	Tenant string            `json:"tenant"`
	Member string            `json:"member"`
	Roles  map[string]string `json:"roles"`
}

type MemberView struct {
	Member
	Subjects []string `json:"subjects"`
	Profile  Profile  `json:"profile"`
}

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
	ProjectEditorRole    = "editor"
)

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

type SupportGrant struct {
	ID      string    `json:"id"`
	Tenant  string    `json:"tenant"`
	Member  string    `json:"member"`
	Reason  string    `json:"reason"`
	Opened  string    `json:"opened"`
	Expires time.Time `json:"expires"`
	Uses    int       `json:"uses"`
}

type MigrationType struct {
	Type    string      `json:"type"`
	Rows    int         `json:"rows"`
	Written int         `json:"written"`
	Refused []ImportRow `json:"refused,omitempty"`
}

type MigrationResult struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Key   string          `json:"key"`
	Types []MigrationType `json:"types"`
}

type MigrationManifest struct {
	At     time.Time       `json:"at"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Key    string          `json:"key"`
	Member string          `json:"member"`
	Types  []MigrationType `json:"types"`
}

type ImportRow struct {
	Row     int        `json:"row"`
	ID      string     `json:"id"`
	Action  string     `json:"action"`
	Outcome string     `json:"outcome"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Message string       `json:"message,omitempty"`
	Issues  []FieldIssue `json:"issues,omitempty"`
	Code    string       `json:"code"`
	Status  int          `json:"status,omitempty"`
	Detail  string       `json:"detail,omitempty"`
}

type HostControlImage struct {
	Lifecycle        string                        `json:"lifecycle"`
	Support          []SupportGrant                `json:"support"`
	Migrations       []MigrationManifest           `json:"migrations"`
	Requests         map[string]HostControlReceipt `json:"requests"`
	RuntimeSelection map[string]string             `json:"runtimeSelection,omitempty"`
	DisabledOwners   []string                      `json:"disabledOwners,omitempty"`
}

type HostControlReceipt struct {
	RequestHash string       `json:"requestHash"`
	Answer      SupportGrant `json:"answer,omitempty"`
}

type TenantSpec struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Template string            `json:"template,omitempty"`
	Seats    []Seat            `json:"seats"`
	Settings map[string]string `json:"settings,omitempty"`
	Created  time.Time         `json:"created,omitempty"`
	By       string            `json:"by,omitempty"`
}

type TenantTemplate struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Settings    map[string]string `json:"settings,omitempty"`
	AdminRoles  map[string]string `json:"adminRoles,omitempty"`
}

type CreateTenantRequest struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Template string            `json:"template,omitempty"`
	Admin    string            `json:"admin"`
	Settings map[string]string `json:"settings,omitempty"`
}
