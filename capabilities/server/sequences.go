package platformserver

import (
	"fmt"
	"maps"
	"sync"
)

// sequences are the number counters (ADR-0024 D3): the host's, keyed by
// "<app>/<sequence>/<year>", taken only in accepted decisions, rebuilt by
// replay and kept by snapshots. Their own lock: reads run inside other apps'
// submissions.
type sequences struct {
	mu   sync.Mutex
	last map[string]int
}

// take advances key and answers the number taken.
func (s *sequences) take(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	s.last[key]++
	return s.last[key]
}

// clone is the counters as they stand, for a staged decision to count on.
func (s *sequences) clone() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.last)
	if out == nil {
		out = map[string]int{}
	}
	return out
}

// check checks an accepted batch's counters against the bases it was
// decided on, then applies them: a batch cannot reuse or skip numbers.
func (s *sequences) check(bases, next map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, n := range next {
		if bases[key] != s.last[key] || n <= s.last[key] {
			return fmt.Errorf("record batch sequence %s does not advance", key)
		}
	}
	return nil
}

func (s *sequences) set(next map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	maps.Copy(s.last, next)
}

func (s *sequences) restore(last map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = last
	if s.last == nil {
		s.last = map[string]int{}
	}
}
