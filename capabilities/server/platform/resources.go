package platform

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Resources owns only live registrations and handles. Durable business writes
// and external emissions never enter this cleanup stack.
type Resources struct {
	mu        sync.Mutex
	closing   bool
	users     int
	idle      chan struct{}
	cleanups  []resourceCleanup
	closeGate chan struct{}
	failure   string
}

type ResourceState struct {
	State    string `json:"state"`
	InFlight int    `json:"inFlight"`
	Pending  int    `json:"pending"`
	Error    string `json:"error,omitempty"`
}

func (s *Resources) State() ResourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := ResourceState{State: "ready", InFlight: s.users, Pending: len(s.cleanups), Error: s.failure}
	if s.closing {
		state.State = "draining"
		if s.users == 0 && len(s.cleanups) == 0 {
			state.State = "closed"
		}
	}
	if s.failure != "" {
		state.State = "cleanup-failed"
	}
	return state
}

// ResourceLease lets an acquisition already in flight register its inverse
// during draining. Closing waits for Release before consuming the stack.
type ResourceLease struct {
	scope    *Resources
	mu       sync.Mutex
	released bool
}

func (s *Resources) Begin() (*ResourceLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, fmt.Errorf("resource scope is draining")
	}
	if s.users == 0 {
		s.idle = make(chan struct{})
	}
	s.users++
	return &ResourceLease{scope: s}, nil
}

func (l *ResourceLease) Track(name string, undo func(context.Context) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released || undo == nil {
		return fmt.Errorf("resource acquisition has ended")
	}
	l.scope.mu.Lock()
	defer l.scope.mu.Unlock()
	l.scope.cleanups = append(l.scope.cleanups, resourceCleanup{name, undo})
	return nil
}

func (l *ResourceLease) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return
	}
	l.released = true
	l.scope.mu.Lock()
	defer l.scope.mu.Unlock()
	l.scope.users--
	if l.scope.users == 0 {
		close(l.scope.idle)
	}
}

type resourceCleanup struct {
	name string
	undo func(context.Context) error
}

// Acquire pins an active resource scope until its returned release is called.
func (s *Resources) Acquire() (func(), error) {
	l, err := s.Begin()
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// Track registers the inverse at acquisition time. It never snapshots shared state.
func (s *Resources) Track(name string, undo func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || undo == nil {
		return fmt.Errorf("cannot register resource %s on an inactive scope", name)
	}
	s.cleanups = append(s.cleanups, resourceCleanup{name, undo})
	return nil
}

// Close stops new acquisitions, drains users, and waits for inverses in reverse
// acquisition order. A failed inverse and its dependencies remain for retry.
func (s *Resources) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	if s.closeGate == nil {
		s.closeGate = make(chan struct{}, 1)
	}
	gate := s.closeGate
	users, idle := s.users, s.idle
	s.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if users != 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if len(s.cleanups) == 0 {
			s.mu.Unlock()
			return nil
		}
		last := s.cleanups[len(s.cleanups)-1]
		s.mu.Unlock()
		if err := last.undo(ctx); err != nil {
			failure := errors.Join(fmt.Errorf("resource %s did not close", last.name), err)
			s.mu.Lock()
			s.failure = failure.Error()
			s.mu.Unlock()
			return failure
		}
		s.mu.Lock()
		s.cleanups = s.cleanups[:len(s.cleanups)-1]
		s.failure = ""
		s.mu.Unlock()
	}
}
