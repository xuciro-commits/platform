// Package platform is what an app sees of the platform (#104 G1): its manifest
// and declarations, the caller of each input with what the host does for it,
// and the ledger that receives its decisions. The host runtime (package
// platformserver: tenants, hosts, the journal, deployment) implements Runtime;
// an app imports this package only, so it cannot reach the runtime.
package platform

import (
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Member is a person, service or AI agent of a tenant (ADR-0010) with one role
// per app. What a rule scopes by is the member's organisation (ADR-0012).
type Member struct {
	ID     string            `json:"id"`
	Tenant string            `json:"tenant"`
	Roles  map[string]string `json:"roles"`
	// Agent marks an AI agent: an irreversible effect it causes waits for a
	// person's approval, and it cannot give one (ADR-0014 D6).
	Agent bool `json:"agent,omitempty"`
	// Language is the member's own choice (ADR-0023 6b), such as "zh-CN";
	// empty: the tenant's default, else what the browser asks for.
	Language string `json:"language,omitempty"`
}

// Caller is a member as one app sees it. Replaying marks the journal replay:
// who could act was decided when the input was accepted (ADR-0008). Automation
// marks an app acting on its own (a subscribed event, a job, an effect's
// answer), as member "app:<id>"; its manifest is its grant.
type Caller struct {
	Member
	App        string
	Replaying  bool
	Automation bool
	rt         Runtime
}

// Role is the member's role in the app being called ("" for none).
func (c Caller) Role() string { return c.Roles[c.App] }

// As views member from app, without a host (tests of one app).
func As(app string, m Member) Caller { return Caller{Member: m, App: app} }

// NewCaller is how the host runtime hands an app its caller.
func NewCaller(rt Runtime, m Member, app string, replaying, automation bool) Caller {
	return Caller{Member: m, App: app, Replaying: replaying, Automation: automation, rt: rt}
}

// Runtime is what the host does for a caller; package platformserver implements
// it. Each method acts as c, for c's app, within the input being handled.
type Runtime interface {
	Publish(c Caller, record *pb.ChangeRecord)
	Invoke(c Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)
	Query(c Caller, protocol, read string) ([]ProviderResult, *kernel.Error)
	Notify(c Caller, n Notification, now time.Time, to []Recipient) []string
	Setting(c Caller, name string) string
	Emit(c Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error)
	Units(c Caller, structure string, now time.Time) []string
	Links(c Caller, entity string) []string
	Link(c Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error
	Deliver(c Caller, dataClass, from, to string, now time.Time) *kernel.Error
	// Records of the app's entity types (ADR-0016).
	Put(c Caller, r *pb.ChangeRecord, entity any) *kernel.Error
	Get(c Caller, t reflect.Type, id string) (any, bool)
	Find(c Caller, t reflect.Type, q Query) ([]any, int, *kernel.Error)
	Check(c Caller, entity any) *kernel.Error
	// Work (ADR-0017): tasks for people, and whether decisions are only probed.
	Assign(c Caller, r *pb.ChangeRecord, a Assignment) *kernel.Error
	Probing() bool
}

// Manifest declares an app (ADR-0010). Names of actions, reads and inputs are
// unique within a tenant; the host routes by them.
type Manifest struct {
	ID      string
	Title   string // the app's name in the workspace's launcher (ADR-0018)
	Version string
	Actions *Catalog
	Reads   []string
	Inputs  map[string]bool // connector inputs; true: recorded in the journal
	// Subscribes names its own actions, or events of protocols it consumes
	// ("<protocol id>#<event>"), delivered to Handle after commit. Apps know no
	// other app: they reach each other through protocols only (ADR-0011).
	Subscribes []string
	Provides   []Provision   // protocols this app implements (ADR-0011)
	Consumes   []Consumption // protocols this app uses; the host binds a provider
	Everyone   []string      // reads any member may use; the app filters by caller
	Jobs       []Job         // scheduled work (Runner), ADR-0013
	Settings   []Setting     // typed values administrators set in Settings
	Emits      []EffectKind  // outbound effects it sends to endpoints the tenant binds (ADR-0014)
	Entities   []Entity      // entity types whose records the host keeps (ADR-0016)
	Flows      []Flow        // long-running processes the host runs for the app (ADR-0020)
	Agents     []Agent       // AI agents the host runs for the app (ADR-0021)
	// Roles are roles that grant no action but open something else, such as
	// models (ADR-0015); the roles its actions grant need not be listed.
	Roles []string
	// Languages translate the app's titles and descriptions (ADR-0023).
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
	Manifest() Manifest
	Declarations() []*pb.AuthorityDeclaration
	Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	// Read serves a named read; the app refuses callers it does not show it to.
	Read(c Caller, name string) (any, *kernel.Error)
	Input(c Caller, name string, body []byte, now time.Time) (any, *kernel.Error)
}

// Event is an accepted decision, delivered to subscribers after commit.
type Event struct {
	App    string
	Record *pb.ChangeRecord
}

// Subscriber is an app that handles the events its manifest subscribes to, as
// owned work after the input (ADR-0013). A refusal is retried, then recorded as
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
type Answerer interface {
	Answer(c Caller, e Effect, o Outcome, now time.Time) *kernel.Error
}

func notFound() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }
