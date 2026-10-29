package kernel

import (
	"platformserver/platform"
	"slices"
	"sync"
)

const (
	auditKept    = 1000
	personalKept = 5000
)

type AuditLog struct {
	mu         sync.Mutex
	entries    []platform.AuditEntry
	deliveries []platform.Delivery
	personal   []platform.PersonalRead
}

func keepLast[T any](xs []T, n int) []T {
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

func (l *AuditLog) Remember(e platform.AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = keepLast(append(l.entries, e), auditKept)
}

func (l *AuditLog) All() []platform.AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]platform.AuditEntry{}, l.entries...)
}

func (l *AuditLog) Delivered(d platform.Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deliveries = keepLast(append(l.deliveries, d), auditKept)
}

func (l *AuditLog) RecentDeliveries() []platform.Delivery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]platform.Delivery{}, l.deliveries...)
}

func (l *AuditLog) ReadPersonal(r platform.PersonalRead) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.personal = keepLast(append(l.personal, r), personalKept)
}

func (l *AuditLog) PersonalReads() []platform.PersonalRead {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]platform.PersonalRead{}, l.personal...)
	slices.Reverse(out)
	return out
}

func (l *AuditLog) SnapshotState() ([]platform.AuditEntry, []platform.Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries), slices.Clone(l.deliveries)
}
func (l *AuditLog) Restore(entries []platform.AuditEntry, deliveries []platform.Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries, l.deliveries = entries, deliveries
}
