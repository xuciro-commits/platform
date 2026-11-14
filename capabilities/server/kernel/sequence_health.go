package kernel

import (
	"context"
	"errors"
	"time"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
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
	journal, ok := s.Store.(persistence.Journals)
	if !ok {
		return errors.New("tenant journal is unavailable")
	}
	if _, err := journal.Entries(ctx, s.ID, journal.Position(s.ID)); err != nil {
		return err
	}
	if _, err := s.Log().QueryObservations(platform.ObservationQuery{Tenant: s.ID, To: time.Now().UTC(), Limit: 1}); err != nil {
		return err
	}
	if _, err := s.Log().QueryAuditEntries(platform.AuditQuery{Tenant: s.ID, To: time.Now().UTC(), Limit: 1}); err != nil {
		return err
	}
	return nil
}

func (s *Sequence) AuditEmpty() bool {
	page, err := s.Log().QueryAuditEntries(platform.AuditQuery{Tenant: s.ID, Limit: 1})
	return err == nil && len(page.Entries) == 0
}

func (s *Sequence) AttachJournal(ctx context.Context, journal persistence.Journals) {
	s.RuntimePosition = func() int64 { return journal.Position(s.ID) }
	s.Store = journal
	s.AuditLog.Backfill()
	id := s.ID
	s.AcceptResult = func(e persistence.Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
}
