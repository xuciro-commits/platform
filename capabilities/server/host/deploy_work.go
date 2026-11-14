package host

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	runtimekernel "platformserver/kernel"
	"platformserver/services/data/transform"
)

const roundSize = 10

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
	var parts []string
	for _, t := range tenants {
		for _, a := range t.apps {
			parts = append(parts, fmt.Sprintf("%s %s@%s\n", t.ID, a.Manifest().ID, a.Manifest().Version))
		}
	}
	return runtimekernel.CodeDigest(parts...)
}

var workReady = runtimekernel.NewWake()

func wakeWork() { workReady.Signal() }

var tenantTicks = []func(*Tenant, time.Time){func(t *Tenant, _ time.Time) { t.FlushObservations() }, (*Tenant).PurgeObservations}

var activeTicks = []func(*Tenant, time.Time){(*Tenant).Evaluate, (*Tenant).PullSources, (*Tenant).CheckConnections, (*Tenant).RunPipelines,
	(*Tenant).ProjectDatasets, (*Tenant).Embed, (*Tenant).PurgeTranscripts, (*Tenant).SweepUploads}

func runWorkFrom(current func() []*Tenant) func(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	var background sync.WaitGroup
	runtimekernel.Pulse(ctx, &background, time.Second, workReady, func() bool {
		tenants, now := current(), Now()
		runtimekernel.Rotate(tenants, 900*time.Millisecond, func(t *Tenant) bool { return t.Round(now, roundSize) })
		var outside []func()
		for _, t := range tenants {
			outside = append(append(outside, t.dispatches(Now())...), t.agentSchedule().Turns().Due(Now())...)
			outside = append(outside, t.continuousSources(Now())...)
		}
		background.Add(1)
		go func() { defer background.Done(); integrations.OnLane(outside) }()
		return true
	})
	runtimekernel.Every(ctx, &background, 5*time.Second, func() bool {
		for _, t := range current() {
			for _, tick := range tenantTicks {
				tick(t, Now())
			}
			if t.businessStopped() {
				continue
			}
			for _, tick := range activeTicks {
				tick(t, Now())
			}
		}
		return true
	})
	return runtimekernel.Drain(cancel, &background)
}
