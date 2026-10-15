package gateway

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"platformserver/platform"
)

func (s *Server) startSampling(ctx context.Context, live *session) error {
	if !slices.Contains(live.profile.Operations, "sample") {
		return nil
	}
	live.mu.Lock()
	running := live.sampling
	live.mu.Unlock()
	if running {
		return nil
	}
	if err := s.call(ctx, live, "start", platform.DriverSession{Session: live.open.Session}, nil); err != nil {
		return err
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.sampling {
		return nil
	}
	sampling, cancel := context.WithCancel(s.ctx)
	live.stop, live.sampling = cancel, true
	go s.sample(sampling, live)
	return nil
}

// The gateway owns the only periodic read loop. Its sink is external; the map
// in session is a bounded latest-value cache, not observation history.
func (s *Server) sample(ctx context.Context, live *session) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	next := map[string]time.Time{}
	for _, point := range live.binding.Configuration.Points {
		next[point.ID] = time.Now().Add(time.Duration(point.IntervalMS) * time.Millisecond)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			var due []string
			for _, point := range live.binding.Configuration.Points {
				if !at.Before(next[point.ID]) {
					due = append(due, point.ID)
					next[point.ID] = at.Add(time.Duration(point.IntervalMS) * time.Millisecond)
				}
			}
			if len(due) == 0 {
				continue
			}
			readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			result, err := s.read(readCtx, live, platform.DriverRead{Session: live.open.Session, Signals: due, Mode: "latest"})
			cancel()
			if err != nil {
				continue
			}
			if s.config.Samples != nil {
				s.sampleMu.Lock()
				encoder := json.NewEncoder(s.config.Samples)
				for _, sample := range result.Samples {
					_ = encoder.Encode(sample)
				}
				s.sampleMu.Unlock()
			}
		}
	}
}
