package platformserver

import (
	"context"
	"encoding/json"
	"fmt"

	"platformserver/journal"
)

func (r *row) recordHistory() []RecordChange {
	if r.loadHistory != nil {
		return r.loadHistory(0)
	}
	return r.history
}

func (r *row) historyLength() int {
	if r.loadHistory != nil {
		return r.historyCount
	}
	return len(r.history)
}
func (r *row) historyAfter(start int) []RecordChange {
	if start >= r.historyLength() {
		return nil
	}
	if r.loadHistory != nil {
		return r.loadHistory(start)
	}
	return r.history[start:]
}

// A row retains only its immutable historical position. Fetches do not install
// a second history cache, so even a long-lived record need not retain its past.
func (s *recordStore) attachHistory(et *entityType, id string, r *row, store journal.RuntimeState, tenant string) {
	count := len(r.history)
	if r.loadHistory != nil || r.historyCount > 0 {
		count = r.historyCount
	}
	copy := *r
	copy.history = nil
	copy.historyCount = count
	typ := et.info.Type
	copy.loadHistory = func(after int) []RecordChange {
		list, err := store.StateHistory(context.Background(), tenant, typ, id, after, count)
		if err != nil {
			panic(fmt.Errorf("record history %s/%s: %w", typ, id, err))
		}
		out := make([]RecordChange, len(list))
		for i, raw := range list {
			if err := json.Unmarshal(raw, &out[i]); err != nil {
				panic(err)
			}
		}
		return out
	}
	et.setRow(id, &copy)
}
