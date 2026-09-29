package manufacturing

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// MES declares "released-orders" once (ADR-0040 21c). Running it lists shop orders
// released to the floor and not yet completed; a member without MES is refused;
// the query is discoverable as kind "query".
func TestNamedQueryOnShopOrders(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seatSup := Seat("sup", "sup-1", map[string]string{
		mes.ID: string(mes.Supervisor), erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin,
	}, "plant-sz")
	seatOp := Seat("op", "op-1", map[string]string{mes.ID: string(mes.Operator)}, "plant-sz")
	seatOther := Seat("other", "other-1", map[string]string{erp.ID: erp.Accountant}, "plant-sz")

	tn, err := NewTenant("plant-sz", erp.New("plant-sz"), seatSup, seatOp, seatOther)
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(tn, "sup-1", now); err != nil {
		t.Fatal(err)
	}
	sup, _ := tn.Member("sup-1")
	op, _ := tn.Member("op-1")
	other, _ := tn.Member("other-1")

	keys := 0
	submit := func(m platform.Member, authority, schema, typ, id string, payload any) error {
		keys++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{
			TenantId: tn.ID, PrincipalId: m.ID, Authority: authority,
			IdempotencyKey: fmt.Sprint("q-so-", keys), Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw,
		}, now)
		if err != nil {
			return err
		}
		return nil
	}

	// Release two shop orders: SO-1 and SO-2
	for _, so := range []string{"SO-1", "SO-2"} {
		if err := submit(sup, mes.ID, mes.SchemaRelease, mes.OrderType, so, map[string]any{
			"product": "P-100", "quantity": 1, "sfcs": 1,
		}); err != nil {
			t.Fatalf("release %s: %v", so, err)
		}
	}

	// Complete SO-1 through its process routing
	for _, res := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
		if err := submit(op, mes.ID, mes.SchemaStart, mes.SFCType, "SO-1-001", map[string]string{"resource": res}); err != nil {
			t.Fatalf("start SO-1-001 on %s: %v", res, err)
		}
		if err := submit(op, mes.ID, mes.SchemaComplete, mes.SFCType, "SO-1-001", map[string]string{}); err != nil {
			t.Fatalf("complete SO-1-001 on %s: %v", res, err)
		}
	}

	// Running "released-orders" shows only SO-2 (Total == 1)
	page, kerr := tn.RunQuery(sup, mes.ID, "released-orders", "", now)
	if kerr != nil {
		t.Fatalf("run released-orders query: %v", kerr)
	}
	if page.Total != 1 {
		t.Fatalf("got total %d, want 1", page.Total)
	}

	// Member without MES role is refused
	if _, kerr := tn.RunQuery(other, mes.ID, "released-orders", "", now); kerr == nil {
		t.Fatal("expected error running query as member without MES, got nil")
	}

	// Query appears in definitions as kind "query"
	found := false
	for _, d := range tn.Definitions(sup) {
		if d.Ref == (platform.AssetRef{App: mes.ID, Kind: platform.AssetQuery, Name: "released-orders"}) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("query released-orders not found in definitions as kind query")
	}
}
