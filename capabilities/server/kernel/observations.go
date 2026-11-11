package kernel

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

const observationPendingLimit = 2000

type ObservationBuffer struct {
	mu         sync.Mutex
	pending    []platform.Observation
	usageDay   time.Time
	usageBytes int64
	dropped    atomic.Int64
	written    atomic.Int64
	failed     atomic.Int64
	purged     atomic.Int64
	lastWrite  atomic.Int64
	lastPurge  atomic.Int64
	purgeMu    sync.Mutex
	purgeRun   time.Time
}

type ObservationCounts struct {
	Pending       int
	Dropped       int64
	Written       int64
	WriteFailures int64
	Purged        int64
	LastWrite     time.Time
	LastPurge     time.Time
}

func ObservationLevelRank(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	}
	return -1
}

func (b *ObservationBuffer) Record(store persistence.Store, event platform.Observation, quotaBytes int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var usageErr error
	day := event.At.Truncate(24 * time.Hour)
	if !b.usageDay.Equal(day) {
		usage, err := store.ObservationUsage(event.Tenant, day)
		usageErr = err
		b.usageDay, b.usageBytes = day, usage
	}
	raw, _ := json.Marshal(event)
	if b.usageBytes+int64(len(raw)) > quotaBytes && ObservationLevelRank(event.Level) < ObservationLevelRank("warn") {
		b.dropped.Add(1)
		return usageErr
	}
	b.usageBytes += int64(len(raw))
	if len(b.pending) >= observationPendingLimit {
		drop := -1
		for index, pending := range b.pending {
			if pending.Level == "debug" || pending.Level == "info" {
				drop = index
				break
			}
		}
		if drop < 0 && ObservationLevelRank(event.Level) < ObservationLevelRank("error") {
			b.dropped.Add(1)
			return usageErr
		}
		if drop >= 0 {
			b.pending = append(b.pending[:drop], b.pending[drop+1:]...)
		} else {
			b.pending = b.pending[1:]
		}
		b.dropped.Add(1)
	}
	b.pending = append(b.pending, event)
	return usageErr
}

func (b *ObservationBuffer) Flush(store persistence.Store, now time.Time) error {
	b.mu.Lock()
	events := b.pending
	b.pending = nil
	b.mu.Unlock()
	if len(events) == 0 {
		return nil
	}
	if err := store.SaveObservations(events); err != nil {
		b.failed.Add(1)
		b.mu.Lock()
		if lost := len(events) + len(b.pending) - observationPendingLimit; lost > 0 {
			b.dropped.Add(int64(lost))
		}
		kept := append(events, b.pending...)
		if len(kept) > observationPendingLimit {
			kept = kept[len(kept)-observationPendingLimit:]
		}
		b.pending = kept
		b.mu.Unlock()
		return err
	}
	b.written.Add(int64(len(events)))
	b.lastWrite.Store(now.UnixNano())
	return nil
}

func (b *ObservationBuffer) Purge(store persistence.Store, tenant string, now, before time.Time) (int, error) {
	b.purgeMu.Lock()
	if !b.purgeRun.IsZero() && now.Sub(b.purgeRun) < time.Hour {
		b.purgeMu.Unlock()
		return 0, nil
	}
	b.purgeRun = now
	b.purgeMu.Unlock()
	removed, err := store.PurgeObservations(tenant, before)
	if err != nil {
		return 0, err
	}
	b.lastPurge.Store(now.UTC().UnixNano())
	b.purged.Add(int64(removed))
	return removed, nil
}

func (b *ObservationBuffer) Dropped() int64 { return b.dropped.Load() }

func (b *ObservationBuffer) Counts() ObservationCounts {
	b.mu.Lock()
	defer b.mu.Unlock()
	counts := ObservationCounts{Pending: len(b.pending), Dropped: b.dropped.Load(), Written: b.written.Load(),
		WriteFailures: b.failed.Load(), Purged: b.purged.Load()}
	if last := b.lastWrite.Load(); last > 0 {
		counts.LastWrite = time.Unix(0, last).UTC()
	}
	if last := b.lastPurge.Load(); last > 0 {
		counts.LastPurge = time.Unix(0, last).UTC()
	}
	return counts
}

func (b *ObservationBuffer) CopyCounts(from *ObservationBuffer) {
	b.dropped.Store(from.dropped.Load())
	b.written.Store(from.written.Load())
	b.failed.Store(from.failed.Load())
	b.purged.Store(from.purged.Load())
	b.lastWrite.Store(from.lastWrite.Load())
	b.lastPurge.Store(from.lastPurge.Load())
}
