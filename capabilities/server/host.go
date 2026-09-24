package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
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
// who could act was decided when the input was accepted (ADR-0008).
type Caller struct {
	Member
	App       string
	Replaying bool
	tenant    *Tenant
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
}

// App is one app's instance in one tenant.
type App interface {
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	Read(c Caller, name string) any
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
}

// NewTenant enables apps for a tenant; it refuses duplicate names and unmet requirements.
func NewTenant(id string, apps ...App) (*Tenant, error) {
	t := &Tenant{ID: id, apps: apps, owner: map[string]App{}}
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
	record, err := a.Submit(t.caller(m, a, false), s, now)
	if err == nil {
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
	out, err := a.Input(t.caller(m, a, false), name, body, now)
	if err == nil && a.Manifest().Inputs[name] {
		t.record(a, name, m, body, now)
	}
	return out, err
}

// Read serves a named read of the app that declares it.
func (t *Tenant) Read(m Member, name string) (any, bool) {
	a := t.owner["read:"+name]
	if a == nil {
		return nil, false
	}
	return a.Read(t.caller(m, a, false), name), true
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
		if e.Kind == "submission" {
			s := &pb.Submission{}
			if protojson.Unmarshal(e.Body, s) != nil {
				return fmt.Errorf("entry %d: bad submission", i+1)
			}
			_, err = a.Submit(t.caller(m, a, true), s, e.At)
		} else {
			_, err = a.Input(t.caller(m, a, true), e.Kind, e.Body, e.At)
		}
		if err != nil {
			return fmt.Errorf("entry %d (%s %s): %v", i+1, e.App, e.Kind, err)
		}
	}
	return nil
}

// Catalog is what m may call: each app's actions for m's role there, and an
// action that uses other apps' actions only when m may call those too.
func (t *Tenant) Catalog(m Member) []Action {
	out := []Action{}
	for _, a := range t.apps {
		for _, action := range a.Manifest().Actions.For(m.Roles[a.Manifest().ID]) {
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner := t.owner["action:"+used]
				return owner == nil || !owner.Manifest().Actions.Permits(m.Roles[owner.Manifest().ID], used)
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

// Apps describes the enabled apps for discovery (ADR-0010).
func (t *Tenant) Apps() []AppInfo {
	var out []AppInfo
	for _, a := range t.apps {
		m := a.Manifest()
		out = append(out, AppInfo{ID: m.ID, Version: m.Version, Requires: append([]string{}, m.Requires...), Reads: m.Reads, Actions: len(m.Actions.actions)})
	}
	return out
}

type AppInfo struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Requires []string `json:"requires"`
	Reads    []string `json:"reads"`
	Actions  int      `json:"actions"`
}

// Submit lets an app call another app's action, only along its declared
// requirements; the member acts with its own role in the called app. The call is
// part of the caller's input, so it is replayed with it, not recorded on its own.
func (c Caller) Submit(app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	target, err := c.peer(app)
	if err != nil {
		return nil, err
	}
	return target.Submit(c.tenant.caller(c.Member, target, c.Replaying), s, now)
}

// Read lets an app read another app it requires.
func (c Caller) Read(app, name string) (any, *kernel.Error) {
	target, err := c.peer(app)
	if err != nil {
		return nil, err
	}
	return target.Read(c.tenant.caller(c.Member, target, c.Replaying), name), nil
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
