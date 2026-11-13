package kernel

import (
	"context"
	"errors"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	persistence "platformserver/kernel/store"
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
