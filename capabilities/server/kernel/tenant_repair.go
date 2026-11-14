package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
)

type TenantRepair struct {
	ID               string
	Code             string
	SnapshotEvery    int64
	Journal          persistence.Journals
	Compatible       func() error
	Prepare          func()
	Recover          func([]persistence.Entry) error
	CheckProcesses   func() error
	Project          func() (*records.SQLProjection, error)
	Capture          func(position func() int64) (json.RawMessage, int64, error)
	Compatibility    func() (SnapshotCompatibility, error)
	RecoveryComplete func(started time.Time)
	Migrate          func() error
	Attach           func()
	Deactivate       func() error
	Replace          func() error
}

func (r TenantRepair) Run(ctx context.Context, started time.Time) (*records.SQLProjection, int, error) {
	id := r.ID
	if err := r.Compatible(); err != nil {
		return nil, 0, err
	}
	r.Prepare()
	entries, err := r.Journal.Entries(ctx, id, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("read tenant %s from the beginning: %w", id, err)
	}
	if err := r.Recover(entries); err != nil {
		return nil, 0, fmt.Errorf("recover tenant %s: %w", id, err)
	}
	if err := r.CheckProcesses(); err != nil {
		return nil, 0, fmt.Errorf("process recovery for tenant %s: %w", id, err)
	}
	projection, err := r.Project()
	if err != nil {
		return nil, 0, fmt.Errorf("rebuild tenant %s projection: %w", id, err)
	}
	if r.SnapshotEvery > 0 {
		state, seq, err := r.Capture(func() int64 { return r.Journal.Position(id) })
		if err != nil {
			return nil, 0, fmt.Errorf("capture repaired tenant %s: %w", id, err)
		}
		compatibility, err := r.Compatibility()
		if err != nil {
			return nil, 0, fmt.Errorf("snapshot format %s: %w", id, err)
		}
		key, err := compatibility.Key(r.Code)
		if err != nil {
			return nil, 0, fmt.Errorf("snapshot format %s: %w", id, err)
		}
		if err := r.Journal.RepairSnapshot(ctx, id, seq, key, state); err != nil {
			return nil, 0, fmt.Errorf("write repaired tenant %s checkpoint: %w", id, err)
		}
	}
	r.RecoveryComplete(started)
	if err := r.Migrate(); err != nil {
		return nil, 0, fmt.Errorf("recover tenant %s runtime state: %w", id, err)
	}
	r.Attach()
	if err := r.Deactivate(); err != nil {
		return nil, 0, fmt.Errorf("restore tenant %s module selection: %w", id, err)
	}
	if err := r.Replace(); err != nil {
		return nil, 0, err
	}
	return projection, len(entries), nil
}
