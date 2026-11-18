package store

import (
	"encoding/base64"
	"fmt"
	"platformserver/kernel/protocol"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 200
)

func encodeAuditCursor(at time.Time, id string) string {
	payload := strconv.FormatInt(at.Unix(), 10) + "." + fmt.Sprintf("%09d", at.Nanosecond()) + "." + id
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeAuditCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid audit cursor")
	}
	parts := strings.SplitN(string(raw), ".", 3)
	if len(parts) != 3 || parts[2] == "" {
		return time.Time{}, "", fmt.Errorf("invalid audit cursor")
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid audit cursor")
	}
	nanos, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || nanos < 0 || nanos >= int64(time.Second) {
		return time.Time{}, "", fmt.Errorf("invalid audit cursor")
	}
	return time.Unix(seconds, nanos).UTC(), parts[2], nil
}

func auditLimit(limit int) int {
	if limit <= 0 {
		return defaultAuditLimit
	}
	if limit > maxAuditLimit {
		return maxAuditLimit
	}
	return limit
}

func normalizeAuditEntry(entry protocol.AuditEntry) protocol.AuditEntry {
	entry.At = entry.At.UTC()
	return entry
}

func normalizePersonalRead(read protocol.PersonalRead) protocol.PersonalRead {
	read.At = read.At.UTC()
	read.IDs = slices.Clone(read.IDs)
	read.Fields = slices.Clone(read.Fields)
	return read
}

func auditBefore(entry protocol.AuditEntry, at time.Time, id string) bool {
	return entry.At.Before(at) || entry.At.Equal(at) && entry.ID < id
}

func sortAuditNewest(entries []protocol.AuditEntry) {
	slices.SortFunc(entries, func(left, right protocol.AuditEntry) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
}

func sortPersonalReadsNewest(reads []protocol.PersonalRead) {
	slices.SortFunc(reads, func(left, right protocol.PersonalRead) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
}

func earliestAudit(entries []protocol.AuditEntry, tenant string) time.Time {
	var earliest time.Time
	for _, entry := range entries {
		if entry.At.IsZero() {
			continue
		}
		if tenant != "" && entry.ID == "" {
			continue
		}
		if earliest.IsZero() || entry.At.Before(earliest) {
			earliest = entry.At
		}
	}
	return earliest.UTC()
}

func earliestPersonalRead(reads []protocol.PersonalRead) time.Time {
	var earliest time.Time
	for _, read := range reads {
		if read.At.IsZero() {
			continue
		}
		if earliest.IsZero() || read.At.Before(earliest) {
			earliest = read.At
		}
	}
	return earliest.UTC()
}

func selectAuditPage(entries []protocol.AuditEntry, query protocol.AuditQuery, retainedFrom time.Time) (protocol.AuditPage, error) {
	var cursorAt time.Time
	var cursorID string
	if query.Cursor != "" {
		var err error
		cursorAt, cursorID, err = decodeAuditCursor(query.Cursor)
		if err != nil {
			return protocol.AuditPage{}, err
		}
	}
	limit := auditLimit(query.Limit)
	matched := make([]protocol.AuditEntry, 0, min(len(entries), limit+1))
	needle := strings.ToLower(query.Search)
	for _, entry := range entries {
		if query.Cursor != "" && !auditBefore(entry, cursorAt, cursorID) {
			continue
		}
		if !query.From.IsZero() && entry.At.Before(query.From) ||
			!query.To.IsZero() && !entry.At.Before(query.To) ||
			query.Member != "" && entry.Member != query.Member ||
			query.App != "" && entry.App != query.App ||
			query.Action != "" && entry.Action != query.Action ||
			needle != "" && !strings.Contains(strings.ToLower(entry.Member+" "+entry.App+" "+entry.Action+" "+entry.Target), needle) {
			continue
		}
		matched = append(matched, normalizeAuditEntry(entry))
	}
	sortAuditNewest(matched)
	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}
	if matched == nil {
		matched = []protocol.AuditEntry{}
	}
	page := protocol.AuditPage{
		Entries:  matched,
		HasMore:  hasMore,
		Coverage: protocol.AuditCoverage{RetainedFrom: retainedFrom.UTC(), Truncated: hasMore},
	}
	if hasMore {
		last := matched[len(matched)-1]
		page.NextCursor = encodeAuditCursor(last.At, last.ID)
	}
	return page, nil
}

func selectPersonalReadPage(reads []protocol.PersonalRead, query protocol.PersonalReadQuery, retainedFrom time.Time) (protocol.PersonalReadPage, error) {
	var cursorAt time.Time
	var cursorID string
	if query.Cursor != "" {
		var err error
		cursorAt, cursorID, err = decodeAuditCursor(query.Cursor)
		if err != nil {
			return protocol.PersonalReadPage{}, err
		}
	}
	limit := auditLimit(query.Limit)
	matched := make([]protocol.PersonalRead, 0, min(len(reads), limit+1))
	for _, read := range reads {
		if query.Cursor != "" && !auditBefore(protocol.AuditEntry{At: read.At, ID: read.ID}, cursorAt, cursorID) {
			continue
		}
		if !query.From.IsZero() && read.At.Before(query.From) ||
			!query.To.IsZero() && !read.At.Before(query.To) ||
			query.Member != "" && read.Member != query.Member ||
			query.Type != "" && read.Type != query.Type {
			continue
		}
		matched = append(matched, normalizePersonalRead(read))
	}
	sortPersonalReadsNewest(matched)
	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}
	if matched == nil {
		matched = []protocol.PersonalRead{}
	}
	page := protocol.PersonalReadPage{
		Entries:  matched,
		HasMore:  hasMore,
		Coverage: protocol.PersonalReadCoverage{RetainedFrom: retainedFrom.UTC(), Truncated: hasMore},
	}
	if hasMore {
		last := matched[len(matched)-1]
		page.NextCursor = encodeAuditCursor(last.At, last.ID)
	}
	return page, nil
}
