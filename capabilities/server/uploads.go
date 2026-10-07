package platformserver

import (
	"sync"
	"time"
)

// uploads are the file hashes uploaded and when, until a record attaches them
// or the sweep removes them (volatile, ADR-0028 D1).
type uploads struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (u *uploads) add(hash string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.at == nil {
		u.at = map[string]time.Time{}
	}
	u.at[hash] = now
}

// olderThan are the hashes uploaded before now minus age.
func (u *uploads) olderThan(now time.Time, age time.Duration) []string {
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

func (u *uploads) forget(hash string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.at, hash)
}
