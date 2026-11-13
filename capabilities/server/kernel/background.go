package kernel

import (
	"context"
	"sync"
	"time"
)

func Every(ctx context.Context, background *sync.WaitGroup, interval time.Duration, tick func() bool) {
	background.Add(1)
	go func() {
		defer background.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !tick() {
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
