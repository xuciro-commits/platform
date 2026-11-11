package data

import (
	"sync"
	"time"
)

const StatusReadLimit = 64

type ReadSchedules struct {
	Confirmations StatusReads
	Dispatches    StatusReads
	Windows       ObservationWindows
}

type StatusReads struct {
	mu       sync.Mutex
	attempts map[string]statusReadAttempt
}

type statusReadAttempt struct {
	Due  time.Time
	Busy bool
}

func (s *StatusReads) Retain(active map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, attempt := range s.attempts {
		if !active[key] && !attempt.Busy {
			delete(s.attempts, key)
		}
	}
}

func (s *StatusReads) Next(key string, saved time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next(key, saved)
}

func (s *StatusReads) next(key string, saved time.Time) time.Time {
	if cached := s.attempts[key].Due; cached.After(saved) {
		return cached
	}
	return saved
}

func (s *StatusReads) Start(key string, saved, at time.Time, reads *int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *reads >= StatusReadLimit || s.attempts[key].Busy || s.next(key, saved).After(at) {
		return false
	}
	if s.attempts == nil {
		s.attempts = map[string]statusReadAttempt{}
	}
	s.attempts[key] = statusReadAttempt{Busy: true}
	*reads++
	return true
}

func (s *StatusReads) Finished(key string, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[key] = statusReadAttempt{Due: next}
}

type ObservationWindows struct {
	mu       sync.Mutex
	attempts map[string]windowAttempt
}

type windowAttempt struct {
	Key  string
	Busy bool
}

func (w *ObservationWindows) Begin(slot, identity string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prior := w.attempts[slot]
	if prior.Busy || prior.Key == identity {
		return "", false
	}
	if w.attempts == nil {
		w.attempts = map[string]windowAttempt{}
	}
	w.attempts[slot] = windowAttempt{Key: prior.Key, Busy: true}
	return prior.Key, true
}

func (w *ObservationWindows) End(slot, key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attempts[slot] = windowAttempt{Key: key}
}
