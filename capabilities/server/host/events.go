package host

import automation "platformserver/services/automate/runtime"

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"time"
)

func (t *Tenant) publish(e platform.Event) {
	e.Changed = t.Changed(e)
	t.events = append(t.events, automation.QueuedEvent{Event: e, Hops: t.hops, Span: t.current()})
	t.acted++
}

func (t *Tenant) planAcceptedEvent(e platform.Event) (platform.AcceptedEventPlan, *kernel.Error) {
	return automation.AcceptedEventPlanner{
		Routed: t.router().Events,
		Effects: func(e platform.Event, names []string) ([]platform.Effect, *kernel.Error) {
			return t.eventEffects(e, names, true)
		},
		Apps:      t.LiveApps,
		Listeners: t.listeners,
		Listener:  func(id string) platform.Listener { return t.App(id).(platform.Listener) },
	}.Plan(e)
}

func (t *Tenant) publishAccepted(e platform.Event, plan platform.AcceptedEventPlan) {
	t.acted++
	t.events = append(t.events, automation.QueuedEvent{Event: e, Hops: plan.Hops, Span: t.current(), Plan: &plan})
}

func (t *Tenant) Deliveries() []platform.Delivery { return t.audit.RecentDeliveries() }

func submitted(member string, a platform.App, s *pb.Submission, at time.Time) platform.AuditEntry {
	return platform.AuditEntry{ID: "submit:" + a.Manifest().ID + ":" + s.GetIdempotencyKey(), At: at, Member: member, App: a.Manifest().ID, Action: s.GetSchema().GetName(),
		Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId()}
}
