package platform

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type receiptRuntime struct {
	Runtime
	published int
}

func (*receiptRuntime) Probing() bool                      { return false }
func (r *receiptRuntime) Publish(Caller, *pb.ChangeRecord) { r.published++ }

func TestAcceptedNoopPublishesOnceWithoutMutationCallback(t *testing.T) {
	ledger := NewLedger("tenant", "owner", NewCatalog(Action{Schema: "owner.reason", Target: "owner.item"}), "owner.item")
	runtime := &receiptRuntime{}
	caller := NewCaller(runtime, Member{ID: "member", Tenant: "tenant"}, "owner", false, true)
	sub := &pb.Submission{TenantId: "tenant", PrincipalId: "member", Authority: "owner", IdempotencyKey: "reason", Target: &pb.EntityRef{Type: "owner.item", Id: "I"}, Schema: &pb.SchemaRef{Name: "owner.reason", Version: 1}}
	decisions := 0
	for range 2 {
		if _, err := ledger.Receive(caller, sub, time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC), nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			decisions++
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if decisions != 1 || runtime.published != 1 || len(ledger.RecordsFor("tenant")) != 1 {
		t.Fatalf("no-op accepted receipt lost or republished: decisions=%d publications=%d", decisions, runtime.published)
	}
}

func TestHistoricalNoopReceiptDoesNotInventPublication(t *testing.T) {
	ledger := NewLedger("tenant", "owner", NewCatalog(Action{Schema: "owner.reason", Target: "owner.item"}), "owner.item")
	runtime := &receiptRuntime{}
	caller := NewCaller(runtime, Member{ID: "member", Tenant: "tenant"}, "owner", true, false)
	sub := &pb.Submission{TenantId: "tenant", PrincipalId: "member", Authority: "owner", IdempotencyKey: "historical-reason", Target: &pb.EntityRef{Type: "owner.item", Id: "I"}, Schema: &pb.SchemaRef{Name: "owner.reason", Version: 1}}
	for range 2 {
		if _, err := ledger.Receive(caller, sub, time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.published != 0 || len(ledger.RecordsFor("tenant")) != 1 {
		t.Fatal("historical nil-callback receipt invented an event or lost its ledger")
	}
}
