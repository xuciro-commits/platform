package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"platformserver/platform"
)

// The caller holds s.mu. Enumerating the receipt owner prevents a restart from
// hiding an unknown command merely because nobody has looked its key up yet.
func (s *Server) pendingAttempts(connection, revision string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.config.StateDir, "receipts"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var blockers []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !platform.DriverDigest(id) || !entry.Type().IsRegular() {
			return nil, fmt.Errorf("the retained receipt directory is incomplete")
		}
		receipt, _, err := s.loadReceipt(id)
		if err != nil {
			return nil, err
		}
		if receipt.Connection == connection && receipt.Revision == revision && receipt.Answer.State == "unknown" {
			blockers = append(blockers, receipt.Dispatch.RunID+"/"+receipt.Dispatch.AttemptID)
		}
	}
	slices.Sort(blockers)
	return blockers, nil
}

func (s *Server) sessionStatus(connection, revision string, live *session) (platform.DriverLifecycle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blockers, err := s.pendingAttempts(connection, revision)
	status := platform.DriverLifecycle{ResourceState: live.resources.State(), Connection: connection, Revision: revision, Draining: live.draining, Released: live.released, Blockers: blockers}
	if live.draining && len(blockers) > 0 {
		status.State = "blocked"
	}
	return status, err
}

func (s *Server) releaseSession(ctx context.Context, connection, revision string, live *session) (platform.DriverLifecycle, error) {
	s.mu.Lock()
	prior := s.active[connection]
	priorDraining := live.draining
	live.draining = true
	active := prior
	if active == revision {
		active = ""
	}
	if err := s.persistBinding(connection, active); err != nil {
		live.draining = priorDraining
		s.mu.Unlock()
		return platform.DriverLifecycle{}, err
	}
	s.active[connection] = active
	s.mu.Unlock()
	if err := s.stopSampling(ctx, live); err != nil {
		return platform.DriverLifecycle{}, err
	}
	status, err := s.sessionStatus(connection, revision, live)
	if err != nil || len(status.Blockers) > 0 {
		return status, err
	}
	if err = s.closeSession(ctx, live); err != nil {
		return platform.DriverLifecycle{}, err
	}
	if err = s.closeUnusedProcesses(ctx, live.binding.Package); err != nil {
		return platform.DriverLifecycle{}, err
	}
	s.mu.Lock()
	live.released = true
	err = s.persistBinding(connection, s.active[connection])
	s.mu.Unlock()
	if err != nil {
		return platform.DriverLifecycle{}, err
	}
	return s.sessionStatus(connection, revision, live)
}
