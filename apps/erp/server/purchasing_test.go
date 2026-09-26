package erp

import (
	"fmt"
	"testing"

	"platformserver"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A buyer orders from a supplier; the order above the approval limit waits for
// a controller. Receiving posts stock at standard cost against goods received
// not invoiced, with the price variance; the bill clears goods received not
// invoiced against payables. On hand and the accounts agree with the moves.
func TestPurchasing(t *testing.T) {
	b := newBooks(t)
	b.expect("currency", b.as(platformserver.PlatformApp, "cy", platformserver.SchemaSettingSet, platformserver.SettingType,
		platformserver.PlatformApp+"/"+platformserver.SettingCurrency, map[string]string{"value": "CNY"}), "ok")
	for _, a := range Chart() {
		b.do("cy", AccountType+".create", AccountType, a.ID, map[string]string{"name": a.Name, "kind": a.Kind})
	}
	b.expect("period", b.do("cy", SchemaPeriodOpen, PeriodType, "2026-10", map[string]any{}), "ok")
	b.expect("supplier", b.do("bo", PartnerType+".create", PartnerType, "S-1", map[string]string{"name": "Steel Co", "role": "supplier"}), "ok")
	b.expect("customer", b.do("bo", PartnerType+".create", PartnerType, "C-1", map[string]string{"name": "Buyer Ltd", "role": "customer"}), "ok")
	b.expect("a buyer keeps no products", b.do("bo", ProductType+".create", ProductType, "M-1", map[string]any{"name": "Steel", "kind": "material", "unit": "kg"}), "ERROR_CODE_POLICY_DENIED")
	b.expect("product", b.do("cy", ProductType+".create", ProductType, "M-1",
		map[string]any{"name": "Steel sheet", "kind": "material", "unit": "kg", "cost": map[string]any{"amount": 500, "currency": "CNY"}}), "ok")

	line := func(product string, qty float64, price int64) map[string]any {
		return map[string]any{"product": product, "quantity": qty, "price": map[string]any{"amount": price}}
	}
	draft := func(id, supplier string, lines ...map[string]any) {
		t.Helper()
		b.expect("draft "+id, b.do("bo", PurchaseType+".create", PurchaseType, id, map[string]any{"supplier": supplier, "date": "2026-10-05", "lines": lines}), "ok")
	}
	purchase := func(id string) Purchase {
		m, _ := b.tn.Member("cy")
		v, err := b.tn.RecordOf(m, PurchaseType, id, b.now)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return v.Record.(Purchase)
	}

	draft("PO-X", "C-1", line("M-1", 10, 520))
	b.expect("only from a supplier", b.do("bo", PurchaseType+".order", PurchaseType, "PO-X", map[string]any{}), "ERROR_CODE_INVALID_ARGUMENT") // the rules are probed before any request
	draft("PO-A", "S-1", line("M-1", 100, 520))
	// Every order goes through the work app's request; under the limit no level applies and it is placed at once.
	b.expect("order", b.do("bo", PurchaseType+".order", PurchaseType, "PO-A", map[string]any{}), work.SchemaRequest)
	a := purchase("PO-A")
	b.expect("ordered", fmt.Sprint(a.State, " ", a.Number, " ", a.Total.Amount, " ", a.Total.Currency), "ordered PO/2026/00001 52000 CNY")

	// 3 000 kg at 5.00: 15 000 CNY, over the limit of 10 000: a controller approves first.
	draft("PO-B", "S-1", line("M-1", 3000, 500))
	b.expect("held", b.do("bo", PurchaseType+".order", PurchaseType, "PO-B", map[string]any{}), work.SchemaRequest)
	b.expect("while held", purchase("PO-B").State, "draft")
	m, _ := b.tn.Member("bo")
	out, _ := b.tn.Read(m, "requests")
	var request string
	for _, r := range out.([]work.ApprovalRequest) {
		if r.Target == PurchaseType+"/PO-B" {
			request = r.ID
		}
	}
	b.expect("approve", b.as(work.ID, "cy", "work.approval.approve", work.ApprovalType, request, map[string]any{}), "ok")
	b.expect("approved and placed", purchase("PO-B").State+" "+purchase("PO-B").Number, "ordered PO/2026/00002")

	b.expect("receive", b.do("bo", PurchaseType+".receive", PurchaseType, "PO-A", map[string]any{}), "ok")
	b.expect("a buyer books no bill", b.do("bo", PurchaseType+".bill", PurchaseType, "PO-A", map[string]string{"invoice": "INV-7"}), "ERROR_CODE_POLICY_DENIED")
	b.expect("bill", b.do("ada", PurchaseType+".bill", PurchaseType, "PO-A", map[string]string{"invoice": "INV-7"}), "ok")
	b.expect("billed", purchase("PO-A").State+" "+purchase("PO-A").Invoice+" "+b.entry("PO-A-GR").Number+" "+b.entry("PO-A-IV").Number,
		"billed INV-7 PJ/2026/00001 PJ/2026/00002")

	ada, _ := b.tn.Member("ada")
	stock, _ := b.tn.Read(ada, OnHand)
	b.expect("on hand", fmt.Sprint(stock), "[{M-1 Steel sheet kg 100 50000}]")
	balances, _ := b.tn.Read(ada, TrialBalance)
	got := ""
	for _, x := range balances.([]Balance) {
		got += fmt.Sprintf("%s %d; ", x.Account, x.Balance)
	}
	// Stock at standard (500.00), the price variance (20.00), payables (520.00); goods received not invoiced cleared.
	b.expect("books", got, "1403 50000; 2202 -52000; 2203 0; 6403 2000; ")
	platformserver.CheckReplay(t, b.tn, b.journal, func() *platformserver.Tenant { return build(t) })
	_ = platform.Money{}
}
