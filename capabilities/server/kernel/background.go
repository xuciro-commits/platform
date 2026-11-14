package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"platformserver/platform"
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

type WorkLoop[T any] struct {
	Tenants func() []T
	Now     func() time.Time
	Wake    Wake
	Round   func(T, time.Time) bool
	Outside func(T) []func()
	OnLane  func([]func())
	Always  func(T)
	Stopped func(T) bool
	Active  func(T)
}

func (w WorkLoop[T]) Start() func(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	var background sync.WaitGroup
	Pulse(ctx, &background, time.Second, w.Wake, func() bool {
		tenants, now := w.Tenants(), w.Now()
		Rotate(tenants, 900*time.Millisecond, func(t T) bool { return w.Round(t, now) })
		var outside []func()
		for _, t := range tenants {
			outside = append(outside, w.Outside(t)...)
		}
		background.Add(1)
		go func() { defer background.Done(); w.OnLane(outside) }()
		return true
	})
	Every(ctx, &background, 5*time.Second, func() bool {
		for _, t := range w.Tenants() {
			w.Always(t)
			if w.Stopped(t) {
				continue
			}
			w.Active(t)
		}
		return true
	})
	return Drain(cancel, &background)
}

func CloseAll(resources *platform.Resources, err error) error {
	closing, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	return errors.Join(err, resources.Close(closing))
}
