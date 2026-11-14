package host

import (
	"context"
	runtimekernel "platformserver/kernel"
	"platformserver/services/resources/monitoring"
	"slices"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type preparedAccepted struct {
	entry   Entry
	key     string
	hash    string
	issues  []platform.FieldIssue
	uploads []string
}

func decideOnImage[T any](t *Tenant, ctx context.Context, holder string, prepare func(*Tenant) (T, *kernel.Error), finish func(*Tenant, preparedAccepted, T, *kernel.Error) (T, *kernel.Error)) (T, *kernel.Error) {
	attempt := func(state *Tenant) (value T, candidate preparedAccepted, refusal *kernel.Error, err error) {
		value, refusal, err = readImageState(t, state, ctx, func(scope *Tenant) (T, *kernel.Error) {
			scope.preparedResult = &candidate
			return prepare(scope)
		})
		return
	}
	return runtimekernel.ImageDecision[T, preparedAccepted]{
		Stopped:     t.businessStopped,
		Quarantined: t.quarantined,
		Hold:        func() error { return t.Lane.Hold(ctx, holder) },
		Unlock:      t.Lane.Unlock,
		Unavailable: func() bool { return t.recordReadRefusal() != nil || t.businessStopped() },
		Load: func() (func() (T, preparedAccepted, *kernel.Error, error), func(preparedAccepted) bool) {
			image := t.readImage.Load()
			if image == nil {
				return nil, nil
			}
			return func() (T, preparedAccepted, *kernel.Error, error) { return attempt(image.state) },
				func(candidate preparedAccepted) bool {
					t.Projection.Lock()
					generation := t.Projection.Generation()
					t.Projection.Unlock()
					return image.epoch == t.StateEpoch.Load() && image.definitions == t.DefinitionEpoch.Load() && image.records == generation && image.position == t.Projection.Current().Applied() && !slices.ContainsFunc(candidate.uploads, t.uploads.Unavailable) && (holder != "work" || image.work == t.WorkEpoch.Load())
				}
		},
		Direct: func() (T, preparedAccepted, *kernel.Error, error) { return attempt(t) },
		Finish: func(candidate preparedAccepted, value T, refusal *kernel.Error) (T, *kernel.Error) {
			return finish(t, candidate, value, refusal)
		},
		Count: func(outcome string) {
			monitoring.Meters.Decisions.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
		},
	}.Run(ctx)
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

func (t *Tenant) stageCandidate(draft *stagedDecision, app string, body, principal []byte, key, hash string, at time.Time, finish func(preparedAccepted)) {
	candidate := preparedAccepted{entry: Entry{App: app, Kind: "accepted-result", Principal: principal, Body: body, At: at, Versions: draft.FlowVersions()}, key: key, hash: hash}
	if !t.stage(candidate) {
		finish(candidate)
	}
}
