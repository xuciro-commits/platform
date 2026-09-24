package kernel

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// FactLog implements K2 and K3 (Contract/spec/K2-K3-facts.md).
type FactLog struct {
	schemas *SchemaRegistry
	logs    map[string][]*pb.FactRecord
	byKey   map[string]map[string]*pb.FactRecord
	next    int
}

func NewFactLog(schemas *SchemaRegistry) *FactLog {
	return &FactLog{schemas: schemas, logs: map[string][]*pb.FactRecord{},
		byKey: map[string]map[string]*pb.FactRecord{}}
}

func (l *FactLog) Records(tenant string) []*pb.FactRecord { return l.logs[tenant] }

func (l *FactLog) Record(f *pb.Fact, now time.Time) (*pb.FactRecord, *Error) {
	invalid := errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	required := []string{f.GetTenantId(), f.GetSubject().GetType(), f.GetSubject().GetId(),
		f.GetAttribute(), f.GetSchema().GetName(), f.GetIdempotencyKey()}
	if slices.Contains(required, "") || f.GetKind() == pb.FactKind_FACT_KIND_UNSPECIFIED {
		return nil, invalid // F1
	}
	p := f.GetProvenance()
	if source(p) == "" || p.GetSourceTime() == nil {
		return nil, invalid // P1
	}
	isClaim, isDerived := f.GetKind() == pb.FactKind_FACT_KIND_CLAIM, f.GetKind() == pb.FactKind_FACT_KIND_DERIVED
	if c := p.GetConfidence(); isClaim && (c <= 0 || c > 1) || !isClaim && c != 0 {
		return nil, invalid // P2
	}
	if isDerived != (len(f.GetDerivedFrom()) > 0) {
		return nil, invalid // F6
	}
	if !l.schemas.Accepts(f.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA) // F2
	}
	if existing := l.byKey[f.GetTenantId()][f.GetIdempotencyKey()]; existing != nil {
		if !proto.Equal(existing.GetFact(), f) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT) // F3
		}
		return existing, nil
	}
	log := l.logs[f.GetTenantId()]
	for _, input := range f.GetDerivedFrom() {
		if !slices.ContainsFunc(log, func(r *pb.FactRecord) bool { return r.GetFactId() == input }) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // F7
		}
	}
	recorded := now
	if n := len(log); n > 0 && log[n-1].GetRecordedTime().AsTime().After(now) {
		recorded = log[n-1].GetRecordedTime().AsTime() // P3
	}
	l.next++
	record := &pb.FactRecord{FactId: fmt.Sprintf("fact-%d", l.next), Fact: f, RecordedTime: timestamppb.New(recorded)}
	l.logs[f.GetTenantId()] = append(log, record) // F4
	if l.byKey[f.GetTenantId()] == nil {
		l.byKey[f.GetTenantId()] = map[string]*pb.FactRecord{}
	}
	l.byKey[f.GetTenantId()][f.GetIdempotencyKey()] = record
	return record, nil
}

// CurrentClaims returns, per source, its latest claim about subject and attribute, in recorded order (F5).
func (l *FactLog) CurrentClaims(tenant string, subject *pb.EntityRef, attribute string) []*pb.FactRecord {
	current := map[string]*pb.FactRecord{}
	for _, r := range l.logs[tenant] {
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
	for _, r := range l.logs[tenant] {
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
