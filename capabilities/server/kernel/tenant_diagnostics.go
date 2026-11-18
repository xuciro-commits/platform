package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"
	"sync"
	"time"
)

const (
	auditKept    = 1000
	personalKept = 5000
)

type AuditLog struct {
	mu         sync.Mutex
	entries    []protocol.AuditEntry
	deliveries []protocol.Delivery
	personal   []protocol.PersonalRead
	sink       func(protocol.AuditEntry)
	readSink   func(protocol.PersonalRead)
}

func (l *AuditLog) Fork() AuditLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return AuditLog{entries: slices.Clone(l.entries), deliveries: slices.Clone(l.deliveries), personal: slices.Clone(l.personal)}
}

func keepLast[T any](xs []T, n int) []T {
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

func (l *AuditLog) Remember(e protocol.AuditEntry) {
	if e.ID == "" {
		e.ID = auditIdentity(e.At, e.Member, e.App, e.Action, e.Target)
	}
	e.At = e.At.UTC()
	l.mu.Lock()
	l.entries = keepLast(append(l.entries, e), auditKept)
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		sink(e)
	}
}

func (l *AuditLog) All() []protocol.AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]protocol.AuditEntry{}, l.entries...)
}

func (l *AuditLog) Delivered(d protocol.Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deliveries = keepLast(append(l.deliveries, d), auditKept)
}

func (l *AuditLog) RecentDeliveries() []protocol.Delivery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]protocol.Delivery{}, l.deliveries...)
}

func (l *AuditLog) ReadPersonal(r protocol.PersonalRead) {
	if r.ID == "" {
		raw, _ := json.Marshal([]any{r.At.UTC().Format(time.RFC3339Nano), r.Member, r.Type, r.IDs, r.Fields})
		sum := sha256.Sum256(raw)
		r.ID = "read-" + hex.EncodeToString(sum[:])
	}
	r.At = r.At.UTC()
	l.mu.Lock()
	l.personal = keepLast(append(l.personal, r), personalKept)
	sink := l.readSink
	l.mu.Unlock()
	if sink != nil {
		sink(r)
	}
}

func (l *AuditLog) PersonalReads() []protocol.PersonalRead {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]protocol.PersonalRead{}, l.personal...)
	slices.Reverse(out)
	return out
}

func (l *AuditLog) SnapshotState() ([]protocol.AuditEntry, []protocol.Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries), slices.Clone(l.deliveries)
}
func (l *AuditLog) Restore(entries []protocol.AuditEntry, deliveries []protocol.Delivery) {
	l.mu.Lock()
	l.entries = slices.Clone(entries)
	l.deliveries = slices.Clone(deliveries)
	entries = slices.Clone(l.entries)
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		for _, entry := range entries {
			if entry.ID == "" {
				entry.ID = auditIdentity(entry.At, entry.Member, entry.App, entry.Action, entry.Target)
			}
			entry.At = entry.At.UTC()
			sink(entry)
		}
	}
}

func (l *AuditLog) SetSink(sink func(protocol.AuditEntry), readSink func(protocol.PersonalRead)) {
	l.mu.Lock()
	l.sink, l.readSink = sink, readSink
	l.mu.Unlock()
}

func (l *AuditLog) Backfill() {
	l.mu.Lock()
	entries := slices.Clone(l.entries)
	reads := slices.Clone(l.personal)
	sink, readSink := l.sink, l.readSink
	l.mu.Unlock()
	if sink != nil {
		for _, entry := range entries {
			sink(entry)
		}
	}
	if readSink != nil {
		for _, read := range reads {
			readSink(read)
		}
	}
}

func auditIdentity(at time.Time, fields ...string) string {
	raw, _ := json.Marshal(append([]string{at.UTC().Format(time.RFC3339Nano)}, fields...))
	sum := sha256.Sum256(raw)
	return "audit-" + hex.EncodeToString(sum[:])
}
