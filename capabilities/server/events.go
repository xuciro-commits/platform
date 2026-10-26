package platformserver

// Accepted decisions reach their subscribers as planned events: the plan is
// frozen while the decision is private, then published and delivered.

import (
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/host"
	"platformserver/platform"
	"slices"
	"time"
)

// caused is an accepted decision with how many deliveries caused it.
type caused struct {
	platform.Event
	hops int
	span trace.SpanContext   // the input that caused it, whose trace its delivery continues
	plan *resultformat.Event // durable work/effect intent; nil for legacy entries
}

// publish queues an accepted decision for its subscribers (Ledger, through Runtime).
func (t *Tenant) publish(e platform.Event) {
	e.Changed = t.Changed(e)
	t.events = append(t.events, caused{Event: e, hops: t.hops, span: t.current()})
	t.acted++
}

// planAcceptedEvent freezes the subscribers and outbound intents while the
// decision is private. Recovery does not rediscover them from a newer
// manifest or changed endpoint subscriptions.
func (t *Tenant) planAcceptedEvent(e platform.Event) (resultformat.Event, *kernel.Error) {
	names := append([]string{e.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(e)...)
	effects, refusal := t.eventEffects(e, names, true)
	if refusal != nil {
		return resultformat.Event{}, refusal
	}
	plan := resultformat.Event{App: e.App, Changed: slices.Clone(e.Changed), Names: names,
		Effects: effects}
	for _, a := range t.liveApps() {
		if slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
			plan.Subscribers = append(plan.Subscribers, a.Manifest().ID)
		}
	}
	for _, id := range t.listeners {
		if id != e.App && t.app(id).(host.Listener).Interested(names, e) &&
			!slices.Contains(plan.Subscribers, id) {
			plan.Subscribers = append(plan.Subscribers, id)
		}
	}
	return plan, nil
}

func (t *Tenant) publishAccepted(e platform.Event, plan resultformat.Event, version int) {
	t.acted++
	c := caused{Event: e, hops: t.hops, span: t.current()}
	if version >= 2 {
		c.plan = &plan
		c.hops = plan.Hops
	}
	t.events = append(t.events, c)
}

// Deliveries is the tenant's recent event deliveries, oldest first.
func (t *Tenant) Deliveries() []Delivery { return t.audit.recentDeliveries() }

func submitted(member string, a platform.App, s *pb.Submission, at time.Time) AuditEntry {
	return AuditEntry{At: at, Member: member, App: a.Manifest().ID, Action: s.GetSchema().GetName(),
		Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId()}
}
