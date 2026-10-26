package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"platformserver/internal/records"
	"platformserver/platform"
	"time"

	"platformserver/journal"
)

func runtimeCheckpoint(raw []byte) (*runtimeSnapshot, error) {
	var header struct {
		Runtime *runtimeSnapshot `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	if header.Runtime != nil {
		var shape struct {
			Records map[string]json.RawMessage `json:"records"`
		}
		if err := json.Unmarshal(raw, &shape); err != nil {
			return nil, err
		}
		if len(shape.Records) > 0 {
			return nil, fmt.Errorf("SQL owner checkpoint contains portable record state")
		}
	}
	return header.Runtime, nil
}

// restoreRuntimeSnapshot restores owner state against its original SQL record
// archive. Accepted tail results see that prefix plus their own replacements;
// the current SQL table is admitted only after the reconstructed tail agrees.
func (t *Tenant) restoreRuntimeSnapshot(ctx context.Context, j Journals, state []byte, seq, current int64, started time.Time) error {
	saved, err := runtimeCheckpoint(state)
	if err != nil {
		return err
	}
	if saved == nil || saved.Format != runtimeSnapshotFormat || saved.Position != seq || seq > current {
		return fmt.Errorf("tenant %s: SQL checkpoint format or position differs", t.ID)
	}
	snapshots, ok := j.(journal.StateSnapshotStorage)
	if !ok {
		return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", t.ID)
	}
	store, ok := j.(journal.RuntimeState)
	if !ok {
		return fmt.Errorf("tenant %s: SQL state unavailable", t.ID)
	}
	view, err := snapshots.BeginSnapshotStateRead(ctx, t.ID, seq, current)
	if err != nil {
		return err
	}
	defer view.Close()
	digest, err := view.Checkpoint(ctx, t.ID)
	if err != nil {
		return err
	}
	if digest != saved.Records {
		return fmt.Errorf("tenant %s: SQL checkpoint records differ", t.ID)
	}
	t.installRuntimeRecords(store, func() int64 { return seq })
	t.records.bindReadView(view)
	defer t.records.bindReadView(nil)
	t.installRuntimeHistory(view, func() int64 { return seq })
	if err = t.recoverSnapshot(state, seq); err != nil {
		return err
	}
	t.installRuntimeHistory(view, func() int64 { return seq })
	entries, err := j.Entries(ctx, t.ID, seq)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Kind != "accepted-result" {
			return fmt.Errorf("tenant %s: SQL checkpoint tail contains a legacy input", t.ID)
		}
	}
	if err = t.recoverEntries(entries); err != nil {
		return err
	}
	if err = t.verifyRuntimeSnapshotTail(ctx, j, seq, current, entries); err != nil {
		return err
	}
	if console, ok := t.app(PlatformApp).(IdentityRecovery); ok {
		console.CloseTokenSecretWindows()
	}
	t.installRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
	t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
	t.attachJournal(ctx, j)
	t.enableCurrentReadScopes()
	return nil
}

func saveRuntimeSnapshot(ctx context.Context, j Journals, tenant string, seq int64, key string, state []byte) error {
	runtime, err := runtimeCheckpoint(state)
	if err != nil {
		return err
	}
	if runtime != nil {
		snapshots, ok := j.(journal.StateSnapshotStorage)
		if !ok {
			return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", tenant)
		}
		return snapshots.SaveStateSnapshot(ctx, tenant, seq, key, state, runtime.Records)
	}
	return j.SaveSnapshot(ctx, tenant, seq, key, state)
}

// verifyRuntimeSnapshotTail retains only records mentioned by the accepted
// suffix. The archive supplies unchanged predecessors; SQL validates their
// union with these same sealed deltas without replacing differing data.
func (t *Tenant) verifyRuntimeSnapshotTail(ctx context.Context, j Journals, snapshot, current int64, entries []Entry) error {
	verifier, ok := j.(journal.StateSnapshotVerifier)
	if !ok {
		return fmt.Errorf("tenant %s: SQL checkpoint comparison unavailable", t.ID)
	}
	deltas := make([]journal.StateDelta, 0, len(entries))
	changed := map[string]journal.StateRecord{}
	owners := map[string]json.RawMessage{}
	for _, entry := range entries {
		delta, err := acceptedStorageDelta(entry.Body)
		if err != nil {
			return err
		}
		deltas = append(deltas, delta)
		for _, record := range delta.Records {
			changed[record.Type+"/"+record.ID] = record
		}
		for _, replacement := range delta.Replacements {
			key := replacement.Type + "/" + replacement.ID
			expected, exists := changed[key]
			if !exists {
				current, readErr := t.records.readView.StateCurrent(ctx, t.ID, replacement.Type, replacement.ID)
				if readErr != nil {
					return readErr
				}
				expected = journal.StateRecord{Type: replacement.Type, ID: replacement.ID, Image: current.Image, HistoryBase: current.HistoryCount, HistoryHead: current.HistoryHead}
			}
			expected.Image, err = journal.ReplaceStateRecordValue(expected.Image, replacement)
			if err != nil {
				return err
			}
			changed[key] = expected
		}
		for _, value := range delta.Values {
			if value.Owner == "app-state" {
				owners[value.Key] = value.Image
			}
		}
		for _, update := range delta.ValueUpdates {
			if update.Owner != "app-state" {
				continue
			}
			prior, exists := owners[update.Key]
			if !exists {
				prior, err = t.records.readView.StateValue(ctx, t.ID, update.Owner, update.Key, -1)
				if err != nil {
					return err
				}
			}
			owners[update.Key], err = update.Apply(prior)
			if err != nil {
				return err
			}
		}
	}
	for _, expected := range changed {
		et := t.records.types[expected.Type]
		if et == nil {
			return fmt.Errorf("tenant %s: restored record type %s is absent", t.ID, expected.Type)
		}
		actual := et.rows.Get(expected.ID)
		if actual == nil {
			return fmt.Errorf("tenant %s: restored record %s/%s is absent", t.ID, expected.Type, expected.ID)
		}
		image, err := actual.Image()
		if err != nil {
			return err
		}
		var saved recordState
		if err = json.Unmarshal(expected.Image, &saved); err != nil {
			return err
		}
		if !sameStoredJSON(image, saved.Value) || actual.Format != saved.Format || !sameStoredJSON(jsonOf(actual.Projection), jsonOf(saved.Projection)) || !sameStoredJSON(jsonOf(actual.Extraction), jsonOf(saved.Extraction)) {
			return fmt.Errorf("tenant %s: restored record differs: %s/%s", t.ID, expected.Type, expected.ID)
		}
		count := len(expected.History)
		if expected.HistoryBase >= 0 {
			count += expected.HistoryBase
		}
		head := expected.HistoryHead
		if head == "" && expected.HistoryBase < 0 {
			head = records.EmptyHistory
			for i, raw := range expected.History {
				head, err = records.AppendHistory(head, i, raw)
				if err != nil {
					return err
				}
			}
		}
		actualHead, err := actual.HistoryDigest()
		if err != nil {
			return err
		}
		if count != actual.HistoryLength() || head != "" && head != actualHead {
			return fmt.Errorf("tenant %s: restored record history differs: %s/%s", t.ID, expected.Type, expected.ID)
		}
	}
	for app, expected := range owners {
		owner, ok := t.app(app).(platform.AcceptedStateApp)
		if !ok {
			return fmt.Errorf("tenant %s: restored owner %s is absent", t.ID, app)
		}
		actual, err := owner.AcceptedState()
		if err != nil {
			return err
		}
		if !sameStoredJSON(actual, expected) {
			return fmt.Errorf("tenant %s: restored owner differs: %s", t.ID, app)
		}
	}
	return verifier.VerifyStateSnapshot(ctx, t.ID, snapshot, current, deltas)
}

// checkpointRuntime runs before serving a restored/migrated SQL tenant. A
// failed capture never publishes an owner image without its matching archive.
func (t *Tenant) checkpointRuntime(ctx context.Context, j Journals, code string) (int64, error) {
	state, seq, err := t.snapshotRuntime(ctx, j)
	if err != nil {
		return 0, err
	}
	key, err := t.snapshotStorageKey(code, state)
	if err != nil {
		return 0, err
	}
	if err = saveRuntimeSnapshot(ctx, j, t.ID, seq, key, state); err != nil {
		return 0, err
	}
	return seq, nil
}
