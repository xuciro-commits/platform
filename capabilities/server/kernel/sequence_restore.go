package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (s *Sequence) RestoreRecords(saved map[string][]records.StoredState, check func(any) error) (map[string][]records.StoredState, error) {
	held := map[string][]records.StoredState{}
	s.Projection.Lock()
	defer s.Projection.Unlock()
	for typ, rows := range saved {
		et := s.Projection.Type(typ)
		if et == nil {
			held[typ] = rows
			continue
		}
		entries := make([]records.Entry[*records.Row], 0, len(rows))
		for _, r := range rows {
			v := reflect.New(et.Info().Go).Elem()
			decode, err := records.DecodeRowImage(et.Info(), r.Value, r.Projection, s.Projection.Current().Release(), r.Format)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(decode, v.Addr().Interface()); err != nil {
				return nil, err
			}
			if err := check(v.Interface()); err != nil {
				return nil, err
			}
			records.EmptyLists(v)
			recovered := &records.Row{Value: v, Format: r.Format, History: r.History, Projection: records.CloneProjection(r.Projection), Extraction: r.Extraction}
			head, err := recovered.HistoryDigest()
			if err != nil {
				return nil, err
			}
			if r.HistoryHead != "" && r.HistoryHead != head {
				return nil, fmt.Errorf("snapshot history chain differs")
			}
			recovered.HistoryHead = head
			recovered.RetainOriginal(r.Value)
			entries = append(entries, records.Entry[*records.Row]{Key: records.Header(v).ID, Value: recovered})
		}
		index, err := records.From(entries)
		if err != nil {
			return nil, err
		}
		et.SetRows(index)
		s.Projection.Restored()
	}
	return held, nil
}

func (s *Sequence) VerifyRuntimeTail(ctx context.Context, j persistence.Journals, snapshot, current int64, entries []persistence.Entry, decode func([]byte) (persistence.StateDelta, error), states func(string) platform.AcceptedStateApp) error {
	verifier, ok := j.(persistence.StateSnapshotVerifier)
	if !ok {
		return fmt.Errorf("tenant %s: SQL checkpoint comparison unavailable", s.ID)
	}
	deltas := make([]persistence.StateDelta, 0, len(entries))
	changed := map[string]persistence.StateRecord{}
	owners := map[string]json.RawMessage{}
	for _, entry := range entries {
		delta, err := decode(entry.Body)
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
				current, readErr := s.Projection.Current().View().StateCurrent(ctx, s.ID, replacement.Type, replacement.ID)
				if readErr != nil {
					return readErr
				}
				expected = persistence.StateRecord{Type: replacement.Type, ID: replacement.ID, Image: current.Image, HistoryBase: current.HistoryCount, HistoryHead: current.HistoryHead}
			}
			expected.Image, err = persistence.ReplaceStateRecordValue(expected.Image, replacement)
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
				prior, err = s.Projection.Current().View().StateValue(ctx, s.ID, update.Owner, update.Key, -1)
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
		et := s.Projection.Type(expected.Type)
		if et == nil {
			return fmt.Errorf("tenant %s: restored record type %s is absent", s.ID, expected.Type)
		}
		actual := et.Rows().Get(expected.ID)
		if actual == nil {
			return fmt.Errorf("tenant %s: restored record %s/%s is absent", s.ID, expected.Type, expected.ID)
		}
		image, err := actual.Image()
		if err != nil {
			return err
		}
		var saved records.StoredState
		if err = json.Unmarshal(expected.Image, &saved); err != nil {
			return err
		}
		if !StoredJSONEqual(image, saved.Value) || actual.Format != saved.Format || !StoredJSONEqual(storedJSON(actual.Projection), storedJSON(saved.Projection)) || !StoredJSONEqual(storedJSON(actual.Extraction), storedJSON(saved.Extraction)) {
			return fmt.Errorf("tenant %s: restored record differs: %s/%s", s.ID, expected.Type, expected.ID)
		}
		count := len(expected.History)
		if expected.HistoryBase >= 0 {
			count += expected.HistoryBase
		}
		head := expected.HistoryHead
		if head == "" && expected.HistoryBase < 0 {
			head = platform.EmptyRecordHistory
			for i, raw := range expected.History {
				head, err = platform.AppendRecordHistory(head, i, raw)
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
			return fmt.Errorf("tenant %s: restored record history differs: %s/%s", s.ID, expected.Type, expected.ID)
		}
	}
	for app, expected := range owners {
		owner := states(app)
		if owner == nil {
			return fmt.Errorf("tenant %s: restored owner %s is absent", s.ID, app)
		}
		actual, err := owner.AcceptedState()
		if err != nil {
			return err
		}
		if !StoredJSONEqual(actual, expected) {
			return fmt.Errorf("tenant %s: restored owner differs: %s", s.ID, app)
		}
	}
	return verifier.VerifyStateSnapshot(ctx, s.ID, snapshot, current, deltas)
}

func storedJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
