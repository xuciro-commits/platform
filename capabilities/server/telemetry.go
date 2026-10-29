package platformserver

import (
	"context"
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
}{}

func init() {
	m := otel.Meter("platformserver")
	meters.attempts, _ = m.Int64Counter("platform.work.attempts", metric.WithDescription("Attempts of owned work: deliveries, jobs, effects"))
	meters.failures, _ = m.Int64Counter("platform.work.failures", metric.WithDescription("Attempts of owned work that did not succeed"))
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
	registration, err := m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		now := time.Now()
		for _, t := range current() {
			h := t.Health(now)
			for _, q := range h.Queues {
				attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.app", q.App))
				o.ObserveInt64(depth, int64(q.Depth), attrs)
				o.ObserveFloat64(oldest, q.Oldest.Seconds(), attrs)
			}
			attrs := metric.WithAttributes(attribute.String("platform.tenant", t.ID))
			o.ObserveInt64(failed, int64(h.Failed), attrs)
			o.ObserveInt64(open, int64(h.OpenBreakers), attrs)
		}
		return nil
	}, depth, oldest, failed, open)
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
