package host

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type preparedAccepted struct {
	entry  Entry
	key    string
	hash   string
	issues []platform.FieldIssue
}

func (t *Tenant) countDecision(ctx context.Context, outcome string) {
	meters.decisions.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
}

func (t *Tenant) matchesReadImage(image *tenantReadImage) bool {
	t.records.Lock()
	generation := t.records.Generation()
	t.records.Unlock()
	return image.epoch == t.stateEpoch.Load() && image.definitions == t.defGen.Load() && image.records == generation && image.position == t.records.Current().Applied() && image.state.systemDefinitionState.published.Load() == t.systemDefinitionState.published.Load()
}

func decideOnImage[T any](t *Tenant, ctx context.Context, holder string, prepare func(*Tenant) (T, *kernel.Error), finish func(*Tenant, preparedAccepted, T, *kernel.Error) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil || t.businessStopped() {
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		image := t.readImage.Load()
		if image == nil {
			break
		}
		var candidate preparedAccepted
		value, refusal, err := readPublishedImage(t, image, ctx, func(scope *Tenant) (T, *kernel.Error) {
			scope.preparedResult = &candidate
			return prepare(scope)
		})
		if err != nil {
			if ctx.Err() != nil || t.quarantined() {
				return value, refusal
			}
			break
		}
		if err := t.mu.Hold(ctx, holder); err != nil {
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if refusal := t.recordReadRefusal(); refusal != nil || t.businessStopped() {
			t.mu.Unlock()
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if t.matchesReadImage(image) && (holder != "work" || image.work == t.workEpoch.Load()) {
			func() {
				defer t.mu.Unlock()
				value, refusal = finish(t, candidate, value, refusal)
			}()
			return value, refusal
		}
		t.mu.Unlock()
		if attempt < 2 {
			t.countDecision(ctx, "retried")
		}
	}
	t.countDecision(ctx, "fallback")
	if err := t.mu.Hold(ctx, holder); err != nil {
		return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	defer t.mu.Unlock()
	if refusal := t.recordReadRefusal(); refusal != nil || t.businessStopped() {
		return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return prepare(t)
}
