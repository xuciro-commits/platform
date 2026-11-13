package kernel

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type CommittedResult struct {
	Settlement
	App          func(string) platform.App
	ApplyBatch   func(*platform.Ledger, []byte) (bool, error)
	ApplyRecord  func(*platform.Ledger, []byte) (bool, error)
	Remember     func(member string, a platform.App, s *pb.Submission, at time.Time)
	PublishBatch func(acceptedformat.Batch)
	Publish      func(platform.Event, platform.AcceptedEventPlan)
}

func (s *Sequence) FinishCommitted(a platform.App, m platform.Member, hash string, committed []byte, h CommittedResult) (*pb.ChangeRecord, *kernel.Error) {
	conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	var envelope struct{ Kind string }
	if err := json.Unmarshal(committed, &envelope); err != nil {
		h.Quarantine(fmt.Errorf("committed result is not an envelope: %w", err))
		return nil, conflict
	}
	if envelope.Kind == "refusal" {
		saved, sub, err := acceptedformat.DecodeRefusal(committed)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != s.ID ||
			saved.RequestHash != hash || sub.GetPrincipalId() != m.ID {
			h.Quarantine(fmt.Errorf("committed refusal differs from the accepted request: %v", err))
			return nil, conflict
		}
		s.Committed.SaveRefusal(saved.App+"/"+sub.GetIdempotencyKey(), saved)
		h.Applied()
		h.ChangedOwner(saved.App)
		answer := saved.Error
		return nil, &answer
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, err := acceptedformat.DecodeBatch(committed)
		sub, subErr := acceptedformat.BatchSubmission(saved, receipt)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != s.ID ||
			saved.RequestHash != hash || subErr != nil || sub.GetPrincipalId() != m.ID {
			h.Quarantine(fmt.Errorf("committed batch differs from the accepted request: %v", err))
			return nil, conflict
		}
		applied, err := h.ApplyBatch(a.AcceptedLedger(), committed)
		if err != nil {
			h.Quarantine(fmt.Errorf("committed record batch could not be applied: %w", err))
			return nil, conflict
		}
		h.Applied()
		if applied {
			h.Remember(m.ID, a, sub, saved.At)
			h.PublishBatch(saved)
			h.ChangedOwner(saved.App)
		}
		h.Enqueue(saved.At)
		return receipt, nil
	}
	saved, receipt, err := acceptedformat.DecodeRecord(committed)
	if err != nil || saved.App != a.Manifest().ID || saved.Tenant != s.ID ||
		saved.RequestHash != hash || receipt.GetSubmission().GetPrincipalId() != m.ID {
		h.Quarantine(fmt.Errorf("committed result differs from the accepted request: %v", err))
		return nil, conflict
	}
	for _, subscriber := range saved.Event.Subscribers {
		if h.App(subscriber) == nil {
			h.Quarantine(fmt.Errorf("committed result requires missing subscriber %s", subscriber))
			return nil, conflict
		}
	}
	applied, err := h.ApplyRecord(a.AcceptedLedger(), committed)
	if err != nil {
		h.Quarantine(fmt.Errorf("committed accepted result could not be applied: %w", err))
		return nil, conflict
	}
	h.Applied()
	if saved.At.IsZero() {
		saved.At = receipt.GetRecordedTime().AsTime()
	}
	if applied {
		h.Remember(m.ID, a, receipt.GetSubmission(), saved.At)
		h.Publish(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event)
		h.ChangedOwner(saved.App)
	}
	h.Enqueue(saved.At)
	return receipt, nil
}

func (s *Sequence) ReplayPrior(a platform.App, sub *pb.Submission, binding *platform.SchemaVersion) (record *pb.ChangeRecord, hash string, refusal *kernel.Error, done bool) {
	prior := s.Committed.Prior(a.Manifest().ID, a.AcceptedLedger(), s.ID, sub.GetIdempotencyKey())
	if len(prior.Answer) > 0 {
		result, receipt, err := acceptedformat.DecodeBatch(prior.Answer)
		hash, hashErr := acceptedformat.SubmissionHash(sub, binding)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
		}
		return receipt, "", nil, true
	}
	if prior.Record != nil {
		if !proto.Equal(prior.Record.GetSubmission(), sub) {
			return nil, "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
		}
		saved, bound := s.Committed.SchemaVersion(a.Manifest().ID + "/" + sub.GetIdempotencyKey())
		if bound != (binding != nil) || binding != nil && saved != *binding {
			return nil, "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
		}
		return prior.Record, "", nil, true
	}
	hash, err := acceptedformat.SubmissionHash(sub, binding)
	if err != nil {
		return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded"), true
	}
	if prior.Refusal != nil {
		if prior.Refusal.RequestHash != hash {
			return nil, "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
		}
		saved := prior.Refusal.Error
		return nil, "", &saved, true
	}
	return nil, hash, nil, false
}

func (p Prior) SourceReplay(principal string, sub *pb.Submission, deleted bool) (*pb.ChangeRecord, *kernel.Error, bool) {
	if p.Record != nil {
		previous := p.Record.GetSubmission()
		if previous.GetPrincipalId() != principal || !proto.Equal(previous.GetTarget(), sub.GetTarget()) || !proto.Equal(previous.GetSchema(), sub.GetSchema()) && !deleted {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
		}
		return p.Record, nil, true
	}
	if p.Refusal != nil {
		saved := p.Refusal.Error
		return nil, &saved, true
	}
	return nil, nil, false
}

func (s *Sequence) ApplyBatch(l *platform.Ledger, raw []byte, h BatchApply) (bool, error) {
	prepared, err := s.PrepareBatch(l, raw, h)
	if err != nil || prepared == nil {
		return false, err
	}
	return s.PromoteBatch(prepared, h)
}

func (s *Sequence) CheckSubmission(a platform.App, m platform.Member, sub *pb.Submission) *kernel.Error {
	if sub.GetTenantId() != s.ID || sub.GetPrincipalId() != m.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if sub.GetAuthority() != a.Manifest().ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY}
	}
	if sub.GetIdempotencyKey() == "" || sub.GetTarget() == nil || sub.GetSchema() == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission has an invalid identity")
	}
	return nil
}
