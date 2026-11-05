package store

import (
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

func normalizeRun(entry platform.RunIndexEntry) platform.RunIndexEntry {
	entry.At = entry.At.UTC()
	entry.Payload = append([]byte(nil), entry.Payload...)
	return entry
}

func normalizeRuns(entries []platform.RunIndexEntry) []platform.RunIndexEntry {
	latest := map[string]platform.RunIndexEntry{}
	for _, entry := range entries {
		entry = normalizeRun(entry)
		if entry.ID == "" {
			continue
		}
		latest[entry.ID] = entry
	}
	out := make([]platform.RunIndexEntry, 0, len(latest))
	for _, entry := range latest {
		out = append(out, entry)
	}
	slices.SortFunc(out, func(left, right platform.RunIndexEntry) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	return out
}

func runRetainedFrom(entries []platform.RunIndexEntry) time.Time {
	var earliest time.Time
	for _, entry := range entries {
		if !entry.At.IsZero() && (earliest.IsZero() || entry.At.Before(earliest)) {
			earliest = entry.At
		}
	}
	return earliest.UTC()
}

func selectRunIndexPage(entries []platform.RunIndexEntry, query platform.RunIndexQuery) platform.RunIndexPage {
	matched := make([]platform.RunIndexEntry, 0, len(entries))
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
	slices.SortFunc(matched, func(left, right platform.RunIndexEntry) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	page := platform.RunIndexPage{Entries: []platform.RunIndexEntry{}, Total: len(matched), RetainedFrom: runRetainedFrom(matched)}
	if query.Offset < 0 || query.Limit < 1 {
		return page
	}
	start := min(query.Offset, len(matched))
	page.Entries = append(page.Entries, matched[start:min(start+query.Limit, len(matched))]...)
	return page
}
