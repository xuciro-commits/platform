package store

import (
	"platformserver/kernel/protocol"
	"slices"
	"strings"
	"time"
)

func normalizeRun(entry protocol.RunIndexEntry) protocol.RunIndexEntry {
	entry.At = entry.At.UTC()
	entry.Payload = append([]byte(nil), entry.Payload...)
	return entry
}

func normalizeRuns(entries []protocol.RunIndexEntry) []protocol.RunIndexEntry {
	latest := map[string]protocol.RunIndexEntry{}
	for _, entry := range entries {
		entry = normalizeRun(entry)
		if entry.ID == "" {
			continue
		}
		latest[entry.ID] = entry
	}
	out := make([]protocol.RunIndexEntry, 0, len(latest))
	for _, entry := range latest {
		out = append(out, entry)
	}
	slices.SortFunc(out, func(left, right protocol.RunIndexEntry) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	return out
}

func runRetainedFrom(entries []protocol.RunIndexEntry) time.Time {
	var earliest time.Time
	for _, entry := range entries {
		if !entry.At.IsZero() && (earliest.IsZero() || entry.At.Before(earliest)) {
			earliest = entry.At
		}
	}
	return earliest.UTC()
}

func selectRunIndexPage(entries []protocol.RunIndexEntry, query protocol.RunIndexQuery) protocol.RunIndexPage {
	matched := make([]protocol.RunIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if query.Scope != "" && entry.Scope != query.Scope ||
			query.Type != "" && entry.Type != query.Type ||
			query.Source != "" && entry.Source != query.Source ||
			query.App != "" && entry.App != query.App ||
			query.Status != "" && entry.Status != query.Status ||
			!query.From.IsZero() && entry.At.Before(query.From) ||
			!query.To.IsZero() && !entry.At.Before(query.To) {
			continue
		}
		matched = append(matched, normalizeRun(entry))
	}
	slices.SortFunc(matched, func(left, right protocol.RunIndexEntry) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	page := protocol.RunIndexPage{Entries: []protocol.RunIndexEntry{}, Total: len(matched), RetainedFrom: runRetainedFrom(matched)}
	if query.Offset < 0 || query.Limit < 1 {
		return page
	}
	start := min(query.Offset, len(matched))
	page.Entries = append(page.Entries, matched[start:min(start+query.Limit, len(matched))]...)
	return page
}
