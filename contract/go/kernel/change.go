package kernel

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type ChangeArchive interface {
	Records(tenant string, through int) []*pb.ChangeRecord
	Accepted(tenant, key string, through int) *pb.ChangeRecord
	Contains(tenant, change string, through int) bool
}

type ChangeLog struct {
	schemas  interface{ Accepts(*pb.SchemaRef) bool }
	logs     map[string][]*pb.ChangeRecord
	byKey    map[string]map[string]*pb.ChangeRecord
	byID     map[string]map[string]bool
	revs     map[[3]string]uint32
	archive  ChangeArchive
	archived map[string]int
	last     map[string]time.Time
	tail     map[string]*pb.ChangeRecord
	next     int
	Facts    func(tenant, factID string) bool
}

func NewChangeLog(schemas interface{ Accepts(*pb.SchemaRef) bool }) *ChangeLog {
	return &ChangeLog{schemas: schemas, logs: map[string][]*pb.ChangeRecord{},
		byKey: map[string]map[string]*pb.ChangeRecord{}, byID: map[string]map[string]bool{}, revs: map[[3]string]uint32{}}
}

func (l *ChangeLog) index(record *pb.ChangeRecord) {
	tenant := record.GetSubmission().GetTenantId()
	if l.byKey[tenant] == nil {
		l.byKey[tenant] = map[string]*pb.ChangeRecord{}
	}
	if l.byID[tenant] == nil {
		l.byID[tenant] = map[string]bool{}
	}
	l.byKey[tenant][record.GetSubmission().GetIdempotencyKey()] = record
	l.byID[tenant][record.GetChangeId()] = true
}

func (l *ChangeLog) Records(tenant string) []*pb.ChangeRecord {
	var out []*pb.ChangeRecord
	if l.archive != nil {
		out = l.archive.Records(tenant, l.archived[tenant])
	}
	return append(out, l.logs[tenant]...)
}

func (l *ChangeLog) accepted(tenant, key string) *pb.ChangeRecord {
	if r := l.byKey[tenant][key]; r != nil {
		return r
	}
	if l.archive != nil {
		return l.archive.Accepted(tenant, key, l.archived[tenant])
	}
	return nil
}

func (l *ChangeLog) Accepted(tenant, key string) *pb.ChangeRecord {
	r := l.accepted(tenant, key)
	if r == nil {
		return nil
	}
	return proto.Clone(r).(*pb.ChangeRecord)
}
func (l *ChangeLog) Last(tenant string) *pb.ChangeRecord {
	r := l.tail[tenant]
	if records := l.logs[tenant]; len(records) > 0 {
		r = records[len(records)-1]
	}
	if r == nil {
		return nil
	}
	return proto.Clone(r).(*pb.ChangeRecord)
}

func (l *ChangeLog) Compact(archive ChangeArchive) {
	if l.archived == nil {
		l.archived = map[string]int{}
	}
	if l.last == nil {
		l.last = map[string]time.Time{}
	}
	if l.tail == nil {
		l.tail = map[string]*pb.ChangeRecord{}
	}
	for tenant, records := range l.logs {
		l.archived[tenant] += len(records)
		if len(records) > 0 {
			l.last[tenant] = records[len(records)-1].GetRecordedTime().AsTime()
			l.tail[tenant] = records[len(records)-1]
		}
	}
	l.archive = archive
	l.logs = map[string][]*pb.ChangeRecord{}
	l.byKey = map[string]map[string]*pb.ChangeRecord{}
	l.byID = map[string]map[string]bool{}
}

func (l *ChangeLog) lastTime(tenant string) time.Time {
	if log := l.logs[tenant]; len(log) > 0 {
		return log[len(log)-1].GetRecordedTime().AsTime()
	}
	return l.last[tenant]
}

func (l *ChangeLog) Fork() *ChangeLog {
	copy := &ChangeLog{schemas: l.schemas, logs: make(map[string][]*pb.ChangeRecord, len(l.logs)),
		byKey: make(map[string]map[string]*pb.ChangeRecord, len(l.byKey)), byID: make(map[string]map[string]bool, len(l.byID)), revs: make(map[[3]string]uint32, len(l.revs)), next: l.next, Facts: l.Facts, archive: l.archive, archived: map[string]int{}, last: map[string]time.Time{}, tail: map[string]*pb.ChangeRecord{}}
	for tenant, n := range l.archived {
		copy.archived[tenant] = n
	}
	for tenant, records := range l.logs {
		copy.logs[tenant] = slices.Clone(records)
	}
	for tenant, keys := range l.byKey {
		copy.byKey[tenant] = make(map[string]*pb.ChangeRecord, len(keys))
		for key, record := range keys {
			copy.byKey[tenant][key] = record
		}
	}
	for tenant, ids := range l.byID {
		copy.byID[tenant] = make(map[string]bool, len(ids))
		for id := range ids {
			copy.byID[tenant][id] = true
		}
	}
	for tenant, r := range l.tail {
		copy.tail[tenant] = r
	}
	for tenant, at := range l.last {
		copy.last[tenant] = at
	}
	for target, revision := range l.revs {
		copy.revs[target] = revision
	}
	return copy
}

func (l *ChangeLog) ApplyAccepted(saved *pb.ChangeRecord) (bool, error) {
	if saved == nil || saved.GetSubmission() == nil || saved.GetRecordedTime() == nil || saved.GetValidTime() == nil {
		return false, fmt.Errorf("accepted change is incomplete")
	}
	s := saved.GetSubmission()
	if s.GetTenantId() == "" || s.GetIdempotencyKey() == "" || s.GetTarget().GetType() == "" || s.GetTarget().GetId() == "" {
		return false, fmt.Errorf("accepted change has no scoped identity")
	}
	if prior := l.accepted(s.GetTenantId(), s.GetIdempotencyKey()); prior != nil {
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
	if saved.GetRecordedTime().AsTime().Before(l.lastTime(s.GetTenantId())) {
		return false, fmt.Errorf("accepted change %s moves recorded time backwards", saved.GetChangeId())
	}
	record := proto.Clone(saved).(*pb.ChangeRecord)
	l.next++
	l.revs[target] = record.GetRevision()
	l.logs[s.GetTenantId()] = append(log, record)
	l.index(record)
	return true, nil
}

func (l *ChangeLog) Submit(s *pb.Submission, now time.Time) (*pb.ChangeRecord, *Error) {
	return l.SubmitChecked(s, now, nil)
}

func (l *ChangeLog) SubmitChecked(s *pb.Submission, now time.Time, check func() *Error) (*pb.ChangeRecord, *Error) {
	required := []string{s.GetTenantId(), s.GetPrincipalId(), s.GetAuthority(), s.GetIdempotencyKey(),
		s.GetTarget().GetType(), s.GetTarget().GetId(), s.GetSchema().GetName()}
	if slices.Contains(required, "") {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if !l.schemas.Accepts(s.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	}
	if existing := l.accepted(s.GetTenantId(), s.GetIdempotencyKey()); existing != nil {
		if !proto.Equal(existing.GetSubmission(), s) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT)
		}
		return existing, nil
	}
	log := l.logs[s.GetTenantId()]
	if c := s.GetCausationId(); c != "" && !l.byID[s.GetTenantId()][c] && (l.archive == nil || !l.archive.Contains(s.GetTenantId(), c, l.archived[s.GetTenantId()])) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
	}
	for _, fact := range s.GetEvidenceFactIds() {
		if l.Facts == nil || !l.Facts(s.GetTenantId(), fact) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
		}
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	if s.ExpectedRevision != nil && s.GetExpectedRevision() != l.revs[target] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	if check != nil {
		if err := check(); err != nil {
			return nil, err
		}
	}
	recorded := now
	if last := l.lastTime(s.GetTenantId()); last.After(now) {
		recorded = last
	}
	valid := s.GetValidTime()
	if valid == nil {
		valid = timestamppb.New(recorded)
	}
	l.next++
	l.revs[target]++
	record := &pb.ChangeRecord{ChangeId: fmt.Sprintf("chg-%d", l.next), Submission: s,
		ValidTime: valid, RecordedTime: timestamppb.New(recorded), Revision: l.revs[target]}
	l.logs[s.GetTenantId()] = append(log, record)
	l.index(record)
	return record, nil
}

func schemaKey(s *pb.SchemaRef) string { return fmt.Sprintf("%s@%d", s.GetName(), s.GetVersion()) }

func (l *ChangeLog) Adopt(records []*pb.ChangeRecord) *Error {
	conflict := errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	if len(records) == 0 {
		return nil
	}
	tenant := records[0].GetSubmission().GetTenantId()
	if len(l.logs[tenant]) > 0 || l.tail[tenant] != nil {
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
	l.byID[tenant] = map[string]bool{}
	for _, r := range records {
		l.index(r)
		l.revs[[3]string{tenant, r.GetSubmission().GetTarget().GetType(), r.GetSubmission().GetTarget().GetId()}]++
	}
	return nil
}
