package kernel

import (
	"context"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type ImageDecision[T, C any] struct {
	Stopped     func() bool
	Quarantined func() bool
	Hold        func() error
	Unlock      func()
	Unavailable func() bool
	Load        func() (read func() (T, C, *kernel.Error, error), matches func(C) bool)
	Direct      func() (T, C, *kernel.Error, error)
	Finish      func(C, T, *kernel.Error) (T, *kernel.Error)
	Count       func(outcome string)
}

func (r ImageDecision[T, C]) Run(ctx context.Context) (value T, refusal *kernel.Error) {
	conflict := func() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT} }
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil || r.Stopped() {
			return value, conflict()
		}
		read, matches := r.Load()
		if read == nil {
			break
		}
		value, candidate, refusal, err := read()
		if err != nil {
			if ctx.Err() != nil || r.Quarantined() {
				return value, refusal
			}
			break
		}
		if err := r.Hold(); err != nil {
			return value, conflict()
		}
		if r.Unavailable() {
			r.Unlock()
			return value, conflict()
		}
		if matches(candidate) {
			func() {
				defer r.Unlock()
				value, refusal = r.Finish(candidate, value, refusal)
			}()
			return value, refusal
		}
		r.Unlock()
		if attempt < 2 {
			r.Count("retried")
		}
	}
	r.Count("fallback")
	if err := r.Hold(); err != nil {
		return value, conflict()
	}
	defer r.Unlock()
	if r.Unavailable() {
		return value, conflict()
	}
	value, candidate, refusal, err := r.Direct()
	if err != nil {
		return value, refusal
	}
	return r.Finish(candidate, value, refusal)
}
