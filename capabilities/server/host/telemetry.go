package host

import (
	"context"
	"log"
	"os"

	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("platformserver")

func (t *Tenant) begin(name string, parent trace.SpanContext, attrs ...attribute.KeyValue) func(outcome string) {
	ctx := t.ctx
	if ctx == nil || parent.IsValid() {
		ctx = trace.ContextWithSpanContext(context.Background(), parent)
	}
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(append(attrs, attribute.String("platform.tenant", t.ID))...))
	previous := t.ctx
	t.ctx = ctx
	return func(outcome string) {
		end(span, outcome)
		t.ctx = previous
	}
}

func (t *Tenant) current() trace.SpanContext {
	if t.ctx == nil {
		return trace.SpanContext{}
	}
	return trace.SpanContextFromContext(t.ctx)
}

func outside(parent trace.SpanContext, name string, attrs ...attribute.KeyValue) trace.Span {
	_, span := tracer.Start(trace.ContextWithSpanContext(context.Background(), parent), name, trace.WithAttributes(attrs...))
	return span
}

func end(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("platform.outcome", outcome))
	if outcome != "ok" {
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
}

var meters = struct {
	attempts, failures metric.Int64Counter
	laneWait, laneHold metric.Float64Histogram
}{}

const laneHoldBudget = 200 * time.Millisecond

func init() {
	m := otel.Meter("platformserver")
	meters.attempts, _ = m.Int64Counter("platform.work.attempts", metric.WithDescription("Attempts of owned work: deliveries, jobs, effects"))
	meters.failures, _ = m.Int64Counter("platform.work.failures", metric.WithDescription("Attempts of owned work that did not succeed"))
	meters.laneWait, _ = m.Float64Histogram("platform.lane.wait", metric.WithUnit("s"), metric.WithDescription("Time spent waiting for the tenant lane, per holder kind"))
	meters.laneHold, _ = m.Float64Histogram("platform.lane.hold", metric.WithUnit("s"), metric.WithDescription("Time the tenant lane was held, per holder kind"))
}

func (t *Tenant) observeLane() {
	tenant := t.ID
	t.mu.Observe(func(holder string, waited, held time.Duration) {
		attrs := metric.WithAttributes(attribute.String("platform.tenant", tenant), attribute.String("platform.lane.holder", holder))
		meters.laneWait.Record(context.Background(), waited.Seconds(), attrs)
		meters.laneHold.Record(context.Background(), held.Seconds(), attrs)
		if held > laneHoldBudget {
			t.laneLastSlow.Store(&LaneHold{Holder: holder, Seconds: held.Seconds(), At: time.Now().UTC()})
			log.Printf("tenant %s lane held %v by %s (budget %v)", tenant, held.Round(time.Millisecond), holder, laneHoldBudget)
		}
	})
}

func counted(t *Tenant, kind, app, outcome string) {
	attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.app", app), attribute.String("platform.kind", kind))
	meters.attempts.Add(context.Background(), 1, attrs)
	if outcome != "ok" && outcome != "delivered" {
		meters.failures.Add(context.Background(), 1, attrs)
	}
}

func observeFrom(current func() []*Tenant) func() error {
	m := otel.Meter("platformserver")
	depth, _ := m.Int64ObservableGauge("platform.queue.depth", metric.WithDescription("Owned work waiting, per tenant and app"))
	oldest, _ := m.Float64ObservableGauge("platform.queue.oldest", metric.WithUnit("s"), metric.WithDescription("Age of the oldest waiting item, per tenant and app"))
	failed, _ := m.Int64ObservableGauge("platform.work.failed", metric.WithDescription("Owned work that gave up and waits for a person"))
	open, _ := m.Int64ObservableGauge("platform.breakers.open", metric.WithDescription("Destinations whose breaker is open"))
	waiters, _ := m.Int64ObservableGauge("platform.lane.waiters", metric.WithDescription("Callers waiting for the tenant lane"))
	held, _ := m.Float64ObservableGauge("platform.lane.held", metric.WithUnit("s"), metric.WithDescription("How long the current tenant lane holder has held it"))
	registration, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		now := time.Now()
		for _, t := range current() {
			queues, failures := t.queueHealth(now)
			for _, q := range queues {
				attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.app", q.App))
				o.ObserveInt64(depth, int64(q.Depth), attrs)
				o.ObserveFloat64(oldest, q.Oldest.Seconds(), attrs)
			}
			openBreakers := 0
			for _, b := range t.Breakers(now) {
				if b.State != "closed" {
					openBreakers++
				}
			}
			lane := t.laneHealth(now, true)
			attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID))
			o.ObserveInt64(failed, int64(failures), attrs)
			o.ObserveInt64(open, int64(openBreakers), attrs)
			o.ObserveInt64(waiters, lane.Waiters, attrs)
			o.ObserveFloat64(held, lane.HeldSeconds, attrs)
		}
		return nil
	}, depth, oldest, failed, open, waiters, held)
	if err != nil {
		return func() error { return err }
	}
	return registration.Unregister
}

func exportTelemetry(ctx context.Context, service string) func(context.Context) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) {}
	}
	res, _ := resource.Merge(resource.Default(), resource.NewSchemaless(attribute.String("service.name", service)))
	spans, err := otlptracehttp.New(ctx)
	if err != nil {
		return func(context.Context) {}
	}
	traces := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans), sdktrace.WithResource(res))
	otel.SetTracerProvider(traces)
	var metrics *sdkmetric.MeterProvider
	if exporter, err := otlpmetrichttp.New(ctx); err == nil {
		metrics = sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)), sdkmetric.WithResource(res))
		otel.SetMeterProvider(metrics)
	}
	return func(ctx context.Context) {
		traces.Shutdown(ctx)
		if metrics != nil {
			metrics.Shutdown(ctx)
		}
	}
}
