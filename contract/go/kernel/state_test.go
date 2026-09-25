package kernel

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// A restored change log answers as the one it was saved from: a repeated
// submission gets its original record, revisions continue, and new change IDs
// follow the restored ones (ADR-0019 D6).
func TestChangeLogRestore(t *testing.T) {
	schemas := NewSchemaRegistry([]*pb.SchemaRef{{Name: "s", Version: 1}}, nil)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	sub := func(key string, expected *uint32) *pb.Submission {
		return &pb.Submission{TenantId: "t", PrincipalId: "p", Authority: "a", IdempotencyKey: key,
			Target: &pb.EntityRef{Type: "x", Id: "1"}, Schema: &pb.SchemaRef{Name: "s", Version: 1}, ExpectedRevision: expected}
	}
	live := NewChangeLog(schemas)
	live.Submit(sub("k1", nil), now)
	live.Submit(sub("k2", nil), now)
	restored := NewChangeLog(schemas)
	restored.Restore("t", live.Records("t"))
	again, err := restored.Submit(sub("k1", nil), now)
	if err != nil || again.GetChangeId() != "chg-1" {
		t.Fatalf("replayed key: %v %v", again, err)
	}
	two := uint32(2)
	next, err := restored.Submit(sub("k3", &two), now)
	if err != nil || next.GetChangeId() != "chg-3" || next.GetRevision() != 3 {
		t.Fatalf("next: %v %v", next, err)
	}
}

func TestIdentityAndConnectorsRestore(t *testing.T) {
	id := NewIdentity([]*pb.EntityRef{{Type: "d", Id: "a"}, {Type: "d", Id: "b"}})
	id.AddRedirect(&pb.Redirect{From: &pb.EntityRef{Type: "d", Id: "a"}, To: []*pb.EntityRef{{Type: "d", Id: "b"}}, Kind: pb.RedirectKind_REDIRECT_KIND_MERGE})
	restored := NewIdentity(nil)
	restored.Restore(id.State())
	if refs, err := restored.Resolve(&pb.EntityRef{Type: "d", Id: "a"}); err != nil || len(refs) != 1 || refs[0].ID != "b" {
		t.Fatalf("resolve %v %v", refs, err)
	}
	c := NewConnectors()
	c.Register(&pb.ConnectorDescriptor{TenantId: "t", ConnectorId: "erp", DataClasses: []string{"x"}, Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_POLL})
	c.Deliver("t", "erp", "x", "", "page-1", time.Now())
	r := NewConnectors()
	r.Restore(c.State())
	if err := r.Deliver("t", "erp", "x", "", "page-2", time.Now()); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("the restored cursor is lost: %v", err)
	}
	if err := r.Deliver("t", "erp", "x", "page-1", "page-2", time.Now()); err != nil {
		t.Fatal(err)
	}
}
