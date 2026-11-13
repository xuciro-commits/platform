package monitoring

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

var Tracer = otel.Tracer("platformserver")

func Outside(parent trace.SpanContext, name string, attrs ...attribute.KeyValue) trace.Span {
	_, span := Tracer.Start(trace.ContextWithSpanContext(context.Background(), parent), name, trace.WithAttributes(attrs...))
	return span
}

func End(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("platform.outcome", outcome))
	if outcome != "ok" {
		span.SetStatus(codes.Error, outcome)
	}
	span.End()
}

var Meters = struct {
	Attempts, Failures    metric.Int64Counter
	LaneWait, LaneHold    metric.Float64Histogram
	ReadImages, Decisions metric.Int64Counter
}{}

const laneHoldBudget = 200 * time.Millisecond

func init() {
	m := otel.Meter("platformserver")
	Meters.Attempts, _ = m.Int64Counter("platform.work.attempts", metric.WithDescription("Attempts of owned work: deliveries, jobs, effects"))
	Meters.Failures, _ = m.Int64Counter("platform.work.failures", metric.WithDescription("Attempts of owned work that did not succeed"))
	Meters.LaneWait, _ = m.Float64Histogram("platform.lane.wait", metric.WithUnit("s"), metric.WithDescription("Time spent waiting for the tenant lane, per holder kind"))
	Meters.LaneHold, _ = m.Float64Histogram("platform.lane.hold", metric.WithUnit("s"), metric.WithDescription("Time the tenant lane was held, per holder kind"))
	Meters.Decisions, _ = m.Int64Counter("platform.decision.conflicts", metric.WithDescription("Optimistic decision retries and lane fallbacks"))
	Meters.ReadImages, _ = m.Int64Counter("platform.read_image.retries", metric.WithDescription("Read image consistency retries, fallbacks and quarantine decisions"))
}

func LaneObserver(tenant string, slow func(holder string, held time.Duration)) func(holder string, waited, held time.Duration) {
	return func(holder string, waited, held time.Duration) {
		attrs := metric.WithAttributes(attribute.String("platform.tenant", tenant), attribute.String("platform.lane.holder", holder))
		Meters.LaneWait.Record(context.Background(), waited.Seconds(), attrs)
		Meters.LaneHold.Record(context.Background(), held.Seconds(), attrs)
		if held > laneHoldBudget {
			slow(holder, held)
			log.Printf("tenant %s lane held %v by %s (budget %v)", tenant, held.Round(time.Millisecond), holder, laneHoldBudget)
		}
	}
}

func Counted(tenant, kind, app, outcome string) {
	attrs := metric.WithAttributes(attribute.String("platform.tenant", tenant), attribute.String("platform.app", app), attribute.String("platform.kind", kind))
	Meters.Attempts.Add(context.Background(), 1, attrs)
	if outcome != "ok" && outcome != "delivered" {
		Meters.Failures.Add(context.Background(), 1, attrs)
	}
}

type TenantSample struct {
	ID           string
	Queues       []QueueHealth
	Failed       int
	OpenBreakers int
	Waiters      int64
	HeldSeconds  float64
}

func ObserveTenants(sample func(now time.Time) []TenantSample) func() error {
	m := otel.Meter("platformserver")
	depth, _ := m.Int64ObservableGauge("platform.queue.depth", metric.WithDescription("Owned work waiting, per tenant and app"))
	oldest, _ := m.Float64ObservableGauge("platform.queue.oldest", metric.WithUnit("s"), metric.WithDescription("Age of the oldest waiting item, per tenant and app"))
	failed, _ := m.Int64ObservableGauge("platform.work.failed", metric.WithDescription("Owned work that gave up and waits for a person"))
	open, _ := m.Int64ObservableGauge("platform.breakers.open", metric.WithDescription("Destinations whose breaker is open"))
	waiters, _ := m.Int64ObservableGauge("platform.lane.waiters", metric.WithDescription("Callers waiting for the tenant lane"))
	held, _ := m.Float64ObservableGauge("platform.lane.held", metric.WithUnit("s"), metric.WithDescription("How long the current tenant lane holder has held it"))
	registration, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for _, t := range sample(time.Now()) {
			for _, q := range t.Queues {
				attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.app", q.App))
				o.ObserveInt64(depth, int64(q.Depth), attrs)
				o.ObserveFloat64(oldest, q.Oldest.Seconds(), attrs)
			}
			attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID))
			o.ObserveInt64(failed, int64(t.Failed), attrs)
			o.ObserveInt64(open, int64(t.OpenBreakers), attrs)
			o.ObserveInt64(waiters, t.Waiters, attrs)
			o.ObserveFloat64(held, t.HeldSeconds, attrs)
		}
		return nil
	}, depth, oldest, failed, open, waiters, held)
	if err != nil {
		return func() error { return err }
	}
	return registration.Unregister
}

func ExportTelemetry(ctx context.Context, service string) func(context.Context) {
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
