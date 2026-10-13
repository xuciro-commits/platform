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

// Host is the composition aggregate used only by Attach. Owners receive their
// named consumer roles and cannot retain or request this aggregate.
type Host interface {
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
	// Below expands units only through active placements of this structure.
	Below(units []string, structure string, day platform.Date) []string
	// Holders are the members with role (any, when empty) in unit or a unit above it in structure on day.
	Holders(structure, unit, role string, day platform.Date) []string
	// Calendar is the working calendar of party ("member:<id>"; "" for the
	// tenant's) on day (ADR-0028 D7).
	Calendar(party string, day platform.Date) platform.Calendar
	// Element is an element of the enterprise model live on day (ADR-0067 D3).
	Element(id string, day platform.Date) (platform.ElementInfo, bool)
	// Related are the elements joined to element by a relationship stereotype
	// on day: those it points at (outgoing) or those pointing at it.
	Related(element, stereotype string, outgoing bool, day platform.Date) []string
}
