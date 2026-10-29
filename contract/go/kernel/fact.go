package kernel

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type FactArchive interface {
	Records(tenant string, through int) []*pb.FactRecord
	Accepted(tenant, key string, through int) *pb.FactRecord
	Contains(tenant, id string, through int) bool
}

type FactLog struct {
	schemas  *SchemaRegistry
	logs     map[string][]*pb.FactRecord
	byKey    map[string]map[string]*pb.FactRecord
	next     int
	archive  FactArchive
	archived map[string]int
	last     map[string]time.Time
}

func NewFactLog(schemas *SchemaRegistry) *FactLog {
	return &FactLog{schemas: schemas, logs: map[string][]*pb.FactRecord{},
		byKey: map[string]map[string]*pb.FactRecord{}}
}

func (l *FactLog) Records(tenant string) []*pb.FactRecord {
	if l.archive == nil {
		return l.logs[tenant]
	}
	return append(l.archive.Records(tenant, l.archived[tenant]), l.logs[tenant]...)
}

func (l *FactLog) Compact(archive FactArchive) {
	if l.archived == nil {
		l.archived = map[string]int{}
	}
	if l.last == nil {
		l.last = map[string]time.Time{}
	}
	for tenant, records := range l.logs {
		l.archived[tenant] += len(records)
		if len(records) > 0 {
			l.last[tenant] = records[len(records)-1].GetRecordedTime().AsTime()
		}
	}
	l.archive = archive
	l.logs = map[string][]*pb.FactRecord{}
	l.byKey = map[string]map[string]*pb.FactRecord{}
}

func (l *FactLog) Contains(tenant, id string) bool {
	return slices.ContainsFunc(l.logs[tenant], func(r *pb.FactRecord) bool { return r.GetFactId() == id }) || l.archive != nil && l.archive.Contains(tenant, id, l.archived[tenant])
}

func (l *FactLog) Record(f *pb.Fact, now time.Time) (*pb.FactRecord, *Error) {
	invalid := errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	required := []string{f.GetTenantId(), f.GetSubject().GetType(), f.GetSubject().GetId(),
		f.GetAttribute(), f.GetSchema().GetName(), f.GetIdempotencyKey()}
	if slices.Contains(required, "") || f.GetKind() == pb.FactKind_FACT_KIND_UNSPECIFIED {
		return nil, invalid
	}
	p := f.GetProvenance()
	if source(p) == "" || p.GetSourceTime() == nil {
		return nil, invalid
	}
	isClaim, isDerived := f.GetKind() == pb.FactKind_FACT_KIND_CLAIM, f.GetKind() == pb.FactKind_FACT_KIND_DERIVED
	if c := p.GetConfidence(); isClaim && (c <= 0 || c > 1) || !isClaim && c != 0 {
		return nil, invalid
	}
	if isDerived != (len(f.GetDerivedFrom()) > 0) {
		return nil, invalid
	}
	if !l.schemas.Accepts(f.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	}
	existing := l.byKey[f.GetTenantId()][f.GetIdempotencyKey()]
	if existing == nil && l.archive != nil {
		existing = l.archive.Accepted(f.GetTenantId(), f.GetIdempotencyKey(), l.archived[f.GetTenantId()])
	}
	if existing != nil {
		if !proto.Equal(existing.GetFact(), f) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT)
		}
		return existing, nil
	}
	log := l.logs[f.GetTenantId()]
	for _, input := range f.GetDerivedFrom() {
		if !l.Contains(f.GetTenantId(), input) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
		}
	}
	recorded := now
	if l.last[f.GetTenantId()].After(recorded) {
		recorded = l.last[f.GetTenantId()]
	}
	if n := len(log); n > 0 && log[n-1].GetRecordedTime().AsTime().After(now) {
		recorded = log[n-1].GetRecordedTime().AsTime()
	}
	l.next++
	record := &pb.FactRecord{FactId: fmt.Sprintf("fact-%d", l.next), Fact: f, RecordedTime: timestamppb.New(recorded)}
	l.logs[f.GetTenantId()] = append(log, record)
	if l.byKey[f.GetTenantId()] == nil {
		l.byKey[f.GetTenantId()] = map[string]*pb.FactRecord{}
	}
	l.byKey[f.GetTenantId()][f.GetIdempotencyKey()] = record
	return record, nil
}

func (l *FactLog) CurrentClaims(tenant string, subject *pb.EntityRef, attribute string) []*pb.FactRecord {
	current := map[string]*pb.FactRecord{}
	records := l.Records(tenant)
	for _, r := range records {
		f := r.GetFact()
		if f.GetKind() != pb.FactKind_FACT_KIND_CLAIM || f.GetAttribute() != attribute || !proto.Equal(f.GetSubject(), subject) {
			continue
		}
		key := source(f.GetProvenance())
		if old := current[key]; old == nil || !old.GetFact().GetProvenance().GetSourceTime().AsTime().After(f.GetProvenance().GetSourceTime().AsTime()) {
			current[key] = r
		}
	}
	var result []*pb.FactRecord
	for _, r := range records {
		if current[source(r.GetFact().GetProvenance())] == r {
			result = append(result, r)
		}
	}
	return result
}

func source(p *pb.Provenance) string {
	if id := p.GetPrincipalId(); id != "" {
		return "principal:" + id
	}
	if id := p.GetConnectorId(); id != "" {
		return "connector:" + id
	}
	return ""
}
