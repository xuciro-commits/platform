package platformserver

import (
	"context"
	"encoding/json"
	"fmt"

	"platformserver/journal"
)

func (r *row) recordHistory() []RecordChange { return r.historyAfter(0) }

func (r *row) historyLength() int {
	if r.loadHistory != nil || r.historyParent != nil || r.historyCount > 0 {
		return r.historyCount
	}
	return len(r.history)
}
func (r *row) historyAfter(start int) []RecordChange {
	if start >= r.historyLength() {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if r.loadHistory != nil {
		return r.loadHistory(start)
	}
	if r.historyParent == nil {
		return r.history[start:]
	}
	base := r.historyParent.historyLength()
	if start >= base {
		return r.history[start-base:]
	}
	// Walk immutable suffix segments once. Recursively concatenating prefixes
	// would copy a long in-memory history again at every retained revision.
	out := make([]RecordChange, 0, r.historyLength()-start)
	var suffixes [][]RecordChange
	for r.historyParent != nil && start < r.historyParent.historyLength() {
		suffixes = append(suffixes, r.history)
		r = r.historyParent
	}
	if r.loadHistory != nil {
		out = append(out, r.loadHistory(start)...)
	} else if r.historyParent != nil {
		out = append(out, r.history[start-r.historyParent.historyLength():]...)
	} else {
		out = append(out, r.history[start:]...)
	}
	for i := len(suffixes) - 1; i >= 0; i-- {
		out = append(out, suffixes[i]...)
	}
	return out
}

// A row retains only its immutable historical position. Fetches do not install
// a second history cache, so even a long-lived record need not retain its past.
func (s *recordStore) attachHistory(et *entityType, id string, r *row, store journal.RuntimeState, tenant string) {
	count := r.historyLength()
	copy := *r
	copy.history = nil
	copy.historyParent = nil
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
