package platformserver

import (
	"slices"
	"sync"
)

const (
	auditKept    = 1000
	personalKept = 5000
)

// auditLog is the tenant's bounded recent history for operators: accepted
// inputs, event deliveries and reads of personal data (ADR-0028 D4). It has
// its own lock because entries are appended from inside the submit pipeline
// and read by consoles without touching the tenant's main locks. Only the
// audit entries and deliveries survive a snapshot; personal reads are volatile.
type auditLog struct {
	mu         sync.Mutex
	entries    []AuditEntry
	deliveries []Delivery
	personal   []PersonalRead
}

func keepLast[T any](xs []T, n int) []T {
	if len(xs) > n {
		return xs[len(xs)-n:]
	}
	return xs
}

func (l *auditLog) remember(e AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = keepLast(append(l.entries, e), auditKept)
}

// all is the recent accepted inputs, oldest first.
func (l *auditLog) all() []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries)
}

func (l *auditLog) delivered(d Delivery) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deliveries = keepLast(append(l.deliveries, d), auditKept)
}

// recentDeliveries is the recent event deliveries, oldest first.
func (l *auditLog) recentDeliveries() []Delivery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.deliveries)
}

func (l *auditLog) readPersonal(r PersonalRead) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.personal = keepLast(append(l.personal, r), personalKept)
}

// personalReads is the latest reads of personal data, newest first.
func (l *auditLog) personalReads() []PersonalRead {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := slices.Clone(l.personal)
	slices.Reverse(out)
	return out
}

func (l *auditLog) snapshot(s *tenantState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s.Audit, s.Deliveries = slices.Clone(l.entries), slices.Clone(l.deliveries)
}

func (l *auditLog) restore(s *tenantState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries, l.deliveries = s.Audit, s.Deliveries
}
