package kernel

import (
	"context"
	"log"
	"sync"
	"time"

	persistence "platformserver/kernel/store"
)

type SnapshotSaver struct {
	Journal persistence.Journals
	Every   int64
	mu      sync.Mutex
	saved   map[string]int64
}

// SnapshotSource.Capture returns the state, its position, an optional exported SQL snapshot of that
// position (empty when there is none) and a release the saver calls once the checkpoint is saved.
type SnapshotSource struct {
	ID          string
	Quarantined func() bool
	Capture     func() ([]byte, int64, string, func(), error)
	Key         func(state []byte) (string, error)
}

func NewSnapshotSaver(journal persistence.Journals, every int64) *SnapshotSaver {
	return &SnapshotSaver{Journal: journal, Every: every, saved: map[string]int64{}}
}

func (s *SnapshotSaver) Note(id string, position int64) {
	s.mu.Lock()
	s.saved[id] = position
	s.mu.Unlock()
}

func (s *SnapshotSaver) Save(ctx context.Context, t SnapshotSource, final bool) {
	if t.Quarantined() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grown := s.Journal.Position(t.ID) - s.saved[t.ID]
	if grown == 0 || !final && (grown < s.Every || grown < s.saved[t.ID]/10) {
		return
	}
	started := time.Now()
	state, seq, pinned, release, err := t.Capture()
	if release == nil {
		release = func() {}
	}
	defer release()
	if t.Quarantined() {
		return
	}
	if err == nil {
		var key string
		key, err = t.Key(state)
		if err == nil {
			err = SaveCheckpoint(persistence.WithSnapshot(ctx, pinned), s.Journal, t.ID, seq, key, state)
		}
	}
	if err != nil {
		log.Printf("snapshot %s: %v", t.ID, err)
		return
	}
	s.saved[t.ID] = seq
	log.Printf("saved a snapshot of %s at %d (%d kB) in %v", t.ID, seq, len(state)/1024, time.Since(started).Round(time.Millisecond))
}
