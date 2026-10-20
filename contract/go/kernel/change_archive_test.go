package kernel

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type testChangeArchive struct{ records []*pb.ChangeRecord }

func (a *testChangeArchive) Records(tenant string, through int) []*pb.ChangeRecord {
	var out []*pb.ChangeRecord
	for _, r := range a.records {
		if len(out) < through && r.GetSubmission().GetTenantId() == tenant {
			out = append(out, proto.Clone(r).(*pb.ChangeRecord))
		}
	}
	return out
}
func (a *testChangeArchive) Accepted(tenant, key string, through int) *pb.ChangeRecord {
	for _, r := range a.Records(tenant, through) {
		if r.GetSubmission().GetIdempotencyKey() == key {
			return r
		}
	}
	return nil
}
func (a *testChangeArchive) Contains(tenant, id string, through int) bool {
	for _, r := range a.Records(tenant, through) {
		if r.GetChangeId() == id {
			return true
		}
	}
	return false
}

func TestChangeArchivePreservesAcceptanceAndForkPosition(t *testing.T) {
	schemas := NewSchemaRegistry([]*pb.SchemaRef{{Name: "x.edit", Version: 1}}, nil)
	live := NewChangeLog(schemas)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	sub := func(key string) *pb.Submission {
		return &pb.Submission{TenantId: "t", PrincipalId: "m", Authority: "a", IdempotencyKey: key, Target: &pb.EntityRef{Type: "x", Id: "one"}, Schema: &pb.SchemaRef{Name: "x.edit", Version: 1}}
	}
	first, err := live.Submit(sub("one"), at)
	if err != nil {
		t.Fatal(err)
	}
	archive := &testChangeArchive{records: live.Records("t")}
	live.Compact(archive)
	if len(live.logs) != 0 || len(live.byKey) != 0 || len(live.byID) != 0 || len(live.revs) != 1 {
		t.Fatal("compaction did not release history while retaining revisions")
	}
	repeated, err := live.Submit(sub("one"), at.Add(time.Hour))
	if err != nil || !proto.Equal(repeated, first) {
		t.Fatal("historical retry changed its original receipt")
	}
	fork := live.Fork()
	request := sub("two")
	request.CausationId = first.GetChangeId()
	expected := uint32(1)
	request.ExpectedRevision = &expected
	second, err := fork.Submit(request, at.Add(-time.Hour))
	if err != nil || second.GetRevision() != 2 || second.GetChangeId() != "chg-2" || !second.GetRecordedTime().AsTime().Equal(at) {
		t.Fatalf("compacted acceptance changed: %v %v", second, err)
	}
	// SQL may already contain the committed next result while this authority is
	// still applying it. Its private prefix must not see that future receipt.
	archive.records = append(archive.records, second)
	before := live.Fork()
	future := sub("three")
	future.CausationId = "chg-2"
	if _, err := before.Submit(future, at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE {
		t.Fatalf("unapplied receipt satisfied a causal reference: %v", err)
	}
	if len(live.Records("t")) != 1 || live.Accepted("t", "two") != nil {
		t.Fatal("a discarded fork or a committed future receipt reached the source")
	}
	if applied, err := live.ApplyAccepted(second); err != nil || !applied {
		t.Fatal(err)
	}
	live.Compact(archive)
	if len(live.Records("t")) != 2 || !proto.Equal(live.Accepted("t", "two"), second) {
		t.Fatal("applied receipt was lost after compaction")
	}
	if applied, err := live.ApplyAccepted(second); err != nil || applied {
		t.Fatalf("idempotent application changed: %v", err)
	}
	// Adopted IDs belong to another authority and need not encode our counter.
	// Their archival position is their accepted order, never their ID spelling.
	adopted := proto.Clone(first).(*pb.ChangeRecord)
	adopted.ChangeId = "previous-owner-change"
	successor := NewChangeLog(schemas)
	if err := successor.Adopt([]*pb.ChangeRecord{adopted}); err != nil {
		t.Fatal(err)
	}
	successor.Compact(&testChangeArchive{records: []*pb.ChangeRecord{adopted}})
	if len(successor.Records("t")) != 1 || !proto.Equal(successor.Accepted("t", "one"), adopted) {
		t.Fatal("adopted history was lost during compaction")
	}
}
