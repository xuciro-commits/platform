package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"time"
)

func Every(ctx context.Context, background *sync.WaitGroup, interval time.Duration, tick func() bool) {
	Pulse(ctx, background, interval, nil, tick)
}

func Pulse(ctx context.Context, background *sync.WaitGroup, interval time.Duration, wake <-chan struct{}, tick func() bool) {
	background.Add(1)
	go func() {
		defer background.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-ticker.C:
			}
			if ctx.Err() != nil || !tick() {
				return
			}
		}
	}()
}

func Drain(cancel func(), background *sync.WaitGroup) func(context.Context) error {
	return func(closing context.Context) error {
		cancel()
		done := make(chan struct{})
		go func() { background.Wait(); close(done) }()
		select {
		case <-done:
			return nil
		case <-closing.Done():
			return closing.Err()
		}
	}
}

type Wake chan struct{}

func NewWake() Wake { return make(Wake, 1) }

func (w Wake) Signal() {
	select {
	case w <- struct{}{}:
	default:
	}
}

func Rotate[T any](items []T, within time.Duration, step func(T) bool) {
	started := time.Now()
	for more := true; more && time.Since(started) < within; {
		more = false
		for _, item := range items {
			if step(item) {
				more = true
			}
		}
	}
}

func CodeDigest(parts ...string) string {
	h := sha256.New()
	if exe, err := os.Executable(); err == nil {
		if f, err := os.Open(exe); err == nil {
			io.Copy(h, f)
			f.Close()
		}
	}
	for _, part := range parts {
		io.WriteString(h, part)
	}
	return hex.EncodeToString(h.Sum(nil))
}
