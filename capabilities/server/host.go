package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Member is a person, service or AI agent of a tenant (ADR-0010): one role per
// app, and attributes (lines, properties) apps may scope their rules by.
type Member struct {
	ID         string              `json:"id"`
	Tenant     string              `json:"tenant"`
	Roles      map[string]string   `json:"roles"`
	Attributes map[string][]string `json:"attributes,omitempty"`
}

// Caller is a member as one app sees it. Replaying marks the journal replay:
// who could act was decided when the input was accepted (ADR-0008). Automation
// marks an app acting on its own for a subscribed event, as member "app:<id>";
// its manifest's subscriptions and requirements are its grant.
type Caller struct {
	Member
	App        string
	Replaying  bool
	Automation bool
	tenant     *Tenant
}

// Role is the member's role in the app being called ("" for none).
func (c Caller) Role() string { return c.Roles[c.App] }

// As views member from app, without a host (tests of one app).
func As(app string, m Member) Caller { return Caller{Member: m, App: app} }

// Manifest declares an app (ADR-0010). Names of actions, reads and inputs are
// unique within a tenant; the host routes by them.
type Manifest struct {
	ID       string
	Version  string
	Actions  *Catalog
	Reads    []string
	Inputs   map[string]bool // connector inputs; true: recorded in the journal
	Requires []string        // apps whose actions or reads this app uses
	// Subscribes names actions (of itself or of required apps), or protocol
	// events it consumes ("<protocol id>#<event>"), delivered to Handle after commit.
	Subscribes []string
	Provides   []Provision   // protocols this app implements (ADR-0011)
	Consumes   []Consumption // protocols this app uses; the host binds a provider
	Everyone   []string      // reads any member may use; the app filters by caller
	Jobs       []Job         // scheduled work (Runner), ADR-0013
	Settings   []Setting     // typed values administrators set in Settings
}

// Event is an accepted decision, delivered to subscribers after commit.
type Event struct {
	App    string
	Record *pb.ChangeRecord
	hops   int // how many deliveries caused it
}

// Subscriber is an app that handles the events its manifest subscribes to, as
// owned work after the input (ADR-0013). A refusal is retried, then recorded as
// a failed delivery; the event's decision stands. Handle must depend only on
// tenant state: a replay runs it again and must reach the same outcome.
type Subscriber interface {
	Handle(c Caller, e Event) *kernel.Error
}

// Delivery is one event handed to one subscriber.
type Delivery struct {
	At         time.Time `json:"at"`
	App        string    `json:"app"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Subscriber string    `json:"subscriber"`
	Outcome    string    `json:"outcome"` // "ok" or an error code
	Attempt    int       `json:"attempt,omitempty"`
}

// App is one app's instance in one tenant.
type App interface {
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	// Read serves a named read; the app refuses callers it does not show it to.
	Read(c Caller, name string) (any, *kernel.Error)
	Input(c Caller, name string, body []byte, now time.Time) (any, *kernel.Error)
}

// Tenant runs a tenant's apps: routing by name, one ordered journal, and calls
// between apps only along declared requirements.
type Tenant struct {
	ID string
	// Record, when set, makes each accepted input durable before it is answered;
	// a failure must stop the server (ADR-0007).
	Record func(Entry)
	mu     sync.Mutex
	apps   []App
	owner  map[string]App // "action:", "read:" and "input:" names → app
	// audit holds accepted top-level inputs, newest last, rebuilt by replay; its
	// own lock, because reads run inside other apps' submissions.
	auditMu    sync.Mutex
	audit      []AuditEntry
	deliveries []Delivery
	events     []Event // published during the current input, queued after it
	bindings   map[string]binding
	relations  *Relations
	org        *Organization
	hops       int // of the event being handled, for the events it causes
	effects    int // decisions published and notifications given, ever
	works      *kernel.Works
	// opsMu guards what reads and the runner share: queues, connectors,
	// notifications and settings (operations.go). It is never held while t.mu is taken.
	opsMu       sync.Mutex
	queues      map[string][]*Task // subscriber → its deliveries, head first
	failed      []*Task
	jobs        []*Task
	connectors  *kernel.Connectors
	descriptors map[string]*pb.ConnectorDescriptor
	lastError   map[string]ConnectorError
	notices     []Notification
	noticeSeq   int
	settings    map[string]string // "<app>/<name>" → value
}

// AuditEntry is one accepted input: who, when, through which app, what.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"` // an action's schema, or "input:<name>"
	Target string    `json:"target,omitempty"`
}

const auditKept = 1000

// Audit is the tenant's recent accepted inputs, oldest first.
func (t *Tenant) Audit() []AuditEntry {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	return slices.Clone(t.audit)
}

func (t *Tenant) remember(e AuditEntry) {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	t.audit = append(t.audit, e)
	if len(t.audit) > auditKept {
		t.audit = t.audit[len(t.audit)-auditKept:]
	}
}

// NewTenant enables apps for a tenant; it refuses duplicate names and unmet requirements.
func NewTenant(id string, apps ...App) (*Tenant, error) {
	t := &Tenant{ID: id, apps: apps, owner: map[string]App{}, bindings: map[string]binding{}, works: kernel.NewWorks(), queues: map[string][]*Task{},
		connectors: kernel.NewConnectors(), descriptors: map[string]*pb.ConnectorDescriptor{}, lastError: map[string]ConnectorError{}, settings: map[string]string{}}
	claim := func(name string, a App) error {
		if other := t.owner[name]; other != nil {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", id, name, other.Manifest().ID, a.Manifest().ID)
		}
		t.owner[name] = a
		return nil
	}
	for i, a := range apps {
		m := a.Manifest()
		for _, r := range m.Requires {
			// Requirements point to apps enabled earlier, so the graph is acyclic by construction.
			if !slices.ContainsFunc(apps[:i], func(x App) bool { return x.Manifest().ID == r }) {
				return nil, fmt.Errorf("tenant %s: %s requires %s, which is not enabled before it", id, m.ID, r)
			}
		}
		if err := t.bind(i, a); err != nil {
			return nil, err
		}
		if r, ok := a.(*Relations); ok {
			t.relations = r
		}
		if o, ok := a.(*Organization); ok {
			t.org = o
		}
		for _, action := range m.Subscribes {
			if protocol, _, ok := strings.Cut(action, "#"); ok {
				if !slices.ContainsFunc(m.Consumes, func(c Consumption) bool { return c.Protocol == protocol }) {
					return nil, fmt.Errorf("tenant %s: %s subscribes to %s of a protocol it does not consume", id, m.ID, action)
				}
				continue
			}
			owner := t.owner["action:"+action]
			if _, own := m.Actions.Action(action); own {
				owner = a // its own action, claimed below
			}
			if owner == nil || owner != a && !slices.Contains(m.Requires, owner.Manifest().ID) {
				return nil, fmt.Errorf("tenant %s: %s subscribes to %s of an app it does not require", id, m.ID, action)
			}
			if _, ok := a.(Subscriber); !ok {
				return nil, fmt.Errorf("tenant %s: %s subscribes but has no Handle", id, m.ID)
			}
		}
		if _, ok := a.(Runner); len(m.Jobs) > 0 && !ok {
			return nil, fmt.Errorf("tenant %s: %s declares jobs but has no Run", id, m.ID)
		}
		for _, j := range m.Jobs {
			t.jobs = append(t.jobs, &Task{ID: "job:" + m.ID + "/" + j.Name, Kind: "job", App: m.ID, Title: j.Title, State: "scheduled", job: j})
		}
		var names []string
		for _, r := range m.Reads {
			names = append(names, "read:"+r)
		}
		for _, action := range m.Actions.actions {
			names = append(names, "action:"+action.Schema)
		}
		for input := range m.Inputs {
			names = append(names, "input:"+input)
		}
		for _, n := range names {
			if err := claim(n, a); err != nil {
				return nil, err
			}
		}
	}
	return t, nil
}

func (t *Tenant) app(id string) App {
	i := slices.IndexFunc(t.apps, func(a App) bool { return a.Manifest().ID == id })
	if i < 0 {
		return nil
	}
	return t.apps[i]
}

func (t *Tenant) caller(m Member, app App, replaying bool) Caller {
	return Caller{Member: m, App: app.Manifest().ID, Replaying: replaying, tenant: t}
}

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

// Submit routes a submission to the app declaring its action and records it when accepted.
func (t *Tenant) Submit(m Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return nil, unknown()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.enqueue(now)
	record, err := a.Submit(t.caller(m, a, false), s, now)
	if err == nil {
		t.remember(submitted(m.ID, a, s, now))
		body, _ := protojson.Marshal(s)
		t.record(a, "submission", m, body, now)
	}
	return record, err
}

// Input routes a connector input (push batch, poll page, heartbeat) to its app.
func (t *Tenant) Input(m Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	a := t.owner["input:"+name]
	if a == nil {
		return nil, unknown()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer t.enqueue(now)
	out, err := a.Input(t.caller(m, a, false), name, body, now)
	if err != nil {
		t.refused(m.ID, name, err, now)
	}
	if err == nil && a.Manifest().Inputs[name] {
		t.remember(AuditEntry{At: now, Member: m.ID, App: a.Manifest().ID, Action: "input:" + name})
		t.record(a, name, m, body, now)
	}
	return out, err
}

// Read serves a named read of the app that declares it, to members holding a
// role in that app or to everyone when the manifest says so; the app may refuse further. Reads an app makes of the apps
// it requires (Caller.Read) are the app's own and are not checked here.
func (t *Tenant) Read(m Member, name string) (any, *kernel.Error) {
	a := t.owner["read:"+name]
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if m.Roles[a.Manifest().ID] == "" && !slices.Contains(a.Manifest().Everyone, name) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return a.Read(t.caller(m, a, false), name)
}

// publish queues an accepted decision for its subscribers (called by Ledger).
func (t *Tenant) publish(e Event) {
	e.hops = t.hops
	t.events = append(t.events, e)
	t.effects++
}

// Deliveries is the tenant's recent event deliveries, oldest first.
func (t *Tenant) Deliveries() []Delivery {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	return slices.Clone(t.deliveries)
}

func (t *Tenant) record(a App, kind string, m Member, body []byte, now time.Time) {
	if t.Record == nil {
		return
	}
	member, _ := json.Marshal(m)
	t.Record(Entry{App: a.Manifest().ID, Kind: kind, Principal: member, Body: body, At: now})
}

// Replay feeds recorded inputs through the apps that first accepted them, as the
// members they came from; a refusal means the record and the code disagree.
func (t *Tenant) Replay(entries []Entry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, e := range entries {
		var m Member
		a := t.app(e.App)
		if a == nil || json.Unmarshal(e.Principal, &m) != nil {
			return fmt.Errorf("entry %d: app %q not enabled or member unreadable", i+1, e.App)
		}
		var err *kernel.Error
		if e.Kind == "delivery" || e.Kind == "job" {
			if err := t.replayWork(e.Kind, e.Body, e.At); err != nil {
				return fmt.Errorf("entry %d: %v", i+1, err)
			}
			continue
		}
		if e.Kind == "submission" {
			s := &pb.Submission{}
			if protojson.Unmarshal(e.Body, s) != nil {
				return fmt.Errorf("entry %d: bad submission", i+1)
			}
			_, err = a.Submit(t.caller(m, a, true), s, e.At)
			t.remember(submitted(m.ID, a, s, e.At))
			t.enqueue(e.At)
		} else {
			_, err = a.Input(t.caller(m, a, true), e.Kind, e.Body, e.At)
			t.remember(AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + e.Kind})
			t.enqueue(e.At)
		}
		if err != nil {
			return fmt.Errorf("entry %d (%s %s): %v", i+1, e.App, e.Kind, err)
		}
	}
	return nil
}

func submitted(member string, a App, s *pb.Submission, at time.Time) AuditEntry {
	return AuditEntry{At: at, Member: member, App: a.Manifest().ID, Action: s.GetSchema().GetName(),
		Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId()}
}

// Catalog is what m may call: each app's actions for m's role there, and an
// action that uses other apps' actions only when m may call those too.
func (t *Tenant) Catalog(m Member) []Action {
	out := []Action{}
	for _, a := range t.apps {
		for _, action := range a.Manifest().Actions.For(m.Roles[a.Manifest().ID]) {
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner, schema := t.owner["action:"+used], used
				if strings.Contains(used, "#") {
					owner, schema, _ = t.provider(used)
				}
				return owner == nil || !owner.Manifest().Actions.Permits(m.Roles[owner.Manifest().ID], schema)
			}) {
				out = append(out, action)
			}
		}
	}
	return out
}

func (t *Tenant) Declarations() []*pb.AuthorityDeclaration {
	var out []*pb.AuthorityDeclaration
	for _, a := range t.apps {
		out = append(out, a.Declarations()...)
	}
	return out
}

// Apps describes the enabled apps from their manifests (ADR-0010): discovery,
// the requirement graph and the capability matrix read this.
func (t *Tenant) Apps() []AppInfo {
	out := []AppInfo{}
	for _, a := range t.apps {
		m := a.Manifest()
		info := AppInfo{ID: m.ID, Version: m.Version, Requires: append([]string{}, m.Requires...), Reads: append([]string{}, m.Reads...), Provides: []string{}, Consumes: []string{},
			Roles: m.Actions.Roles(), Capabilities: m.Actions.Capabilities(), Inputs: []string{}, Uses: []string{}, Subscribes: append([]string{}, m.Subscribes...)}
		for input, journaled := range m.Inputs {
			info.Inputs = append(info.Inputs, input+map[bool]string{true: "", false: " (not journaled)"}[journaled])
		}
		slices.Sort(info.Inputs)
		for _, pv := range m.Provides {
			info.Provides = append(info.Provides, pv.Protocol.ID())
		}
		for _, c := range m.Consumes {
			info.Consumes = append(info.Consumes, c.Protocol+map[bool]string{true: " (optional)", false: ""}[c.Optional])
		}
		for _, action := range m.Actions.actions {
			for _, used := range action.Uses {
				info.Uses = append(info.Uses, action.Schema+" → "+used)
			}
		}
		out = append(out, info)
	}
	return out
}

type AppInfo struct {
	ID           string           `json:"id"`
	Version      string           `json:"version"`
	Requires     []string         `json:"requires"`
	Reads        []string         `json:"reads"`
	Roles        []string         `json:"roles"`
	Capabilities []CapabilityInfo `json:"capabilities"`
	Inputs       []string         `json:"inputs"`
	Uses         []string         `json:"uses"`
	Subscribes   []string         `json:"subscribes"`
	Provides     []string         `json:"provides"`
	Consumes     []string         `json:"consumes"`
}

// Submit lets an app call another app's action, only along its declared
// requirements; the member acts with its own role in the called app. The call is
// part of the caller's input, so it is replayed with it, not recorded on its own.
func (c Caller) Submit(app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	target, err := c.peer(app)
	if err != nil {
		return nil, err
	}
	called := c.tenant.caller(c.Member, target, c.Replaying)
	called.Automation = c.Automation
	return target.Submit(called, s, now)
}

// Read lets an app read another app it requires.
func (c Caller) Read(app, name string) (any, *kernel.Error) {
	target, err := c.peer(app)
	if err != nil {
		return nil, err
	}
	called := c.tenant.caller(c.Member, target, c.Replaying)
	called.Automation = c.Automation
	return target.Read(called, name)
}

func (c Caller) peer(app string) (App, *kernel.Error) {
	if c.tenant == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	self := c.tenant.app(c.App)
	target := c.tenant.app(app)
	if self == nil || target == nil || !slices.Contains(self.Manifest().Requires, app) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} // undeclared requirement
	}
	return target, nil
}
