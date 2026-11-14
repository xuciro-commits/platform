package kernel

import (
	"context"
	"errors"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type ImageRead[T any] struct {
	Quarantined func() bool
	Load        func() (read func() (T, *kernel.Error, error), next <-chan struct{})
	Count       func(outcome string)
	Fallback    func() (T, *kernel.Error)
}

func (r ImageRead[T]) Run(ctx context.Context) (value T, refusal *kernel.Error) {
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil || r.Quarantined() {
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		read, next := r.Load()
		if read == nil {
			break
		}
		value, refusal, err := read()
		if err == nil {
			return value, refusal
		}
		var mismatch *persistence.StatePositionError
		if !errors.As(err, &mismatch) || mismatch.Position <= mismatch.Applied {
			if mismatch != nil {
				r.Count("quarantined")
			}
			return value, refusal
		}
		if attempt == 2 {
			break
		}
		r.Count("retried")
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		case <-next:
			timer.Stop()
		case <-timer.C:
		}
	}
	r.Count("fallback")
	return r.Fallback()
}

type ReadView[S any] struct {
	Fork    func(S, context.Context) (S, error)
	Open    func(S, context.Context)
	Close   func(S)
	Failure func(S) error
	Refusal func(S) *kernel.Error
}

func ReadInView[S, T any](v ReadView[S], state S, ctx context.Context, read func(S) (T, *kernel.Error)) (value T, refusal *kernel.Error, failure error) {
	view, err := v.Fork(state, ctx)
	if err != nil {
		return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read scope could not be prepared: {reason}", err.Error()), err
	}
	defer func() {
		v.Close(view)
		if err := v.Failure(view); err != nil {
			var zero T
			value, refusal, failure = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}, err
		}
	}()
	v.Open(view, ctx)
	if refusal := v.Refusal(view); refusal != nil {
		return value, refusal, v.Failure(view)
	}
	value, refusal = read(view)
	return value, refusal, nil
}

func RecoverImageRead[T any](quarantine func(error), value *T, refusal **kernel.Error, failure *error) {
	recovered := recover()
	if recovered == nil {
		return
	}
	err, ok := recovered.(error)
	if !ok {
		panic(recovered)
	}
	*failure = err
	var mismatch *persistence.StatePositionError
	if errors.As(err, &mismatch) && mismatch.Position < mismatch.Applied {
		quarantine(err)
	}
	var zero T
	*value, *refusal = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
}

func RecoverLockedRead[T any](record, quarantine func(error), value *T, refusal **kernel.Error) {
	recovered := recover()
	if recovered == nil {
		return
	}
	err, ok := recovered.(error)
	if !ok {
		panic(recovered)
	}
	record(err)
	var mismatch *persistence.StatePositionError
	if errors.As(err, &mismatch) {
		quarantine(err)
	}
	var zero T
	*value, *refusal = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
}
