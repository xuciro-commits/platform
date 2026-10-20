package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"platformserver/internal/records"

	"platformserver/journal"
)

// A row retains only its immutable historical position. Fetches do not install
// a second history cache, so even a long-lived record need not retain its past.
func (s *recordStore) attachHistory(et *entityType, id string, r *records.Row, store journal.RuntimeState, tenant string) {
	count := r.HistoryLength()
	copy := *r
	copy.History = nil
	copy.HistoryParent = nil
	copy.HistoryCount = count
	typ := et.info.Type
	copy.LoadHistory = func(after int) []records.RecordChange {
		list, err := store.StateHistory(context.Background(), tenant, typ, id, after, count)
		if err != nil {
			panic(fmt.Errorf("record history %s/%s: %w", typ, id, err))
		}
		out := make([]records.RecordChange, len(list))
		for i, raw := range list {
			if err := json.Unmarshal(raw, &out[i]); err != nil {
				panic(err)
			}
		}
		return out
	}
	et.setRow(id, &copy)
}
