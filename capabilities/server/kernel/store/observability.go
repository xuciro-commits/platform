package store

import (
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

const (
	defaultObservationLimit = 100
	maxObservationLimit     = 200
)

func normalizeObservation(event platform.Observation) platform.Observation {
	event.At = event.At.UTC()
	if event.Attributes == nil {
		event.Attributes = map[string]string{}
	} else {
		event.Attributes = maps.Clone(event.Attributes)
	}
	return event
}

func encodeObservationCursor(event platform.Observation) string {
	payload := strconv.FormatInt(event.At.Unix(), 10) + "." + fmt.Sprintf("%09d", event.At.Nanosecond()) + "." + event.ID
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeObservationCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid observation cursor")
	}
	parts := strings.SplitN(string(raw), ".", 3)
	if len(parts) != 3 || parts[2] == "" {
		return time.Time{}, "", fmt.Errorf("invalid observation cursor")
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid observation cursor")
	}
	nanos, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || nanos < 0 || nanos >= int64(time.Second) {
		return time.Time{}, "", fmt.Errorf("invalid observation cursor")
	}
	return time.Unix(seconds, nanos).UTC(), parts[2], nil
}

func prepareObservationQuery(query platform.ObservationQuery) (time.Time, string, bool, error) {
	if query.Tenant == "" {
		return time.Time{}, "", false, fmt.Errorf("observations: tenant is required")
	}
	if query.Cursor == "" {
		return time.Time{}, "", false, nil
	}
	at, id, err := decodeObservationCursor(query.Cursor)
	return at, id, true, err
}

func observationLimit(limit int) int {
	if limit <= 0 {
		return defaultObservationLimit
	}
	if limit > maxObservationLimit {
		return maxObservationLimit
	}
	return limit
}

func observationMatches(event platform.Observation, query platform.ObservationQuery) bool {
	if event.Tenant != query.Tenant {
		return false
	}
	if !query.From.IsZero() && event.At.Before(query.From) {
		return false
	}
	if !query.To.IsZero() && !event.At.Before(query.To) {
		return false
	}
	if query.Level != "" && event.Level != query.Level {
		return false
	}
	if query.Source != "" && event.Source != query.Source {
		return false
	}
	if query.App != "" && event.App != query.App {
		return false
	}
	if query.Member != "" && event.Member != query.Member {
		return false
	}
	if query.RequestID != "" && event.RequestID != query.RequestID {
		return false
	}
	if query.TraceID != "" && event.TraceID != query.TraceID {
		return false
	}
	if query.RunID != "" && event.RunID != query.RunID {
		return false
	}
	if query.Resource != "" && event.Resource != query.Resource {
		return false
	}
	if query.Search != "" {
		needle := strings.ToLower(query.Search)
		if !strings.Contains(strings.ToLower(event.Message), needle) {
			matched := false
			for _, value := range event.Attributes {
				if strings.Contains(strings.ToLower(value), needle) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

func observationBefore(event platform.Observation, at time.Time, id string) bool {
	return event.At.Before(at) || event.At.Equal(at) && event.ID < id
}

func sortObservationsNewest(events []platform.Observation) {
	slices.SortFunc(events, func(left, right platform.Observation) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
}

func sortObservationsOldest(events []platform.Observation) {
	slices.SortFunc(events, func(left, right platform.Observation) int {
		if order := left.At.Compare(right.At); order != 0 {
			return order
		}
		return strings.Compare(left.ID, right.ID)
	})
}

func earliestObservation(events []platform.Observation, tenant string) time.Time {
	var earliest time.Time
	for _, event := range events {
		if event.Tenant != tenant {
			continue
		}
		if earliest.IsZero() || event.At.Before(earliest) {
			earliest = event.At
		}
	}
	return earliest.UTC()
}

func selectObservationPage(events []platform.Observation, query platform.ObservationQuery, retainedFrom time.Time) (platform.ObservationPage, error) {
	cursorAt, cursorID, hasCursor, err := prepareObservationQuery(query)
	if err != nil {
		return platform.ObservationPage{}, err
	}
	limit := observationLimit(query.Limit)
	matched := make([]platform.Observation, 0, min(len(events), limit+1))
	for _, event := range events {
		if hasCursor && !observationBefore(event, cursorAt, cursorID) {
			continue
		}
		if observationMatches(event, query) {
			matched = append(matched, normalizeObservation(event))
		}
	}
	sortObservationsNewest(matched)
	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}
	return finishObservationPage(matched, retainedFrom, hasMore), nil
}

func finishObservationPage(entries []platform.Observation, retainedFrom time.Time, hasMore bool) platform.ObservationPage {
	if entries == nil {
		entries = []platform.Observation{}
	}
	page := platform.ObservationPage{
		Entries:  entries,
		HasMore:  hasMore,
		Coverage: platform.ObservationLogCoverage{RetainedFrom: retainedFrom.UTC(), Truncated: hasMore},
	}
	if hasMore {
		page.NextCursor = encodeObservationCursor(entries[len(entries)-1])
	}
	return page
}
