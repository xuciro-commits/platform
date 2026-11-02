package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	runtimekernel "platformserver/kernel"
	"platformserver/services/data/transform"
	"sync"
	"time"
)

const roundSize = 10

func Schedule(tenants []*Tenant, now time.Time, size int, within time.Duration) {
	started := time.Now()
	for more := true; more && time.Since(started) < within; {
		more = false
		for _, t := range tenants {
			if t.Round(now, size) {
				more = true
			}
		}
	}
}

type snapshotter struct {
	journal Journals
	code    string
	every   int64
	mu      sync.Mutex
	saved   map[string]int64
}

func (s *snapshotter) save(ctx context.Context, t *Tenant, final bool) {
	if t.quarantined() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grown := s.journal.Position(t.ID) - s.saved[t.ID]
	if grown == 0 || !final && (grown < s.every || grown < s.saved[t.ID]/10) {
		return
	}
	started := time.Now()
	state, seq, err := t.snapshotRuntime(ctx, s.journal)
	if t.quarantined() {
		return
	}
	if err == nil {
		var key string
		key, err = t.snapshotStorageKey(s.code, state)
		if err == nil {
			err = runtimekernel.SaveCheckpoint(ctx, s.journal, t.ID, seq, key, state)
		}
	}
	if err != nil {
		log.Printf("snapshot %s: %v", t.ID, err)
		return
	}
	s.saved[t.ID] = seq
	log.Printf("saved a snapshot of %s at %d (%d kB) in %v", t.ID, seq, len(state)/1024, time.Since(started).Round(time.Millisecond))
}

func CodeOf(tenants ...*Tenant) string {
	h := sha256.New()
	if exe, err := os.Executable(); err == nil {
		if f, err := os.Open(exe); err == nil {
			io.Copy(h, f)
			f.Close()
		}
	}
	for _, t := range tenants {
		for _, a := range t.apps {
			fmt.Fprintf(h, "%s %s@%s\n", t.ID, a.Manifest().ID, a.Manifest().Version)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

var workReady = make(chan struct{}, 1)

func wakeWork() {
	select {
	case workReady <- struct{}{}:
	default:
	}
}

func RunWork(tenants ...*Tenant) func(context.Context) error {
	return runWorkFrom(func() []*Tenant { return tenants })
}

func runWorkFrom(current func() []*Tenant) func(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	var loops, outsideRuns sync.WaitGroup
	loops.Add(2)
	go func() {
		defer loops.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-workReady:
			case <-ticker.C:
			}
			if ctx.Err() != nil {
				return
			}
			tenants := current()
			Schedule(tenants, Now(), roundSize, 900*time.Millisecond)
			var outside []func()
			for _, t := range tenants {
				outside = append(append(outside, t.dispatches(Now())...), t.turns(Now())...)
				outside = append(outside, t.continuousSources(Now())...)
			}
			outsideRuns.Add(1)
			go func() { defer outsideRuns.Done(); integrations.OnLane(outside) }()
		}
	}()
	go func() {
		defer loops.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			for _, t := range current() {
				if t.businessStopped() {
					continue
				}
				t.Evaluate(Now())
				t.PullSources(Now())
				t.CheckConnections(Now())
				t.RunPipelines(Now())
				t.ProjectDatasets(Now())
				t.Embed(Now())
				t.PurgeTranscripts(Now())
				t.SweepUploads(Now())
			}
		}
	}()
	done := make(chan struct{})
	var stop sync.Once
	return func(ctx context.Context) error {
		stop.Do(func() { cancel(); go func() { loops.Wait(); outsideRuns.Wait(); close(done) }() })
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
