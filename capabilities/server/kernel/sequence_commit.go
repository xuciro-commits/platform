package kernel

import (
	"errors"
	"fmt"

	persistence "platformserver/kernel/store"
)

func (s *Sequence) Recording() bool { return s.AcceptResult != nil || s.Record != nil }

func (s *Sequence) Accept(entry persistence.Entry, key, hash string, delta func([]byte) (persistence.StateDelta, error), quarantine func(error)) ([]byte, error) {
	entry.Epoch = persistence.FormatEpoch
	if s.AcceptResult == nil {
		if s.Record != nil {
			s.Record(entry)
		}
		return entry.Body, nil
	}
	before := int64(0)
	if s.RuntimePosition != nil {
		before = s.RuntimePosition()
		s.Committed.ReleasePending()
	}
	raw, err := s.AcceptResult(entry, key, hash)
	if err != nil && (errors.Is(err, persistence.ErrCommitUnknown) || s.RuntimePosition != nil && s.RuntimePosition() > before) {
		quarantine(fmt.Errorf("accepted result commit could not be resolved: %w", err))
	}
	if err == nil && s.RuntimePosition != nil && s.RuntimePosition() > before {
		state, decodeErr := delta(raw)
		if decodeErr != nil {
			quarantine(fmt.Errorf("committed state delta: %w", decodeErr))
			return nil, decodeErr
		}
		s.Committed.HoldPending(state)
	}
	return raw, err
}
