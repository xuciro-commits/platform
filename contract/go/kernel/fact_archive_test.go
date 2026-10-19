package kernel

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type testFactArchive struct{ records []*pb.FactRecord }

func (a *testFactArchive) Records(tenant string, through int) []*pb.FactRecord {
	out := []*pb.FactRecord{}
	for i, r := range a.records {
		if i < through && r.GetFact().GetTenantId() == tenant {
			out = append(out, proto.Clone(r).(*pb.FactRecord))
		}
	}
	return out
}
func (a *testFactArchive) Accepted(tenant, key string, through int) *pb.FactRecord {
	for _, r := range a.Records(tenant, through) {
		if r.GetFact().GetIdempotencyKey() == key {
			return r
		}
	}
	return nil
}
func (a *testFactArchive) Contains(tenant, id string, through int) bool {
	for _, r := range a.Records(tenant, through) {
		if r.GetFactId() == id {
			return true
		}
	}
	return false
}

func TestFactArchivePreservesClaimsAndAppliedPrefix(t *testing.T) {
	schema := &pb.SchemaRef{Name: "test.value", Version: 1}
	registry := NewSchemaRegistry([]*pb.SchemaRef{schema}, nil)
	log := NewFactLog(registry)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	fact := func(key string, source time.Time) *pb.Fact {
		return &pb.Fact{TenantId: "t", Kind: pb.FactKind_FACT_KIND_CLAIM, Subject: &pb.EntityRef{Type: "x", Id: "one"}, Attribute: "value", Schema: schema, IdempotencyKey: key, Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: "source"}, SourceTime: timestamppb.New(source), Confidence: 1}}
	}
	first, err := log.Record(fact("one", at), at)
	if err != nil {
		t.Fatal(err)
	}
	archive := &testFactArchive{records: log.Records("t")}
	log.Compact(archive)
	if len(log.logs) != 0 || len(log.byKey) != 0 {
		t.Fatal("fact history remained resident")
	}
	if repeated, err := log.Record(fact("one", at), at.Add(time.Hour)); err != nil || !proto.Equal(repeated, first) {
		t.Fatalf("historical retry changed: %v", err)
	}
	future := NewFactLog(registry)
	future.Restore("t", archive.records)
	second, err := future.Record(fact("two", at.Add(time.Second)), at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	archive.records = append(archive.records, second)
	derived := fact("derived", at)
	derived.Kind = pb.FactKind_FACT_KIND_DERIVED
	derived.Provenance.Confidence = 0
	derived.DerivedFrom = []string{second.GetFactId()}
	if _, err := log.Record(derived, at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE {
		t.Fatalf("unapplied future fact became a valid source: %v", err)
	}
	got, err := log.Record(fact("two", at.Add(time.Second)), at.Add(-time.Hour))
	if err != nil || !proto.Equal(got, second) {
		t.Fatalf("fact identity/time changed: %v %v", got, err)
	}
	claims := log.CurrentClaims("t", first.GetFact().GetSubject(), "value")
	if len(claims) != 1 || !proto.Equal(claims[0], second) {
		t.Fatalf("current claims changed: %v", claims)
	}
	log.Compact(archive)
	if len(log.Records("t")) != 2 || !log.Contains("t", first.GetFactId()) || len(log.logs) != 0 {
		t.Fatal("applied fact prefix changed after compaction")
	}
}
