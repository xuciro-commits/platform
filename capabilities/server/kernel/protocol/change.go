package protocol

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

const changeLogMaxDepth = 16

type changeLogBase struct {
	parent    *changeLogBase
	depth     int
	compacted bool
	logs      map[string][]*pb.ChangeRecord
	byKey     map[string]map[string]*pb.ChangeRecord
	byID      map[string]map[string]bool
	revs      map[[3]string]uint32
	archived  map[string]int
	last      map[string]time.Time
	tail      map[string]*pb.ChangeRecord
}

type ChangeLog struct {
	schemas   interface{ Accepts(*pb.SchemaRef) bool }
	base      *changeLogBase
	compacted bool
	logs      map[string][]*pb.ChangeRecord
	byKey     map[string]map[string]*pb.ChangeRecord
	byID      map[string]map[string]bool
	revs      map[[3]string]uint32
	archive   platform.ChangeArchive
	archived  map[string]int
	last      map[string]time.Time
	tail      map[string]*pb.ChangeRecord
	next      int
	Facts     func(tenant, factID string) bool
}

func NewChangeLog(schemas interface{ Accepts(*pb.SchemaRef) bool }) *ChangeLog {
	return &ChangeLog{schemas: schemas, logs: map[string][]*pb.ChangeRecord{},
		byKey: map[string]map[string]*pb.ChangeRecord{}, byID: map[string]map[string]bool{}, revs: map[[3]string]uint32{}}
}

func (l *ChangeLog) ensureWritable() {
	if l.logs == nil {
		l.logs = map[string][]*pb.ChangeRecord{}
	}
	if l.byKey == nil {
		l.byKey = map[string]map[string]*pb.ChangeRecord{}
	}
	if l.byID == nil {
		l.byID = map[string]map[string]bool{}
	}
	if l.revs == nil {
		l.revs = map[[3]string]uint32{}
	}
}

func (l *ChangeLog) index(record *pb.ChangeRecord) {
	l.ensureWritable()
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

func (l *ChangeLog) archivedCount(tenant string) int {
	if l.compacted {
		return l.archived[tenant]
	}
	total := l.archived[tenant]
	for b := l.base; b != nil; b = b.parent {
		total += b.archived[tenant]
		if b.compacted {
			break
		}
	}
	return total
}

func (l *ChangeLog) Records(tenant string) []*pb.ChangeRecord {
	var out []*pb.ChangeRecord
	if l.archive != nil {
		out = l.archive.Records(tenant, l.archivedCount(tenant))
	}
	if !l.compacted && l.base != nil {
		var chain []*changeLogBase
		for b := l.base; b != nil; b = b.parent {
			chain = append(chain, b)
			if b.compacted {
				break
			}
		}
		for i := len(chain) - 1; i >= 0; i-- {
			if recs := chain[i].logs[tenant]; len(recs) > 0 {
				out = append(out, recs...)
			}
		}
	}
	return append(out, l.logs[tenant]...)
}

func (l *ChangeLog) accepted(tenant, key string) *pb.ChangeRecord {
	if r := l.byKey[tenant][key]; r != nil {
		return r
	}
	if !l.compacted {
		for b := l.base; b != nil; b = b.parent {
			if r := b.byKey[tenant][key]; r != nil {
				return r
			}
			if b.compacted {
				break
			}
		}
	}
	if l.archive != nil {
		return l.archive.Accepted(tenant, key, l.archivedCount(tenant))
	}
	return nil
}

func (l *ChangeLog) hasChangeID(tenant, id string) bool {
	if l.byID[tenant][id] {
		return true
	}
	if !l.compacted {
		for b := l.base; b != nil; b = b.parent {
			if b.byID[tenant][id] {
				return true
			}
			if b.compacted {
				break
			}
		}
	}
	return false
}

func (l *ChangeLog) revision(target [3]string) uint32 {
	if rev, ok := l.revs[target]; ok {
		return rev
	}
	for b := l.base; b != nil; b = b.parent {
		if rev, ok := b.revs[target]; ok {
			return rev
		}
	}
	return 0
}

func (l *ChangeLog) Accepted(tenant, key string) *pb.ChangeRecord {
	r := l.accepted(tenant, key)
	if r == nil {
		return nil
	}
	return proto.Clone(r).(*pb.ChangeRecord)
}

func (l *ChangeLog) Last(tenant string) *pb.ChangeRecord {
	var r *pb.ChangeRecord
	if records := l.logs[tenant]; len(records) > 0 {
		r = records[len(records)-1]
	} else if t := l.tail[tenant]; t != nil {
		r = t
	} else {
		for b := l.base; b != nil; b = b.parent {
			if records := b.logs[tenant]; len(records) > 0 {
				r = records[len(records)-1]
				break
			}
			if t := b.tail[tenant]; t != nil {
				r = t
				break
			}
		}
	}
	if r == nil {
		return nil
	}
	return proto.Clone(r).(*pb.ChangeRecord)
}

func (l *ChangeLog) Compact(archive platform.ChangeArchive) {
	archived := map[string]int{}
	last := map[string]time.Time{}
	tail := map[string]*pb.ChangeRecord{}
	var chain []*changeLogBase
	if !l.compacted {
		for b := l.base; b != nil; b = b.parent {
			chain = append(chain, b)
			if b.compacted {
				break
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		for tenant, n := range b.archived {
			archived[tenant] += n
		}
		for tenant, at := range b.last {
			last[tenant] = at
		}
		for tenant, r := range b.tail {
			tail[tenant] = r
		}
		for tenant, records := range b.logs {
			archived[tenant] += len(records)
			if len(records) > 0 {
				last[tenant] = records[len(records)-1].GetRecordedTime().AsTime()
				tail[tenant] = records[len(records)-1]
			}
		}
	}
	for tenant, n := range l.archived {
		archived[tenant] += n
	}
	for tenant, at := range l.last {
		last[tenant] = at
	}
	for tenant, r := range l.tail {
		tail[tenant] = r
	}
	for tenant, records := range l.logs {
		archived[tenant] += len(records)
		if len(records) > 0 {
			last[tenant] = records[len(records)-1].GetRecordedTime().AsTime()
			tail[tenant] = records[len(records)-1]
		}
	}
	l.archive = archive
	l.archived = archived
	l.last = last
	l.tail = tail
	l.compacted = true
	l.logs = nil
	l.byKey = nil
	l.byID = nil
}

func (l *ChangeLog) lastTime(tenant string) time.Time {
	if log := l.logs[tenant]; len(log) > 0 {
		return log[len(log)-1].GetRecordedTime().AsTime()
	}
	if at, ok := l.last[tenant]; ok {
		return at
	}
	for b := l.base; b != nil; b = b.parent {
		if log := b.logs[tenant]; len(log) > 0 {
			return log[len(log)-1].GetRecordedTime().AsTime()
		}
		if at, ok := b.last[tenant]; ok {
			return at
		}
	}
	return time.Time{}
}

func (l *ChangeLog) hasLocalChanges() bool {
	return l.compacted || len(l.logs) > 0 || len(l.revs) > 0 || len(l.archived) > 0 || len(l.last) > 0 || len(l.tail) > 0
}

func (l *ChangeLog) flattenToBase() *changeLogBase {
	var all []*changeLogBase
	for b := l.base; b != nil; b = b.parent {
		all = append(all, b)
	}
	out := &changeLogBase{
		depth:    1,
		logs:     map[string][]*pb.ChangeRecord{},
		byKey:    map[string]map[string]*pb.ChangeRecord{},
		byID:     map[string]map[string]bool{},
		revs:     map[[3]string]uint32{},
		archived: map[string]int{},
		last:     map[string]time.Time{},
		tail:     map[string]*pb.ChangeRecord{},
	}
	applyLayer := func(compacted bool, logs map[string][]*pb.ChangeRecord, byKey map[string]map[string]*pb.ChangeRecord, byID map[string]map[string]bool, revs map[[3]string]uint32, archived map[string]int, last map[string]time.Time, tail map[string]*pb.ChangeRecord) {
		if compacted {
			out.compacted = true
			out.logs = map[string][]*pb.ChangeRecord{}
			out.byKey = map[string]map[string]*pb.ChangeRecord{}
			out.byID = map[string]map[string]bool{}
			out.archived = map[string]int{}
			out.last = map[string]time.Time{}
			out.tail = map[string]*pb.ChangeRecord{}
		}
		for tenant, n := range archived {
			out.archived[tenant] += n
		}
		for tenant, at := range last {
			out.last[tenant] = at
		}
		for tenant, r := range tail {
			out.tail[tenant] = r
		}
		for tenant, recs := range logs {
			out.logs[tenant] = append(out.logs[tenant], recs...)
		}
		for tenant, keys := range byKey {
			if out.byKey[tenant] == nil {
				out.byKey[tenant] = make(map[string]*pb.ChangeRecord, len(keys))
			}
			maps.Copy(out.byKey[tenant], keys)
		}
		for tenant, ids := range byID {
			if out.byID[tenant] == nil {
				out.byID[tenant] = make(map[string]bool, len(ids))
			}
			maps.Copy(out.byID[tenant], ids)
		}
		maps.Copy(out.revs, revs)
	}
	for i := len(all) - 1; i >= 0; i-- {
		b := all[i]
		applyLayer(b.compacted, b.logs, b.byKey, b.byID, b.revs, b.archived, b.last, b.tail)
	}
	applyLayer(l.compacted, l.logs, l.byKey, l.byID, l.revs, l.archived, l.last, l.tail)
	return out
}

func (l *ChangeLog) Fork() platform.ChangeLog {
	if l.hasLocalChanges() {
		var snap *changeLogBase
		if l.base != nil && l.base.depth >= changeLogMaxDepth {
			snap = l.flattenToBase()
		} else {
			depth := 1
			if l.base != nil {
				depth = l.base.depth + 1
			}
			snap = &changeLogBase{
				parent:    l.base,
				depth:     depth,
				compacted: l.compacted,
				logs:      l.logs,
				byKey:     l.byKey,
				byID:      l.byID,
				revs:      l.revs,
				archived:  l.archived,
				last:      l.last,
				tail:      l.tail,
			}
		}
		l.base = snap
		l.compacted = false
		l.logs = nil
		l.byKey = nil
		l.byID = nil
		l.revs = nil
		l.archived = nil
		l.last = nil
		l.tail = nil
	}
	return &ChangeLog{
		schemas: l.schemas,
		base:    l.base,
		next:    l.next,
		Facts:   l.Facts,
		archive: l.archive,
	}
}

func (l *ChangeLog) ForkWithSchemas(schemas interface{ Accepts(*pb.SchemaRef) bool }) *ChangeLog {
	fork := l.Fork().(*ChangeLog)
	fork.schemas = schemas
	return fork
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
	prevRev := l.revision(target)
	if saved.GetRevision() != prevRev+1 {
		return false, fmt.Errorf("accepted change %s has revision %d after %d", saved.GetChangeId(), saved.GetRevision(), prevRev)
	}
	if saved.GetRecordedTime().AsTime().Before(l.lastTime(s.GetTenantId())) {
		return false, fmt.Errorf("accepted change %s moves recorded time backwards", saved.GetChangeId())
	}
	l.ensureWritable()
	record := proto.Clone(saved).(*pb.ChangeRecord)
	l.next++
	l.revs[target] = record.GetRevision()
	l.logs[s.GetTenantId()] = append(l.logs[s.GetTenantId()], record)
	l.index(record)
	return true, nil
}

func sameSubmission(accepted, incoming *pb.Submission) bool {
	if proto.Equal(accepted, incoming) {
		return true
	}
	if incoming.GetTarget().GetId() != "" || incoming.GetSchema().GetName() != incoming.GetTarget().GetType()+".create" {
		return false
	}
	clone := proto.Clone(incoming).(*pb.Submission)
	clone.Target.Id = accepted.GetTarget().GetId()
	return proto.Equal(accepted, clone)
}

func (l *ChangeLog) SubmitChecked(s *pb.Submission, now time.Time, authorize, check func() *platform.Error) (*pb.ChangeRecord, *platform.Error) {
	emptyCreate := s.GetTarget().GetId() == "" && s.GetSchema().GetName() == s.GetTarget().GetType()+".create"
	required := []string{s.GetTenantId(), s.GetPrincipalId(), s.GetAuthority(), s.GetIdempotencyKey(),
		s.GetTarget().GetType(), s.GetSchema().GetName()}
	if !emptyCreate {
		required = append(required, s.GetTarget().GetId())
	}
	if slices.Contains(required, "") {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if !l.schemas.Accepts(s.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	}
	if existing := l.accepted(s.GetTenantId(), s.GetIdempotencyKey()); existing != nil {
		if !sameSubmission(existing.GetSubmission(), s) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT)
		}
		return existing, nil
	}
	if c := s.GetCausationId(); c != "" && !l.hasChangeID(s.GetTenantId(), c) && (l.archive == nil || !l.archive.Contains(s.GetTenantId(), c, l.archivedCount(s.GetTenantId()))) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
	}
	for _, fact := range s.GetEvidenceFactIds() {
		if l.Facts == nil || !l.Facts(s.GetTenantId(), fact) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
		}
	}
	if err := authorize(); err != nil {
		return nil, err
	}
	if !emptyCreate {
		target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
		revision := l.revision(target)
		if s.ExpectedRevision != nil && s.GetExpectedRevision() != revision {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "The record changed since you opened it", CurrentRevision: &revision}
		}
	}
	if check != nil {
		if err := check(); err != nil {
			return nil, err
		}
	}
	if s.GetTarget().GetId() == "" {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	revision := l.revision(target)
	if s.ExpectedRevision != nil && s.GetExpectedRevision() != revision {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "The record changed since you opened it", CurrentRevision: &revision}
	}
	recorded := now
	if last := l.lastTime(s.GetTenantId()); last.After(now) {
		recorded = last
	}
	valid := s.GetValidTime()
	if valid == nil {
		valid = timestamppb.New(recorded)
	}
	l.ensureWritable()
	l.next++
	revision++
	l.revs[target] = revision
	record := &pb.ChangeRecord{ChangeId: fmt.Sprintf("chg-%d", l.next), Submission: s,
		ValidTime: valid, RecordedTime: timestamppb.New(recorded), Revision: revision}
	l.logs[s.GetTenantId()] = append(l.logs[s.GetTenantId()], record)
	l.index(record)
	return record, nil
}

func schemaKey(s *pb.SchemaRef) string { return fmt.Sprintf("%s@%d", s.GetName(), s.GetVersion()) }
