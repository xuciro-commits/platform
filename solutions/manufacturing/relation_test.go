package manufacturing

import (
	"encoding/json"
	"testing"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

// A shop order lists its released SFCs under the declared inverse relation "sfcs"
// (ADR-0040 21b D1). A member with no MES role cannot read the order.
func TestNamedRelationOnShopOrder(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seatSup := Seat("sup", "sup-1", map[string]string{mes.ID: string(mes.Supervisor), erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin}, "plant-sz")
	seatOther := Seat("other", "other-1", map[string]string{erp.ID: erp.Accountant}, "plant-sz")
	tn, err := NewTenant("plant-sz", erp.New("plant-sz"), seatSup, seatOther)
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(tn, "sup-1", now); err != nil {
		t.Fatal(err)
	}
	sup, _ := tn.Member("sup-1")
	other, _ := tn.Member("other-1")

	raw, _ := json.Marshal(map[string]any{"product": "P-100", "quantity": 4, "sfcs": 2})
	if _, err := tn.Submit(sup, &pb.Submission{
		TenantId: tn.ID, PrincipalId: sup.ID, Authority: mes.ID,
		IdempotencyKey: "rel-so-1", Target: &pb.EntityRef{Type: mes.OrderType, Id: "SO-1"},
		Schema: &pb.SchemaRef{Name: mes.SchemaRelease, Version: 1}, Payload: raw,
	}, now); err != nil {
		t.Fatalf("release shop order: %v", err)
	}

	view, kerr := tn.RecordOf(sup, mes.OrderType, "SO-1", now)
	if kerr != nil {
		t.Fatalf("record of SO-1: %v", kerr)
	}
	found := false
	for _, r := range view.Related {
		if r.Type == mes.SFCType {
			found = true
			if r.Relation != "sfcs" {
				t.Errorf("got relation %q, want %q", r.Relation, "sfcs")
			}
			if r.Total != 2 {
				t.Errorf("got total %d, want 2", r.Total)
			}
		}
	}
	if !found {
		t.Fatalf("view.Related missing mes.sfc: %+v", view.Related)
	}

	// Member with no MES role gets an error (not found/denied) reading the shop order.
	if _, kerr := tn.RecordOf(other, mes.OrderType, "SO-1", now); kerr == nil {
		t.Fatal("expected error reading SO-1 without MES role, got nil")
	}
}
