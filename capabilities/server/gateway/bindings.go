package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"platformserver/platform"
)

type bindingState struct {
	Connection string                            `json:"connection"`
	Active     string                            `json:"active"`
	Revisions  map[string]platform.DriverBinding `json:"revisions"`
	Draining   map[string]bool                   `json:"draining,omitempty"`
	Released   map[string]bool                   `json:"released,omitempty"`
}

func bindingFile(connection string) string {
	sum := sha256.Sum256([]byte(connection))
	return hex.EncodeToString(sum[:]) + ".json"
}

// Binding configuration is gateway state for the original Connection revision.
// Loading opens no device connection and executes no queued command.
func (s *Server) loadBindings() error {
	entries, err := os.ReadDir(filepath.Join(s.config.StateDir, "bindings"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.config.StateDir, "bindings", entry.Name()))
		if err != nil {
			return err
		}
		var state bindingState
		if json.Unmarshal(raw, &state) != nil || !identity(state.Connection) || bindingFile(state.Connection) != entry.Name() {
			return fmt.Errorf("stored protocol binding is invalid")
		}
		for revision, binding := range state.Revisions {
			if revision != binding.Revision() {
				return fmt.Errorf("stored binding revision differs from its configuration")
			}
			live, err := s.newSession(state.Connection, revision, binding)
			if err != nil {
				return err
			}
			live.draining, live.released = state.Draining[revision], state.Released[revision]
			if live.released {
				if err = live.resources.Close(s.ctx); err != nil {
					return err
				}
			}
			s.sessions[sessionKey(state.Connection, revision)] = live
		}
		if state.Active != "" {
			if _, ok := state.Revisions[state.Active]; !ok {
				return fmt.Errorf("stored active binding revision is absent")
			}
			s.active[state.Connection] = state.Active
		}
	}
	return nil
}

// Caller holds s.mu. Persist active and all revisions together so a restart
// cannot accidentally make an older frozen binding writable.
func (s *Server) persistBinding(connection, active string) error {
	state := bindingState{Connection: connection, Active: active, Revisions: map[string]platform.DriverBinding{}, Draining: map[string]bool{}, Released: map[string]bool{}}
	for _, live := range s.sessions {
		if live.open.Connection == connection {
			state.Revisions[live.open.Revision] = live.binding
			if live.draining {
				state.Draining[live.open.Revision] = true
			}
			if live.released {
				state.Released[live.open.Revision] = true
			}
		}
	}
	return writeState(filepath.Join(s.config.StateDir, "bindings", bindingFile(connection)), state)
}

func (s *Server) newSession(connection, revision string, binding platform.DriverBinding) (*session, error) {
	if err := binding.Check(); err != nil {
		return nil, err
	}
	runtime, err := s.config.Resolve(binding)
	if err != nil {
		return nil, err
	}
	for _, profile := range runtime.Profiles {
		if profile.ID == binding.Profile {
			live := &session{binding: binding, profile: profile, open: platform.DriverOpen{Session: sessionKey(connection, revision), Connection: connection, Revision: revision, Profile: binding.Profile, Configuration: binding.Configuration}, latest: map[string]platform.DriverSample{}}
			_ = live.resources.Track("driver connection", func(ctx context.Context) error { return s.closeConnection(ctx, live) })
			return live, nil
		}
	}
	return nil, fmt.Errorf("protocol profile is unavailable")
}

func (s *Server) discardPreview(connection, revision string, live *session) {
	s.mu.Lock()
	if s.sessions[sessionKey(connection, revision)] != live {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.closeSession(ctx, live); err == nil {
		s.mu.Lock()
		if s.sessions[sessionKey(connection, revision)] == live {
			delete(s.sessions, sessionKey(connection, revision))
		}
		s.mu.Unlock()
	}
}
