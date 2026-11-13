package host

import (
	"context"
	"platformserver/services/resources/monitoring"
	"slices"
	"time"

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
	monitoring.Meters.Decisions.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
}

func (t *Tenant) matchesReadImage(image *tenantReadImage) bool {
	t.Projection.Lock()
	generation := t.Projection.Generation()
	t.Projection.Unlock()
	return image.epoch == t.StateEpoch.Load() && image.definitions == t.DefinitionEpoch.Load() && image.records == generation && image.position == t.Projection.Current().Applied()
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
		if err := t.Lane.Hold(ctx, holder); err != nil {
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if refusal := t.recordReadRefusal(); refusal != nil || t.businessStopped() {
			t.Lane.Unlock()
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if t.matchesReadImage(image) && !slices.ContainsFunc(candidate.uploads, t.uploads.Unavailable) && (holder != "work" || image.work == t.WorkEpoch.Load()) {
			func() {
				defer t.Lane.Unlock()
				value, refusal = finish(t, candidate, value, refusal)
			}()
			return value, refusal
		}
		t.Lane.Unlock()
		if attempt < 2 {
			t.countDecision(ctx, "retried")
		}
	}
	t.countDecision(ctx, "fallback")
	if err := t.Lane.Hold(ctx, holder); err != nil {
		return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	defer t.Lane.Unlock()
	if refusal := t.recordReadRefusal(); refusal != nil || t.businessStopped() {
		return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
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

func (t *Tenant) stageCandidate(draft *stagedDecision, app string, body, principal []byte, key, hash string, at time.Time, finish func(preparedAccepted)) {
	candidate := preparedAccepted{entry: Entry{App: app, Kind: "accepted-result", Principal: principal, Body: body, At: at, Versions: draft.FlowVersions()}, key: key, hash: hash, uploads: t.preparedUploads()}
	if t.preparedResult != nil {
		*t.preparedResult = candidate
		return
	}
	finish(candidate)
}
