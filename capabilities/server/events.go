package platformserver

import automation "platformserver/engine/automation"

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"time"
)

func (t *Tenant) publish(e platform.Event) {
	e.Changed = t.Changed(e)
	t.events = append(t.events, automation.QueuedEvent{Event: e, Hops: t.hops, Span: t.current()})
	t.acted++
}

func (t *Tenant) planAcceptedEvent(e platform.Event) (platform.AcceptedEventPlan, *kernel.Error) {
	names := append([]string{e.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(e)...)
	effects, refusal := t.eventEffects(e, names, true)
	if refusal != nil {
		return platform.AcceptedEventPlan{}, refusal
	}
	plan := platform.AcceptedEventPlan{App: e.App, Changed: slices.Clone(e.Changed), Names: names,
		Effects: effects}
	for _, a := range t.liveApps() {
		if slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
			plan.Subscribers = append(plan.Subscribers, a.Manifest().ID)
		}
	}
	for _, id := range t.listeners {
		if id != e.App && t.app(id).(platform.Listener).Interested(names, e) &&
			!slices.Contains(plan.Subscribers, id) {
			plan.Subscribers = append(plan.Subscribers, id)
		}
	}
	return plan, nil
}

func (t *Tenant) publishAccepted(e platform.Event, plan platform.AcceptedEventPlan, version int) {
	t.acted++
	c := automation.QueuedEvent{Event: e, Hops: t.hops, Span: t.current()}
	if version >= 2 {
		c.Plan = &plan
		c.Hops = plan.Hops
	}
	t.events = append(t.events, c)
}

func (t *Tenant) Deliveries() []platform.Delivery { return t.audit.RecentDeliveries() }

func submitted(member string, a platform.App, s *pb.Submission, at time.Time) platform.AuditEntry {
	return platform.AuditEntry{At: at, Member: member, App: a.Manifest().ID, Action: s.GetSchema().GetName(),
		Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId()}
}
