package kernel

import (
	"errors"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
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

func CommitRefusal(err error) *contract.Error {
	if err == persistence.ErrAcceptedConflict {
		return &contract.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted result could not be committed")
}

func (s *Sequence) ResultApplied(useCurrentRows bool) {
	s.StateEpoch.Add(1)
	s.Projection.Lock()
	if s.Projection.Current().Position() != nil {
		s.Projection.Current().MarkApplied(s.Projection.Current().Position()())
	}
	if useCurrentRows {
		s.Projection.UseCurrentRows()
	}
	s.Projection.Unlock()
}

func ConflictRefusal(err error) *contract.Error {
	if err == nil {
		return nil
	}
	if refusal, ok := err.(*contract.Error); ok {
		return refusal
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}

func OutcomeOf(err *contract.Error) string {
	if err != nil {
		return err.Code.String()
	}
	return "ok"
}
