package monitoring

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func CountReadImage(ctx context.Context, tenant, outcome string) {
	Meters.ReadImages.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", tenant), attribute.String("platform.outcome", outcome)))
}
