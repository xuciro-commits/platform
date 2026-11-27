package kernel

import (
	"context"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type ImageDecision[T, C any] struct {
	Attempts    int
	Stopped     func() bool
	Quarantined func() bool
	Hold        func() error
	Unlock      func()
	Contended   func() bool
	Unavailable func() bool
	Load        func() (read func() (T, C, *platform.Error, error), matches func(C) bool)
	Direct      func() (T, C, *platform.Error, error)
	Finish      func(C, T, *platform.Error) (T, *platform.Error)
	Count       func(outcome string)
}

func (r ImageDecision[T, C]) Run(ctx context.Context) (value T, refusal *platform.Error) {
	var zero T
	conflict := func() *platform.Error { return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT} }
	direct := func() (T, *platform.Error) {
		r.Count("fallback")
		value, decision, refusal, err := r.Direct()
		if err != nil {
			return value, refusal
		}
		if ctx.Err() != nil {
			return zero, conflict()
		}
		return r.Finish(decision, value, refusal)
	}
	attempts := r.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil || r.Stopped() {
			return zero, conflict()
		}
		read, matches := r.Load()
		if read == nil {
			break
		}
		value, decision, refusal, err := read()
		if err != nil {
			if ctx.Err() != nil || r.Quarantined() {
				return value, refusal
			}
			break
		}
		if err := r.Hold(); err != nil {
			return zero, conflict()
		}
		if ctx.Err() != nil || r.Unavailable() {
			r.Unlock()
			return zero, conflict()
		}
		if matches(decision) {
			func() {
				defer r.Unlock()
				value, refusal = r.Finish(decision, value, refusal)
			}()
			return value, refusal
		}
		if r.Contended() || (r.Attempts > 0 && attempt+1 == attempts) {
			defer r.Unlock()
			return direct()
		}
		r.Unlock()
		if attempt+1 < attempts {
			r.Count("retried")
		}
	}
	if err := r.Hold(); err != nil {
		return zero, conflict()
	}
	defer r.Unlock()
	if ctx.Err() != nil || r.Unavailable() {
		return zero, conflict()
	}
	return direct()
}
