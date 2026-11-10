package host

import (
	"context"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type preparedAccepted struct {
	entry   Entry
	key     string
	hash    string
	issues  []platform.FieldIssue
	uploads []string
}

func (t *Tenant) countDecision(ctx context.Context, outcome string) {
	meters.decisions.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
}

func (t *Tenant) matchesReadImage(image *tenantReadImage) bool {
	t.records.Lock()
	generation := t.records.Generation()
	t.records.Unlock()
	return image.epoch == t.stateEpoch.Load() && image.definitions == t.defGen.Load() && image.records == generation && image.position == t.records.Current().Applied()
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
		value, refusal, err := readImageState(t, image.state, ctx, func(scope *Tenant) (T, *kernel.Error) {
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
		if t.matchesReadImage(image) && !slices.ContainsFunc(candidate.uploads, t.uploads.Unavailable) && (holder != "work" || image.work == t.workEpoch.Load()) {
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
	for _, app := range t.apps {
		if _, ok := app.(platform.ReadForkApp); !ok {
			return prepare(t)
		}
	}
	var candidate preparedAccepted
	value, refusal, err := readImageState(t, t, ctx, func(scope *Tenant) (T, *kernel.Error) { scope.preparedResult = &candidate; return prepare(scope) })
	if err != nil {
		return value, refusal
	}
	return finish(t, candidate, value, refusal)
}

func decideWithError[T any](t *Tenant, holder string, prepare func(*Tenant) (T, error), apply func(*Tenant, preparedAccepted) (T, error)) (T, error) {
	result, refusal := decideOnImage(t, context.Background(), holder, func(t *Tenant) (readAnswer[T], *kernel.Error) {
		value, err := prepare(t)
		return readAnswer[T]{value: value, err: err}, nil
	}, func(t *Tenant, candidate preparedAccepted, value readAnswer[T], refusal *kernel.Error) (readAnswer[T], *kernel.Error) {
		if len(candidate.entry.Body) > 0 {
			value.value, value.err = apply(t, candidate)
		}
		return value, refusal
	})
	if refusal != nil {
		return result.value, refusal
	}
	return result.value, result.err
}

func (t *Tenant) preparedUploads() []string {
	if t.preparedResult == nil {
		return nil
	}
	return t.preparedResult.uploads
}
