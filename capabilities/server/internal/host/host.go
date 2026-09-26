// Package host is what the host offers its own apps beyond the app API
// (ADR-0025 D4): the platform's apps import platformserver/platform and this
// package, never the host runtime. Business apps outside this module cannot
// import it. The host detects an app's roles by the interfaces it implements;
// it keeps no field for any particular app.
package host

import (
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
}
