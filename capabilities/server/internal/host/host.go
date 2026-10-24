// Package host is what the host offers its own apps beyond the app API.
// The platform's apps import platformserver/platform and this
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
	WorkHost
	AgentHost
	ConsoleHost
}

// Listener is a platform app delivered other apps' events as owned work, with
// every name an event goes by (its action and the protocol events it is).
type Listener interface {
	Interested(names []string, e platform.Event) bool
	Listen(c platform.Caller, e platform.Event, names []string, now time.Time) *kernel.Error
}

// Tasks serves Caller.Assign for every app and closes tasks a platform app
// no longer waits for.
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

// Directory answers who belongs where in the organisation, for
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
	// tenant's) on day.
	Calendar(party string, day platform.Date) platform.Calendar
	// Element is an element of the enterprise model live on day.
	Element(id string, day platform.Date) (platform.ElementInfo, bool)
	// Related are the elements joined to element by a relationship stereotype
	// on day: those it points at (outgoing) or those pointing at it.
	Related(element, stereotype string, outgoing bool, day platform.Date) []string
}
