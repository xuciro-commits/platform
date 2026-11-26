package host

import (
	"context"
	"maps"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/resources/monitoring"
	"slices"
	"time"

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

func decideOnImage[T any](t *Tenant, ctx context.Context, holder string, prepare func(*Tenant) (T, *platform.Error), finish func(*Tenant, preparedAccepted, T, *platform.Error) (T, *platform.Error)) (T, *platform.Error) {
	attempt := func(state *Tenant) (value T, decision preparedAccepted, refusal *platform.Error, err error) {
		value, refusal, err = readImageState(t, state, ctx, func(scope *Tenant) (T, *platform.Error) {
			scope.preparedResult = &decision
			if state == t {
				if image := t.readImage.Load(); image != nil && maps.Equal(image.state.disabledOwners, t.disabledOwners) {
					scope.readScope.definitions = image.state.readScope.definitions.HitsOnly()
				}
			}
			return prepare(scope)
		})
		return
	}
	return runtimekernel.ImageDecision[T, preparedAccepted]{
		Stopped:     t.businessStopped,
		Quarantined: t.quarantined,
		Hold:        func() error { return t.Lane.Hold(ctx, holder) },
		Unlock:      t.Lane.Unlock,
		Contended:   func() bool { return t.Lane.State().Waiters > 0 },
		Unavailable: func() bool { return t.recordReadRefusal() != nil || t.businessStopped() },
		Load: func() (func() (T, preparedAccepted, *platform.Error, error), func(preparedAccepted) bool) {
			image := t.readImage.Load()
			if image == nil {
				return nil, nil
			}
			return func() (T, preparedAccepted, *platform.Error, error) { return attempt(image.state) },
				func(decision preparedAccepted) bool {
					t.Projection.Lock()
					generation := t.Projection.Generation()
					t.Projection.Unlock()
					return image.epoch == t.StateEpoch.Load() && image.definitions == t.DefinitionEpoch.Load() && image.records == generation && image.position == t.Projection.Current().Applied() && !slices.ContainsFunc(decision.uploads, t.uploads.Unavailable) && (holder != "work" || image.work == t.WorkEpoch.Load())
				}
		},
		Direct: func() (T, preparedAccepted, *platform.Error, error) { return attempt(t) },
		Finish: func(decision preparedAccepted, value T, refusal *platform.Error) (T, *platform.Error) {
			return finish(t, decision, value, refusal)
		},
		Count: func(outcome string) {
			monitoring.Meters.Decisions.Add(ctx, 1, metric.WithAttributes(attribute.String("platform.tenant", t.ID), attribute.String("platform.outcome", outcome)))
		},
	}.Run(ctx)
}

func decideWithError[T any](t *Tenant, holder string, prepare func(*Tenant) (T, error), apply func(*Tenant, preparedAccepted) (T, error)) (T, error) {
	result, refusal := decideOnImage(t, context.Background(), holder, func(t *Tenant) (readAnswer[T], *platform.Error) {
		value, err := prepare(t)
		return readAnswer[T]{value: value, err: err}, nil
	}, func(t *Tenant, decision preparedAccepted, value readAnswer[T], refusal *platform.Error) (readAnswer[T], *platform.Error) {
		if len(decision.entry.Body) > 0 {
			value.value, value.err = apply(t, decision)
		}
		return value, refusal
	})
	if refusal != nil {
		return result.value, refusal
	}
	return result.value, result.err
}

func (t *Tenant) stageDecision(draft *stagedDecision, app string, body, principal []byte, key, hash string, at time.Time, finish func(preparedAccepted)) {
	decision := preparedAccepted{entry: Entry{App: app, Kind: "accepted-result", Principal: principal, Body: body, At: at, Versions: draft.FlowVersions()}, key: key, hash: hash}
	if !t.stage(decision) {
		finish(decision)
	}
}
