// Package host is what the host offers its own apps beyond the app API
// (ADR-0025 D4): the platform's apps import platformserver/platform and this
// package, never the host runtime. Business apps outside this module cannot
// import it. The host detects an app's roles by the interfaces it implements;
// it keeps no field for any particular app.
package host

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Host is the tenant as its own apps see it.
type Host interface {
	// OwnerOf is the app that declares a data class ("crm.opportunity").
	OwnerOf(dataClass string) (app string, ok bool)
	// ProtocolEvent is a protocol event's declaration, by "<protocol id>#<event>".
	ProtocolEvent(name string) (platform.ProtocolEvent, bool)
	// As is c acting in app: the same member, replay and automation, with the
	// roles it holds there (a platform app deciding for a caller of another app).
	As(c platform.Caller, app string) platform.Caller
	// Caller is member m acting in app; Automation is app acting as itself.
	Caller(parent platform.Caller, m platform.Member, app string) platform.Caller
	Automation(parent platform.Caller, app string) platform.Caller
	// Member is a member of the tenant by ID, with its roles now; Holding are
	// the members holding role in app.
	Member(id string) (platform.Member, bool)
	Holding(app, role string) []string
	// Action is an action's declaration and the app declaring it.
	Action(schema string) (app string, a platform.Action, ok bool)
	// Submit routes s to the app declaring its action, as c's member, inside
	// the input being handled: it is not journaled apart, the input replays it.
	Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	// Attempt isolates an optional nested decision. A refusal discards its
	// changes, allowing the parent to record the refused business outcome.
	Attempt(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error)
	// Recipients resolves recipients to members on now's day.
	Recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string
	// Directory is the organisation, or nil when the tenant runs none.
	Directory() Directory
	// Readable reports whether m may read the record ref ("<type>/<id>") now.
	Readable(m platform.Member, ref string, now time.Time) bool
	// Stored reports whether the tenant's file store holds the bytes with this sha256 (ADR-0028).
	Stored(tenant, hash string) bool
	// Record is a record by "<type>/<id>" as its app holds it (an event's Changed ones, ADR-0028 D8).
	Record(ref string) (any, bool)
	// Install declares an entity the app composed at runtime — an object someone
	// defined in this tenant (ADR-0034) — with the actions generated for it and
	// the pages people open it through. Installed again, the type takes its new
	// fields and its records come with them. The app teaches its own ledger the
	// data class and schemas first (Ledger.Extend).
	Install(c platform.Caller, e platform.Entity, actions []platform.Action, pages ...platform.Page) error
	ValidateInstall(e platform.Entity, actions []platform.Action, pages ...platform.Page) error
	// The named automatic page may be replaced; other installed pages must
	// still validate against this object's new fields and actions.
	ValidateInstallDependents(e platform.Entity, actions []platform.Action, generatedPage string, pages ...platform.Page) error
	// InstallPage offers a page someone composed in this tenant (ADR-0034): the
	// same descriptor a manifest declares, checked against what is installed and
	// served to the members who may read its object. Composed again, it replaces
	// the one before it.
	InstallPage(c platform.Caller, p platform.Page) error
	ValidateInstallPage(p platform.Page) error
	// InstallApplication offers an application someone in this tenant handed to
	// its people (ADR-0036): a name, an icon and the pages it holds, checked
	// against what is installed. Handed over again, it replaces the one before.
	InstallApplication(c platform.Caller, a platform.Application) error
	ValidateInstallApplication(a platform.Application) error
	// Function installation updates only the definition registry. The owner
	// retains its publication family; inference still uses the model effect.
	ValidateInstallQuery(q platform.NamedQuery) error
	InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error
	ValidateInstallFunction(f platform.AIFunction) error
	InstallFunction(c platform.Caller, f platform.AIFunction, version int) error
	ValidateInstallOperation(o platform.Operation) error
	InstallOperation(c platform.Caller, o platform.Operation, version int) error
	// Function resolves an owner's retained AI function through its existing registry.
	Function(app, name string, version int) (platform.AIFunction, int, bool)
	Operation(app, name string, version int) (platform.Operation, int, bool)
	// Entity is an entity type's declaration, for an app composing over it.
	Entity(typ string) (platform.EntityInfo, bool)
	// Declares says whether an entity type, a field (<type>.<field>) or an action exists.
	Declares(name string) bool
	// Seen marks an app's notifications with any of keys read for everyone.
	Seen(c platform.Caller, keys ...string)
	// Invoke submits a protocol action to the tenant's provider for c's app,
	// inside the input being handled (a flow's step, ADR-0020).
	Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *kernel.Error)
	// The roles other platform apps take in this tenant, or nil.
	Tasks() Tasks
	Runs() Runs
	Processes() Processes
	// ActiveRelease is the tenant's active release ID, empty before any
	// activation; the caller already holds the tenant (ADR-0039 D4).
	ActiveRelease() string
	// BindFlow closes the exact native version over current owner assets. A
	// prior binding must still match; an empty prior captures a new run.
	BindFlow(app, name string, version int, prior FlowBinding) (FlowBinding, error)
}

// FlowBinding exposes hashes and asset paths, never private definition bytes.
// Release is empty for development without a matching activated flow asset.
type FlowBinding struct {
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

// Processes is the flow app (ADR-0020): it takes the flows every app
// declares, pins the versions instances took for replay, and hears an agent
// step's run end.
type Processes interface {
	Declare(a platform.App) error
	// Install registers a flow an app composed at runtime as its next version;
	// Validate checks one without registering it (#132).
	Install(a platform.App, fl platform.Flow) error
	// HasPublishedFlow checks published membership against the original flow owner.
	HasPublishedFlow(id string) bool
	Validate(a platform.App, fl platform.Flow) error
	// HasRunningDependency protects a composed flow's source and transitive
	// object dependencies while its instances are running, waiting, compensating or stuck.
	HasRunningDependency(typ string) (bool, error)
	// Check refuses to start when a running instance needs a version the code no longer declares.
	Check() error
	// Versions are those new instances took during the input being handled
	// (then forgotten), journaled with it; Pin gives a replay those of its entry.
	Versions() map[string]int
	Pin(versions map[string]int)
	RunEnded(c platform.Caller, run RunEnd, now time.Time)
}

// Runs are agent runs (ADR-0021) a flow's agent step starts and signals.
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

// RunSignal is what people made of a run's result (ADR-0021 D8).
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
// no longer waits for (ADR-0017).
type Tasks interface {
	Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error
	Close(c platform.Caller, r *pb.ChangeRecord, id string)
}

// Attached is an app the host hands itself to when a tenant is composed.
type Attached interface {
	Attach(Host)
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

// Directory answers who belongs where in the organisation (ADR-0012), for
// Caller.Units, approvals along the organisation and notifications to a unit's role.
type Directory interface {
	// Units are the units party ("member:<id>") belongs to on day, and those
	// below them in structure ("" for the units themselves).
	Units(party, structure string, day platform.Date) []string
	// Holders are the members with role (any, when empty) in unit or a unit above it in structure on day.
	Holders(structure, unit, role string, day platform.Date) []string
	// Calendar is the working calendar of party ("member:<id>"; "" for the
	// tenant's) on day (ADR-0028 D7).
	Calendar(party string, day platform.Date) platform.Calendar
}
