package kernel

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func TestChangeLogStagedAcceptedResult(t *testing.T) {
	schemas := NewSchemaRegistry([]*pb.SchemaRef{{Name: "x.edit", Version: 1}}, nil)
	live := NewChangeLog(schemas)
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	sub := func(key string, payload []byte) *pb.Submission {
		return &pb.Submission{TenantId: "tenant", PrincipalId: "member", Authority: "app", IdempotencyKey: key,
			Target: &pb.EntityRef{Type: "x", Id: "one"}, Schema: &pb.SchemaRef{Name: "x.edit", Version: 1}, Payload: payload}
	}
	first := sub("key-1", []byte(`{"value":1}`))
	staged := live.Fork()
	accepted, err := staged.Submit(first, now)
	if err != nil || accepted.GetChangeId() != "chg-1" || accepted.GetRevision() != 1 {
		t.Fatalf("staged decision: %v %v", accepted, err)
	}
	if len(live.Records("tenant")) != 0 { // an append failure may discard the fork
		t.Fatal("uncommitted decision reached the live log")
	}
	if applied, err := live.ApplyAccepted(accepted); err != nil || !applied {
		t.Fatal(err)
	}
	if applied, err := live.ApplyAccepted(accepted); err != nil || applied || len(live.Records("tenant")) != 1 {
		t.Fatalf("idempotent application: %v, %d records", err, len(live.Records("tenant")))
	}
	if repeated, err := live.Submit(first, now.Add(time.Minute)); err != nil || !proto.Equal(repeated, accepted) {
		t.Fatalf("duplicate did not return the saved receipt: %v %v", repeated, err)
	}
	if _, err := live.Submit(sub("key-1", []byte(`{"value":2}`)), now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
		t.Fatalf("changed input reused the key: %v", err)
	}
	second, err := live.Fork().Submit(sub("key-2", []byte(`{"value":2}`)), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := live.ApplyAccepted(second); err != nil || !applied || second.GetRevision() != 2 {
		t.Fatalf("next accepted revision: %v, %v", second, err)
	}
	gap := proto.Clone(second).(*pb.ChangeRecord)
	gap.ChangeId = "chg-9"
	gap.Submission = sub("key-9", []byte(`{"value":9}`))
	gap.Revision = 3
	if _, err := live.ApplyAccepted(gap); err == nil {
		t.Fatal("a missing predecessor was accepted")
	}
	conflict := proto.Clone(second).(*pb.ChangeRecord)
	conflict.Submission = sub("key-2", []byte(`{"value":99}`))
	if _, err := live.ApplyAccepted(conflict); err == nil {
		t.Fatal("a conflicting saved receipt was accepted")
	}
	if len(live.Records("tenant")) != 2 {
		t.Fatal("a rejected saved result changed the log")
	}
	second.Revision = 99 // the caller does not own the accepted log's copy
	if live.Records("tenant")[1].GetRevision() != 2 {
		t.Fatal("a caller mutated the committed record")
	}
}
