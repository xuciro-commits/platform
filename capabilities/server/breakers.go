package platformserver

import (
	"sync"
	"time"
)

// Breakers per destination (ADR-0027 D4): an endpoint or an AI provider that
// failed several times in a row opens its breaker; while it is open its work
// waits instead of spending attempts; after a pause one attempt probes it, and
// a success closes it. They are volatile: what was attempted is journaled.
const (
	breakerAfter = 5                // failures in a row that open a breaker
	breakerPause = 30 * time.Second // the first pause; it doubles while probes fail
	breakerMax   = 10 * time.Minute
)

type breaker struct {
	failures int
	until    time.Time // open until then; zero: closed
	probing  bool
	pause    time.Duration
}

type breakers struct {
	mu sync.Mutex
	m  map[string]*breaker
}

// allow reports whether work for dest may go now: closed, or open and past its
// pause, which lets one probe through.
func (b *breakers) allow(dest string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	x := b.m[dest]
	switch {
	case x == nil || x.until.IsZero():
		return true
	case x.probing || now.Before(x.until):
		return false
	}
	x.probing = true
	return true
}

// report records how an attempt for dest ended.
func (b *breakers) report(dest string, ok bool, now time.Time) {
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
	if x.probing || x.failures >= breakerAfter {
		x.pause = min(max(x.pause*2, breakerPause), breakerMax)
		if !x.probing {
			x.pause = breakerPause
		}
		x.until, x.probing = now.Add(x.pause), false
	}
}

// Breaker is a destination's breaker as Settings shows it.
type Breaker struct {
	Destination string    `json:"destination"` // endpoint:<id> or ai:<provider>
	State       string    `json:"state"`       // closed, open or probing
	Failures    int       `json:"failures"`
	Until       time.Time `json:"until,omitzero"`
}

// Breakers are the tenant's destinations that failed lately.
func (t *Tenant) Breakers(now time.Time) []Breaker {
	b := &t.breakers
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Breaker{}
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
			out = append(out, Breaker{Destination: dest, State: state, Failures: x.failures, Until: x.until})
		}
	}
	return out
}

// ioLane bounds the work waiting on the outside at once across the process:
// effects, model calls and embeddings run on it, outside every tenant's lock,
// so one slow destination holds one slot, not the process (ADR-0027 D2).
var ioLane = make(chan struct{}, 16)

// onLane runs each job on the lane and waits for all of them.
func onLane(jobs []func()) {
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
