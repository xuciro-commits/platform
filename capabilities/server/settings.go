package platformserver

import (
	"maps"
	"sync"
)

// settingValues are the apps' settings as set in this tenant, "<app>/<name>" →
// value (declared defaults apply where none is set). Own lock: settings are
// read by every decision and the runner, and set by one console action.
type settingValues struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *settingValues) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}

func (s *settingValues) set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
}

func (s *settingValues) clone() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.values)
	if out == nil {
		out = map[string]string{}
	}
	return out
}

func (s *settingValues) restore(values map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = values
	if s.values == nil {
		s.values = map[string]string{}
	}
}
