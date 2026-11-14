package kernel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"time"

	persistence "platformserver/kernel/store"
)

type TenantRestore struct {
	ID                     string
	Code                   string
	SnapshotEvery          int64
	Journal                persistence.Journals
	Quarantine             func(error)
	Fault                  func() string
	AuditEmpty             func() bool
	BackfillAudit          func([]persistence.Entry)
	Compatibility          func() (SnapshotCompatibility, error)
	Build                  func() string
	RestoreRuntimeSnapshot func(state []byte, seq, statePosition int64, started time.Time) error
	CheckpointRuntime      func() (int64, error)
	RecoverSnapshot        func(state []byte, seq int64) error
	RecoverEntries         func([]persistence.Entry) error
	StateDelta             func() (persistence.StateDelta, error)
	Complete               func(started time.Time) error
	Migrate                func(prefix *persistence.StateCheckpoint) error
	AttachJournal          func()
	SetRestored            func(int64)
	SetFresh               func(bool)
}

func (r TenantRestore) Run(ctx context.Context) error {
	j := r.Journal
	started := time.Now().UTC()
	failed := func() { log.Printf("quarantined %s: %s", r.ID, r.Fault()) }
	quarantine := func(err error) {
		r.Quarantine(err)
		failed()
	}
	var statePosition int64
	var stateActive bool
	if recovery, ok := j.(persistence.RuntimeStateRecovery); ok {
		var err error
		statePosition, stateActive, err = recovery.StatePosition(ctx, r.ID)
		if err != nil {
			return fmt.Errorf("read SQL state position %s: %w", r.ID, err)
		}
	}
	if r.AuditEmpty() {
		if all, err := j.Entries(ctx, r.ID, 0); err == nil {
			r.BackfillAudit(all)
		}
	}
	var after int64
	if r.SnapshotEvery > 0 {
		compatibility, err := r.Compatibility()
		if err != nil {
			return fmt.Errorf("snapshot format %s: %w", r.ID, err)
		}
		key, err := compatibility.RuntimeKey(r.Code)
		if err != nil {
			return fmt.Errorf("snapshot format %s: %w", r.ID, err)
		}
		seq, state, ok, err := j.Snapshot(ctx, r.ID, key)
		if err == nil && !ok {
			key, err = compatibility.Key(r.Code)
			if err == nil {
				seq, state, ok, err = j.Snapshot(ctx, r.ID, key)
			}
		}
		if err == nil && !ok && key != r.Code {
			seq, state, ok, err = j.Snapshot(ctx, r.ID, r.Code)
		}
		if err != nil {
			if errors.Is(err, persistence.ErrTenantSnapshot) {
				quarantine(err)
				return nil
			}
			return fmt.Errorf("snapshot %s: %w", r.ID, err)
		}
		if ok && (!stateActive || seq <= statePosition) {
			if err := compatibility.Check(r.ID, state, seq, r.Build()); err != nil {
				quarantine(err)
				return nil
			}
			runtime, runtimeErr := ReadCheckpoint(state)
			if runtimeErr != nil {
				r.Quarantine(runtimeErr)
				return nil
			}
			if runtime != nil {
				if _, err := j.Entries(ctx, r.ID, seq); err != nil {
					if !errors.Is(err, persistence.ErrTenantJournal) {
						return fmt.Errorf("read journal %s: %w", r.ID, err)
					}
					quarantine(err)
					return nil
				}
				if j.Position(r.ID) == statePosition {
					if err := r.RestoreRuntimeSnapshot(state, seq, statePosition, started); err != nil {
						quarantine(fmt.Errorf("SQL checkpoint recovery: %w", err))
						return nil
					}
					latest, err := r.CheckpointRuntime()
					if err != nil {
						quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
						return nil
					}
					r.SetRestored(latest)
					r.SetFresh(false)
					log.Printf("restored %s from the snapshot at %d, then replayed %d entries with SQL records on demand", r.ID, seq, statePosition-seq)
					return nil
				}
			} else {
				if err := r.RecoverSnapshot(state, seq); err != nil {
					failed()
					return nil
				}
				after = seq
			}
		}
		r.SetRestored(after)
	}
	entries, err := j.Entries(ctx, r.ID, after)
	if err != nil {
		if errors.Is(err, persistence.ErrTenantJournal) {
			quarantine(err)
			return nil
		}
		return fmt.Errorf("read journal %s: %w", r.ID, err)
	}
	var prefix *persistence.StateCheckpoint
	if stateActive && statePosition >= after && statePosition < j.Position(r.ID) {
		split := int(statePosition - after)
		if err := r.RecoverEntries(entries[:split]); err != nil {
			failed()
			return nil
		}
		delta, err := r.StateDelta()
		if err != nil {
			quarantine(fmt.Errorf("runtime state prefix: %w", err))
			return nil
		}
		prefix = &persistence.StateCheckpoint{Position: statePosition, Delta: delta}
		if err := r.RecoverEntries(entries[split:]); err != nil {
			failed()
			return nil
		}
	} else if err := r.RecoverEntries(entries); err != nil {
		failed()
		return nil
	}
	r.SetFresh(after == 0 && len(entries) == 0)
	if err := r.Complete(started); err != nil {
		quarantine(err)
		return nil
	}
	if after > 0 {
		log.Printf("restored %s from the snapshot at %d, then replayed %d entries", r.ID, after, len(entries))
	} else {
		log.Printf("replayed %d entries for %s", len(entries), r.ID)
	}
	if err := r.Migrate(prefix); err != nil {
		quarantine(fmt.Errorf("runtime state: %w", err))
		return nil
	}
	r.AttachJournal()
	if _, ok := j.(persistence.StateSnapshotStorage); ok && r.SnapshotEvery > 0 {
		seq, err := r.CheckpointRuntime()
		if err != nil {
			quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
			return nil
		}
		r.SetRestored(seq)
	}
	return nil
}

type SelectionReplacement struct {
	Compatible func() bool
	Adopt      func()
	Recover    func([]persistence.Entry) error
	Migrate    func() error
	Attach     func()
	Deactivate func() error
}

type SelectionRestore struct {
	ID         string
	Journal    persistence.Journals
	Selection  map[string]string
	Current    map[string]string
	Suspended  func() bool
	Quarantine func(error)
	Deactivate func() error
	Compose    func() (SelectionReplacement, error)
}

func (r SelectionRestore) Run(ctx context.Context) (bool, error) {
	if maps.Equal(r.Selection, r.Current) {
		if err := r.Deactivate(); err != nil {
			r.Quarantine(err)
		}
		return false, nil
	}
	if !r.Suspended() {
		r.Quarantine(fmt.Errorf("provider change requires maintenance"))
		return false, nil
	}
	fresh, err := r.Compose()
	if err != nil {
		r.Quarantine(err)
		return false, nil
	}
	if !fresh.Compatible() {
		r.Quarantine(fmt.Errorf("replacement has incompatible owner or kernel restore formats"))
		return false, nil
	}
	fresh.Adopt()
	entries, err := r.Journal.Entries(ctx, r.ID, 0)
	if err != nil {
		return false, err
	}
	for _, step := range []func() error{func() error { return fresh.Recover(entries) }, fresh.Migrate} {
		if err := step(); err != nil {
			r.Quarantine(err)
			return false, nil
		}
	}
	fresh.Attach()
	if err := fresh.Deactivate(); err != nil {
		r.Quarantine(err)
		return false, nil
	}
	return true, nil
}
