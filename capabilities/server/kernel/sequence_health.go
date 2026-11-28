package kernel

import (
	"context"
	"errors"
	"go.opentelemetry.io/otel/trace"
	"platformserver/kernel/protocol"
	"time"

	persistence "platformserver/kernel/store"
)

type LaneHealth struct {
	Holder      string    `json:"holder,omitempty"`
	HeldSeconds float64   `json:"heldSeconds"`
	Waiters     int64     `json:"waiters"`
	LastSlow    *LaneHold `json:"lastSlow,omitempty"`
}

func (s *Sequence) LaneHealth(now time.Time, outside bool) LaneHealth {
	state := s.Lane.State()
	lane := LaneHealth{Waiters: state.Waiters, LastSlow: s.LastSlowHold.Load()}
	if outside && !state.Since.IsZero() {
		lane.Holder, lane.HeldSeconds = state.Holder, now.Sub(state.Since).Seconds()
	}
	return lane
}

func (s *Sequence) StorageReady(ctx context.Context) error {
	if s.Log() == nil {
		return errors.New("operational store is unavailable")
	}
	if s.Store == nil {
		return nil
	}
	var journal persistence.Journals
	switch value := s.Store.(type) {
	case persistence.Journals:
		journal = value
	default:
		return errors.New("tenant journal is unavailable")
	}
	if _, err := journal.Entries(ctx, s.ID, journal.Position(s.ID)); err != nil {
		return err
	}
	if _, err := s.Log().QueryObservations(protocol.ObservationQuery{Tenant: s.ID, To: time.Now().UTC(), Limit: 1}); err != nil {
		return err
	}
	if _, err := s.Log().QueryAuditEntries(protocol.AuditQuery{Tenant: s.ID, To: time.Now().UTC(), Limit: 1}); err != nil {
		return err
	}
	return nil
}

func (s *Sequence) AuditEmpty() bool {
	page, err := s.Log().QueryAuditEntries(protocol.AuditQuery{Tenant: s.ID, Limit: 1})
	return err == nil && len(page.Entries) == 0
}

func (s *Sequence) AttachJournal(ctx context.Context, journal persistence.Journals) {
	s.RuntimePosition = func() int64 { return journal.Position(s.ID) }
	s.JournalEntries = func(readCtx context.Context, after int64) ([]persistence.Entry, error) {
		return journal.Entries(readCtx, s.ID, after)
	}
	s.Store = journal
	s.AuditLog.Backfill()
	id := s.ID
	s.AcceptResult = func(request context.Context, e persistence.Entry, key, hash string) (*persistence.AcceptedWrite, error) {
		commit := ctx
		if span := trace.SpanFromContext(request); span.IsRecording() {
			commit = trace.ContextWithSpan(ctx, span)
		}
		return journal.AppendAccepted(commit, id, e, key, hash)
	}
	s.AcceptResults = func(entries []persistence.AcceptedAppend) ([][]byte, error) {
		return journal.AppendAcceptedBatch(ctx, id, entries)
	}
}
