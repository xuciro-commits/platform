package host

import (
	"context"
	"platformkernel/kernel"
)

type readAnswer[T any] struct {
	value T
	err   error
}

func readWithError[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, error)) (T, error) {
	result, refusal := recordRead(t, ctx, func(scope *Tenant) (readAnswer[T], *kernel.Error) {
		value, err := read(scope)
		return readAnswer[T]{value: value, err: err}, nil
	})
	if refusal != nil {
		return result.value, refusal
	}
	return result.value, result.err
}
