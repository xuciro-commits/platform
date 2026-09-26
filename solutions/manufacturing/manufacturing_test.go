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
	"platformserver/apps/flow"
	"platformserver/platform"
)

const tenant = "plant-sz"

var seats = []platformserver.Seat{
	Seat("sup", "sup-1", map[string]string{"mes": string(mes.Supervisor), erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin,
		flow.ID: flow.Admin}, "plant-sz"),
	Seat("op", "op-l1", map[string]string{"mes": string(mes.Operator)}, "L1"),
}

// ADR-0024 7c: the ERP releases a production order; the plant releases a shop
// order against it, makes it, and its confirmation flow confirms it through
// production.orders/1; the ERP posts the production's cost. A confirmation the
// ERP refuses (its period is closed) reaches the plant's flow at once, and a
// supervisor's resend is accepted once the period is open again.
func TestProductionThroughTheProtocol(t *testing.T) {
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		tn, err := NewTenant(tenant, erp.New(tenant), seats...)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if err := Seed(tn, "sup-1", now); err != nil {
		t.Fatal(err)
	}
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		m, _ := tn.Member(who)
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(m, &pb.Submission{TenantId: tenant, PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %s, want %s", what, got, want)
		}
	}
	sup, _ := tn.Member("sup-1")
	order := func(id string) mes.Order {
		v, err := tn.RecordOf(sup, mes.OrderType, id, now)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return v.Record.(mes.Order)
	}
	production := func(id string) erp.Production {
		v, _ := tn.RecordOf(sup, erp.ProductionType, id, now)
		return v.Record.(erp.Production)
	}
	work := func() {
		for range 3 {
			now = now.Add(2 * time.Second)
			tn.Work(now)
		}
	}
	make := func(shop, mo string, qty, sfcs int) {
		t.Helper()
		expect("release "+shop, do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, shop,
			map[string]any{"product": "P-100", "quantity": qty, "sfcs": sfcs, "planned": mo}), "ok")
		for n := 1; n <= sfcs; n++ {
			for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
				sfc := fmt.Sprintf("%s-%03d", shop, n)
				expect("start", do("op-l1", mes.ID, mes.SchemaStart, mes.SFCType, sfc, map[string]string{"resource": resource}), "ok")
				expect("complete", do("op-l1", mes.ID, mes.SchemaComplete, mes.SFCType, sfc, map[string]string{}), "ok")
			}
		}
	}

	expect("MO-1", do("sup-1", erp.ID, erp.ProductionType+".create", erp.ProductionType, "MO-1", map[string]any{"product": "P-100", "quantity": 4}), "ok")
	expect("a draft is no planned order", do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, "SO-0",
		map[string]any{"product": "P-100", "quantity": 4, "sfcs": 2, "planned": "MO-1"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect("release MO-1", do("sup-1", erp.ID, erp.ProductionType+".release", erp.ProductionType, "MO-1", map[string]any{}), "ok")
	expect("more than planned", do("sup-1", mes.ID, mes.SchemaRelease, mes.OrderType, "SO-0",
		map[string]any{"product": "P-100", "quantity": 5, "sfcs": 1, "planned": "MO-1"}), "ERROR_CODE_INVALID_ARGUMENT")
	make("SO-1", "MO-1", 4, 2)
	work()
	o := order("SO-1")
	expect("confirmed through the protocol", o.ERP+" "+o.Confirmation, "confirmed MJ/2026/00001")
	// The order's page shows the process about it (ADR-0026 D4).
	if v, _ := tn.RecordOf(sup, mes.OrderType, "SO-1", now); len(v.Processes) != 1 || v.Processes[0].(flow.FlowInstance).State != "done" {
		t.Fatalf("processes of SO-1: %+v", v.Processes)
	}
	mo := production("MO-1")
	expect("the ERP's order", fmt.Sprint(mo.State, " ", mo.ShopOrder, " ", mo.Yield, " ", mo.Scrap), "confirmed SO-1 4 0")
	stock, _ := tn.Read(sup, erp.OnHand)
	expect("stock", fmt.Sprint(stock), "[{M-STEEL Steel kg -8 -4000} {P-100 Pump housing pcs 4 4800}]")

	// A refusal: the ERP's period is closed when the second order ends.
	expect("MO-2", do("sup-1", erp.ID, erp.ProductionType+".create", erp.ProductionType, "MO-2", map[string]any{"product": "P-100", "quantity": 1}), "ok")
	expect("release MO-2", do("sup-1", erp.ID, erp.ProductionType+".release", erp.ProductionType, "MO-2", map[string]any{}), "ok")
	expect("close", do("sup-1", erp.ID, erp.PeriodType+".close", erp.PeriodType, "2026-10", map[string]any{}), "ok")
	make("SO-2", "MO-2", 1, 1)
	work()
	expect("refused at once", order("SO-2").ERP+" "+order("SO-2").ERPDetail, "refused ERROR_CODE_INVALID_ARGUMENT")
	expect("reopen", do("sup-1", erp.ID, erp.PeriodType+".reopen", erp.PeriodType, "2026-10", map[string]any{}), "ok")
	expect("resend", do("sup-1", mes.ID, mes.SchemaResend, mes.OrderType, "SO-2", map[string]any{}), "ok")
	work()
	expect("accepted", order("SO-2").ERP+" "+order("SO-2").Confirmation+" "+production("MO-2").State, "confirmed MJ/2026/00002 confirmed")

	balances, _ := tn.Read(sup, erp.TrialBalance)
	got := ""
	for _, x := range balances.([]erp.Balance) {
		got += fmt.Sprintf("%s %d; ", x.Account, x.Balance)
	}
	// 10 kg of steel (50.00) into work in progress, 5 housings out at 12.00; the standard's 10.00 more is a variance.
	expect("books", got, "1403 -5000; 1405 6000; 5001 0; 6404 -1000; ")
	platformserver.CheckReplay(t, tn, journal, build)
	_ = platform.Money{}
}
