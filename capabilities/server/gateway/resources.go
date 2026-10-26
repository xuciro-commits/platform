package gateway

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// References include opened sessions and calls preparing a new session. A
// failing preview cannot terminate a process another connection is using.
func (s *Server) releaseProcess(ctx context.Context, binding platform.DriverBinding, client rpcClient) error {
	s.mu.Lock()
	if s.processRefs[client] > 1 {
		s.processRefs[client]--
		s.mu.Unlock()
		return nil
	}
	s.processRefs[client] = 0
	client.Close()
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.CloseWait(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processes[processKey(binding)] == client {
		delete(s.processes, processKey(binding))
	}
	delete(s.processRefs, client)
	return nil
}

func (s *Server) stopSampling(ctx context.Context, live *session) error {
	live.mu.Lock()
	if live.stop != nil {
		live.stop()
	}
	done := live.done
	live.sampling, live.stop = false, nil
	if live.preview != nil {
		live.preview.Stop()
	}
	live.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Server) closeSession(ctx context.Context, live *session) error {
	// Seal admission before stopping a sampler so a concurrent start cannot
	// create a new loop after the old one has been joined.
	return live.resources.Close(ctx)
}

func (s *Server) closeConnection(ctx context.Context, live *session) error {
	if err := s.stopSampling(ctx, live); err != nil {
		return err
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.client == nil {
		return nil
	}
	if live.client.Available() {
		if err := live.client.Call(ctx, "close", platform.DriverSession{Session: live.open.Session}, nil); err != nil {
			return err
		}
	}
	if err := s.releaseProcess(ctx, live.binding, live.client); err != nil {
		return err
	}
	live.client, live.opened = nil, false
	return nil
}

func (s *Server) closeSessions(ctx context.Context) error {
	s.mu.Lock()
	keys := make([]string, 0, len(s.sessions))
	for key := range s.sessions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	lives := make([]*session, 0, len(keys))
	for _, key := range keys {
		lives = append(lives, s.sessions[key])
	}
	s.mu.Unlock()
	var failures []error
	for _, live := range lives {
		if err := s.closeSession(ctx, live); err != nil {
			failures = append(failures, err)
		}
	}
	if err := s.closeUnusedProcesses(ctx, ""); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (s *Server) closeUnusedProcesses(ctx context.Context, packageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, client := range s.processes {
		if packageID != "" && !strings.HasPrefix(key, packageID+"@") {
			continue
		}
		if s.processRefs[client] != 0 {
			continue
		}
		if err := client.CloseWait(ctx); err != nil {
			return err
		}
		delete(s.processes, key)
		delete(s.processRefs, client)
	}
	return nil
}
