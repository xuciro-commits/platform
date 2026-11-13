package host

import (
	"context"
	"time"

	runtimekernel "platformserver/kernel"
	"platformserver/services/resources/monitoring"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (t *Tenant) begin(name string, parent trace.SpanContext, attrs ...attribute.KeyValue) func(outcome string) {
	ctx := t.readContext()
	if ctx == nil || parent.IsValid() {
		ctx = trace.ContextWithSpanContext(context.Background(), parent)
	}
	ctx, span := monitoring.Tracer.Start(ctx, name, trace.WithAttributes(append(attrs, attribute.String("platform.tenant", t.ID))...))
	previous := t.readScope
	scope := &recordReadScope{ctx: ctx}
	if previous != nil {
		*scope = *previous
		scope.ctx = ctx
	}
	t.readScope = scope
	return func(outcome string) {
		monitoring.End(span, outcome)
		t.readScope = previous
	}
}

func (t *Tenant) current() trace.SpanContext {
	if t.readScope == nil {
		return trace.SpanContext{}
	}
	return trace.SpanContextFromContext(t.readContext())
}

func (t *Tenant) observeLane() {
	t.Lane.Observe(monitoring.LaneObserver(t.ID, func(holder string, held time.Duration) {
		t.LastSlowHold.Store(&runtimekernel.LaneHold{Holder: holder, Seconds: held.Seconds(), At: time.Now().UTC()})
	}))
}

func observeFrom(current func() []*Tenant) func() error {
	return monitoring.ObserveTenants(func(now time.Time) []monitoring.TenantSample {
		var samples []monitoring.TenantSample
		for _, t := range current() {
			queues, failed := t.queueHealth(now)
			lane := t.laneHealth(now, true)
			samples = append(samples, monitoring.TenantSample{ID: t.ID, Queues: queues, Failed: failed, OpenBreakers: monitoring.OpenBreakers(t.Breakers(now)), Waiters: lane.Waiters, HeldSeconds: lane.HeldSeconds})
		}
		return samples
	})
}
