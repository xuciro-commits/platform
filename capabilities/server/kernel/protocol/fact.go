package protocol

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/values"
	"platformserver/platform"
)

type FactLog struct {
	schemas interface{ Accepts(*pb.SchemaRef) bool }
	tenants values.Index[tenantFacts]
	next    int
}

type tenantFacts struct {
	tail   *factLink
	count  int
	keys   values.Index[*pb.FactRecord]
	claims values.Index[*factLink]
}

type factLink struct {
	record *pb.FactRecord
	prior  *factLink
}

func (t *tenantFacts) append(r *pb.FactRecord) {
	f := r.GetFact()
	t.tail = &factLink{record: r, prior: t.tail}
	t.count++
	t.keys.Set(f.GetIdempotencyKey(), r)
	if f.GetKind() == pb.FactKind_FACT_KIND_CLAIM {
		key := strconv.Quote(RefOf(f.GetSubject()).key()) + f.GetAttribute()
		t.claims.Set(key, &factLink{record: r, prior: t.claims.Get(key)})
	}
}

func NewFactLog(schemas interface{ Accepts(*pb.SchemaRef) bool }) *FactLog {
	return &FactLog{schemas: schemas}
}

func (l *FactLog) Records(tenant string) []*pb.FactRecord {
	log := l.tenants.Get(tenant)
	if log.count == 0 {
		return nil
	}
	out := make([]*pb.FactRecord, log.count)
	for i, link := len(out)-1, log.tail; link != nil; i, link = i-1, link.prior {
		out[i] = link.record
	}
	return out
}

func (l *FactLog) Accepted(tenant, key string) *pb.FactRecord {
	return l.tenants.Get(tenant).keys.Get(key)
}

func (l *FactLog) Contains(tenant, id string) bool {
	for link := l.tenants.Get(tenant).tail; link != nil; link = link.prior {
		if link.record.GetFactId() == id {
			return true
		}
	}
	return false
}

func (l *FactLog) Record(f *pb.Fact, now time.Time) (*pb.FactRecord, *platform.Error) {
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
	log := l.tenants.Get(f.GetTenantId())
	existing := log.keys.Get(f.GetIdempotencyKey())
	if existing != nil {
		if !proto.Equal(existing.GetFact(), f) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT)
		}
		return existing, nil
	}
	for _, input := range f.GetDerivedFrom() {
		if !l.Contains(f.GetTenantId(), input) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
		}
	}
	recorded := now
	if log.tail != nil && log.tail.record.GetRecordedTime().AsTime().After(now) {
		recorded = log.tail.record.GetRecordedTime().AsTime()
	}
	l.next++
	record := &pb.FactRecord{FactId: fmt.Sprintf("fact-%d", l.next), Fact: f, RecordedTime: timestamppb.New(recorded)}
	log.append(record)
	l.tenants.Set(f.GetTenantId(), log)
	return record, nil
}

func (l *FactLog) CurrentClaims(tenant string, subject *pb.EntityRef, attribute string) []*pb.FactRecord {
	current := map[string]*pb.FactRecord{}
	head := l.tenants.Get(tenant).claims.Get(strconv.Quote(RefOf(subject).key()) + attribute)
	for link := head; link != nil; link = link.prior {
		r := link.record
		f := r.GetFact()
		if !proto.Equal(f.GetSubject(), subject) {
			continue
		}
		key := source(f.GetProvenance())
		if old := current[key]; old == nil || f.GetProvenance().GetSourceTime().AsTime().After(old.GetFact().GetProvenance().GetSourceTime().AsTime()) {
			current[key] = r
		}
	}
	var result []*pb.FactRecord
	for link := head; link != nil; link = link.prior {
		r := link.record
		if current[source(r.GetFact().GetProvenance())] == r {
			result = append(result, r)
		}
	}
	slices.Reverse(result)
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
