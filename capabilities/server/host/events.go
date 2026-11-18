package host

import automation "platformserver/services/automate/runtime"

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func (t *Tenant) publish(e platform.Event) {
	e.Changed = t.Changed(e)
	t.events = append(t.events, automation.QueuedEvent{Event: e, Hops: t.hops, Span: t.current()})
	t.acted++
}

func (t *Tenant) planAcceptedEvent(e platform.Event) (protocol.AcceptedEventPlan, *platform.Error) {
	return automation.AcceptedEventPlanner{
		Routed: t.router().Events,
		Effects: func(e platform.Event, names []string) ([]platform.Effect, *platform.Error) {
			return t.eventEffects(e, names, true)
		},
		Apps:      t.LiveApps,
		Listeners: t.Listeners,
		Listener:  func(id string) platform.Listener { return t.App(id).(platform.Listener) },
	}.Plan(e)
}

func (t *Tenant) publishAccepted(e platform.Event, plan protocol.AcceptedEventPlan) {
	t.acted++
	t.events = append(t.events, automation.QueuedEvent{Event: e, Hops: plan.Hops, Span: t.current(), Plan: &plan})
}

func (t *Tenant) Deliveries() []protocol.Delivery { return t.AuditLog.RecentDeliveries() }
