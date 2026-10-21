// Package tenantstate owns small, independent, concurrency-safe pieces of a tenant.
// It contains no authorization or accepted-result decisions.
package tenantstate

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"
)

// Quota paces owned work per app and remembers the round-robin start.
type Quota struct {
	used map[string]usedMinute
	turn int
}
type usedMinute struct {
	minute time.Time
	n      int
}

func (q *Quota) Over(app string, limit int, now time.Time) bool {
	if limit <= 0 {
		return false
	}
	u := q.used[app]
	return u.minute.Equal(now.Truncate(time.Minute)) && u.n >= limit
}
func (q *Quota) Spend(app string, now time.Time) {
	if q.used == nil {
		q.used = map[string]usedMinute{}
	}
	u, minute := q.used[app], now.Truncate(time.Minute)
	if !u.minute.Equal(minute) {
		u = usedMinute{minute: minute}
	}
	u.n++
	q.used[app] = u
}
func (q *Quota) Turn(n int) int {
	if n < 1 {
		return 0
	}
	return q.turn % n
}
func (q *Quota) Next(n int) {
	if n < 1 {
		n = 1
	}
	q.turn = (q.turn + 1) % n
}

// Uploads remembers unattached uploaded hashes until attachment or sweeping.
type Uploads struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (u *Uploads) Add(hash string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.at == nil {
		u.at = map[string]time.Time{}
	}
	u.at[hash] = now
}
func (u *Uploads) OlderThan(now time.Time, age time.Duration) []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var old []string
	for hash, at := range u.at {
		if now.Sub(at) > age {
			old = append(old, hash)
		}
	}
	return old
}
func (u *Uploads) Forget(hash string) { u.mu.Lock(); defer u.mu.Unlock(); delete(u.at, hash) }

// Settings owns the tenant's explicit app setting values.
type Settings struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *Settings) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}
func (s *Settings) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
}
func (s *Settings) Clone() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.values)
	if out == nil {
		out = map[string]string{}
	}
	return out
}
func (s *Settings) Restore(values map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = values
	if s.values == nil {
		s.values = map[string]string{}
	}
}

// Cancels maps an executing effect to its transient cancellation handle.
type Cancels struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func (c *Cancels) Add(id string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		c.running = map[string]context.CancelFunc{}
	}
	c.running[id] = cancel
}
func (c *Cancels) Remove(id string) { c.mu.Lock(); defer c.mu.Unlock(); delete(c.running, id) }
func (c *Cancels) Cancel(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.running[id]; cancel != nil {
		cancel()
		delete(c.running, id)
	}
}

// Sequences owns accepted, replayable number counters by app/name/year.
type Sequences struct {
	mu   sync.Mutex
	last map[string]int
}

func (s *Sequences) Take(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	s.last[key]++
	return s.last[key]
}
func (s *Sequences) At(key string) int { s.mu.Lock(); defer s.mu.Unlock(); return s.last[key] }
func (s *Sequences) Clone() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.last)
	if out == nil {
		out = map[string]int{}
	}
	return out
}
func (s *Sequences) Check(bases, next map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, n := range next {
		if bases[key] != s.last[key] || n <= s.last[key] {
			return fmt.Errorf("record batch sequence %s does not advance", key)
		}
	}
	return nil
}
func (s *Sequences) Set(next map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	maps.Copy(s.last, next)
}
func (s *Sequences) Restore(last map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = last
	if s.last == nil {
		s.last = map[string]int{}
	}
}
