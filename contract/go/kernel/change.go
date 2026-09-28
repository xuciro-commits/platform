package kernel

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// ChangeLog implements K4 (contract/spec/K4-change-record.md) for one authority.
type ChangeLog struct {
	schemas *SchemaRegistry
	logs    map[string][]*pb.ChangeRecord
	byKey   map[string]map[string]*pb.ChangeRecord
	revs    map[[3]string]uint32 // (tenant, target type, target id) → revision (C12)
	next    int
	// Facts reports whether a fact is recorded in a tenant (K2); nil knows none (C11).
	Facts func(tenant, factID string) bool
}

func NewChangeLog(schemas *SchemaRegistry) *ChangeLog {
	return &ChangeLog{schemas: schemas, logs: map[string][]*pb.ChangeRecord{},
		byKey: map[string]map[string]*pb.ChangeRecord{}, revs: map[[3]string]uint32{}}
}

func (l *ChangeLog) Records(tenant string) []*pb.ChangeRecord { return l.logs[tenant] }

// Fork gives one decision a private change-log view. Accepted records are
// immutable; the maps and slices that SubmitChecked extends are independent.
// The host can discard this view when a durable append fails (ADR-0038 19a).
func (l *ChangeLog) Fork() *ChangeLog {
	copy := &ChangeLog{schemas: l.schemas, logs: make(map[string][]*pb.ChangeRecord, len(l.logs)),
		byKey: make(map[string]map[string]*pb.ChangeRecord, len(l.byKey)), revs: make(map[[3]string]uint32, len(l.revs)), next: l.next, Facts: l.Facts}
	for tenant, records := range l.logs {
		copy.logs[tenant] = slices.Clone(records)
	}
	for tenant, keys := range l.byKey {
		copy.byKey[tenant] = make(map[string]*pb.ChangeRecord, len(keys))
		for key, record := range keys {
			copy.byKey[tenant][key] = record
		}
	}
	for target, revision := range l.revs {
		copy.revs[target] = revision
	}
	return copy
}

// ApplyAccepted advances a log from the saved decision, without running its
// policy or business rules. It rejects a missing predecessor or conflicting
// receipt; reapplying the same saved record is a no-op. The host calls this
// only after the result's durable append or while recovering it (ADR-0038).
func (l *ChangeLog) ApplyAccepted(saved *pb.ChangeRecord) (bool, error) {
	if saved == nil || saved.GetSubmission() == nil || saved.GetRecordedTime() == nil || saved.GetValidTime() == nil {
		return false, fmt.Errorf("accepted change is incomplete")
	}
	s := saved.GetSubmission()
	if s.GetTenantId() == "" || s.GetIdempotencyKey() == "" || s.GetTarget().GetType() == "" || s.GetTarget().GetId() == "" {
		return false, fmt.Errorf("accepted change has no scoped identity")
	}
	if prior := l.byKey[s.GetTenantId()][s.GetIdempotencyKey()]; prior != nil {
		if proto.Equal(prior, saved) {
			return false, nil
		}
		return false, fmt.Errorf("accepted change conflicts with an earlier receipt")
	}
	if saved.GetChangeId() != fmt.Sprintf("chg-%d", l.next+1) {
		return false, fmt.Errorf("accepted change %s is not next after %d", saved.GetChangeId(), l.next)
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	if saved.GetRevision() != l.revs[target]+1 {
		return false, fmt.Errorf("accepted change %s has revision %d after %d", saved.GetChangeId(), saved.GetRevision(), l.revs[target])
	}
	log := l.logs[s.GetTenantId()]
	if len(log) > 0 && saved.GetRecordedTime().AsTime().Before(log[len(log)-1].GetRecordedTime().AsTime()) {
		return false, fmt.Errorf("accepted change %s moves recorded time backwards", saved.GetChangeId())
	}
	record := proto.Clone(saved).(*pb.ChangeRecord)
	l.next++
	l.revs[target] = record.GetRevision()
	l.logs[s.GetTenantId()] = append(log, record)
	if l.byKey[s.GetTenantId()] == nil {
		l.byKey[s.GetTenantId()] = map[string]*pb.ChangeRecord{}
	}
	l.byKey[s.GetTenantId()][s.GetIdempotencyKey()] = record
	return true, nil
}

func (l *ChangeLog) Submit(s *pb.Submission, now time.Time) (*pb.ChangeRecord, *Error) {
	return l.SubmitChecked(s, now, nil)
}

// SubmitChecked runs check after replay detection and before the append (C10);
// a replay returns the original record without running it.
func (l *ChangeLog) SubmitChecked(s *pb.Submission, now time.Time, check func() *Error) (*pb.ChangeRecord, *Error) {
	required := []string{s.GetTenantId(), s.GetPrincipalId(), s.GetAuthority(), s.GetIdempotencyKey(),
		s.GetTarget().GetType(), s.GetTarget().GetId(), s.GetSchema().GetName()}
	if slices.Contains(required, "") {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // C1
	}
	if !l.schemas.Accepts(s.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA) // C2
	}
	if existing := l.byKey[s.GetTenantId()][s.GetIdempotencyKey()]; existing != nil {
		if !proto.Equal(existing.GetSubmission(), s) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT) // C5
		}
		return existing, nil // C4
	}
	log := l.logs[s.GetTenantId()]
	if c := s.GetCausationId(); c != "" && !slices.ContainsFunc(log, func(r *pb.ChangeRecord) bool { return r.GetChangeId() == c }) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // C3
	}
	for _, fact := range s.GetEvidenceFactIds() {
		if l.Facts == nil || !l.Facts(s.GetTenantId(), fact) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // C11
		}
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	if s.ExpectedRevision != nil && s.GetExpectedRevision() != l.revs[target] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // C12
	}
	if check != nil {
		if err := check(); err != nil {
			return nil, err // C10
		}
	}
	recorded := now
	if n := len(log); n > 0 && log[n-1].GetRecordedTime().AsTime().After(now) {
		recorded = log[n-1].GetRecordedTime().AsTime() // C6
	}
	valid := s.GetValidTime()
	if valid == nil {
		valid = timestamppb.New(recorded) // C7
	}
	l.next++
	l.revs[target]++
	record := &pb.ChangeRecord{ChangeId: fmt.Sprintf("chg-%d", l.next), Submission: s,
		ValidTime: valid, RecordedTime: timestamppb.New(recorded), Revision: l.revs[target]}
	l.logs[s.GetTenantId()] = append(log, record)
	if l.byKey[s.GetTenantId()] == nil {
		l.byKey[s.GetTenantId()] = map[string]*pb.ChangeRecord{}
	}
	l.byKey[s.GetTenantId()][s.GetIdempotencyKey()] = record
	return record, nil
}

func schemaKey(s *pb.SchemaRef) string { return fmt.Sprintf("%s@%d", s.GetName(), s.GetVersion()) }

// Adopt takes over a previous authority's accepted records unchanged (K5 A10):
// into an empty tenant log, in recorded order, with unique change IDs and keys.
func (l *ChangeLog) Adopt(records []*pb.ChangeRecord) *Error {
	conflict := errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	if len(records) == 0 {
		return nil
	}
	tenant := records[0].GetSubmission().GetTenantId()
	if len(l.logs[tenant]) > 0 {
		return conflict
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i, r := range records {
		s := r.GetSubmission()
		if s.GetTenantId() != tenant || ids[r.GetChangeId()] || keys[s.GetIdempotencyKey()] ||
			i > 0 && r.GetRecordedTime().AsTime().Before(records[i-1].GetRecordedTime().AsTime()) {
			return conflict
		}
		ids[r.GetChangeId()], keys[s.GetIdempotencyKey()] = true, true
	}
	l.logs[tenant] = append([]*pb.ChangeRecord(nil), records...)
	l.byKey[tenant] = map[string]*pb.ChangeRecord{}
	for _, r := range records {
		l.byKey[tenant][r.GetSubmission().GetIdempotencyKey()] = r
		l.revs[[3]string{tenant, r.GetSubmission().GetTarget().GetType(), r.GetSubmission().GetTarget().GetId()}]++
	}
	return nil
}
