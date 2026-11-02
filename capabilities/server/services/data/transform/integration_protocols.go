package integrations

import (
	"platformserver/platform"
	"sync"
	"time"
)

const (
	BreakerAfter = 5
	BreakerPause = 30 * time.Second
	BreakerMax   = 10 * time.Minute
)

type breaker struct {
	failures int
	until    time.Time
	probing  bool
	pause    time.Duration
}

type Breakers struct {
	mu sync.Mutex
	m  map[string]*breaker
}

func (b *Breakers) Allow(dest string, now time.Time, claim bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	x := b.m[dest]
	switch {
	case x == nil || x.until.IsZero():
		return true
	case x.probing || now.Before(x.until):
		return false
	}
	x.probing = claim
	return true
}

func (b *Breakers) Report(dest string, ok bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]*breaker{}
	}
	x := b.m[dest]
	if x == nil {
		x = &breaker{}
		b.m[dest] = x
	}
	if ok {
		*x = breaker{}
		return
	}
	x.failures++
	if x.probing || x.failures >= BreakerAfter {
		x.pause = min(max(x.pause*2, BreakerPause), BreakerMax)
		if !x.probing {
			x.pause = BreakerPause
		}
		x.until, x.probing = now.Add(x.pause), false
	}
}

var ioLane = make(chan struct{}, 16)

func OnLane(jobs []func()) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		ioLane <- struct{}{}
		go func() {
			defer func() { <-ioLane; wg.Done() }()
			job()
		}()
	}
	wg.Wait()
}

func (b *Breakers) View(now time.Time) []platform.Breaker {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []platform.Breaker{}
	for dest, x := range b.m {
		state := "closed"
		switch {
		case x.probing:
			state = "probing"
		case !x.until.IsZero() && now.Before(x.until):
			state = "open"
		case !x.until.IsZero():
			state = "probing"
		}
		if x.failures > 0 {
			out = append(out, platform.Breaker{Destination: dest, State: state, Failures: x.failures, Until: x.until})
		}
	}
	return out
}
