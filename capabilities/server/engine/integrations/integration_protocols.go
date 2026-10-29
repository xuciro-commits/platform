package integrations

import (
	"platformserver/platform"
	"sync"
	"time"
)

// Breakers per destination: an endpoint or an AI provider that
// failed several times in a row opens its breaker; while it is open its work
// waits instead of spending attempts; after a pause one attempt probes it, and
// a success closes it. They are volatile: what was attempted is journaled.
const (
	BreakerAfter = 5                // failures in a row that open a breaker
	BreakerPause = 30 * time.Second // the first pause; it doubles while probes fail
	BreakerMax   = 10 * time.Minute
)

type breaker struct {
	failures int
	until    time.Time // open until then; zero: closed
	probing  bool
	pause    time.Duration
}

type Breakers struct {
	mu sync.Mutex
	m  map[string]*breaker
}

// allow reports whether work for dest may go now: closed, or open and past its
// pause. claim takes the one half-open probe; a readiness check does not.
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

// report records how an attempt for dest ended.
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

// ioLane bounds the work waiting on the outside at once across the process:
// effects, model calls and embeddings run on it, outside every tenant's lock,
// so one slow destination holds one slot, not the process.
var ioLane = make(chan struct{}, 16)

// onLane runs each job on the lane and waits for all of them.
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

// Breakers are the tenant's destinations that failed lately.
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
