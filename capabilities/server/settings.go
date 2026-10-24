package platformserver

import (
	"maps"
	"sync"
	"sync/atomic"
)

// settingValues are the apps' settings as set in this tenant, "<app>/<name>" →
// value (declared defaults apply where none is set). Reads are lock-free: a
// request asks for a dozen settings, and settings change once in a while, so a
// write replaces the whole map instead of blocking the readers.
type settingValues struct {
	mu     sync.Mutex // serializes writers
	values atomic.Pointer[map[string]string]
}

func (s *settingValues) get(key string) (string, bool) {
	if values := s.values.Load(); values != nil {
		v, ok := (*values)[key]
		return v, ok
	}
	return "", false
}

func (s *settingValues) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]string{}
	if current := s.values.Load(); current != nil {
		next = maps.Clone(*current)
	}
	next[key] = value
	s.values.Store(&next)
}

func (s *settingValues) clone() map[string]string {
	if current := s.values.Load(); current != nil {
		return maps.Clone(*current)
	}
	return map[string]string{}
}

func (s *settingValues) restore(values map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if values == nil {
		values = map[string]string{}
	}
	s.values.Store(&values)
}
