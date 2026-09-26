package erp

import (
	"fmt"
	"testing"

	"platformserver"
)

// A planner releases a production order for a finished product with its
// components; a confirmation over the quantity is refused; an accepted one
// issues the components, receives the yield and writes off the scrap at
// standard cost, the rest of work in progress a variance, and stock follows.
func TestProduction(t *testing.T) {
	b := newBooks(t)
	b.expect("currency", b.as(platformserver.PlatformApp, "cy", platformserver.SchemaSettingSet, platformserver.SettingType,
		platformserver.PlatformApp+"/"+platformserver.SettingCurrency, map[string]string{"value": "CNY"}), "ok")
	for _, a := range Chart() {
		b.do("cy", AccountType+".create", AccountType, a.ID, map[string]string{"name": a.Name, "kind": a.Kind})
	}
	b.do("cy", SchemaPeriodOpen, PeriodType, "2026-10", map[string]any{})
	cost := func(v int64) map[string]any { return map[string]any{"amount": v, "currency": "CNY"} }
	b.expect("steel", b.do("cy", ProductType+".create", ProductType, "M-1", map[string]any{"name": "Steel", "kind": "material", "unit": "kg", "cost": cost(500)}), "ok")
	b.expect("bracket", b.do("cy", ProductType+".create", ProductType, "P-100", map[string]any{"name": "Bracket", "kind": "finished", "unit": "pcs",
		"cost": cost(1100), "components": []any{map[string]any{"product": "M-1", "quantity": 2}}}), "ok")

	b.expect("draft", b.do("pi", ProductionType+".create", ProductionType, "MO-1", map[string]any{"product": "P-100", "quantity": 10, "due": "2026-10-20"}), "ok")
	b.expect("no material is made", b.do("pi", ProductionType+".create", ProductionType, "MO-X", map[string]any{"product": "M-1", "quantity": 1}), "ok")
	b.expect("release a material", b.do("pi", ProductionType+".release", ProductionType, "MO-X", map[string]any{}), "ERROR_CODE_INVALID_ARGUMENT")
	b.expect("release", b.do("pi", ProductionType+".release", ProductionType, "MO-1", map[string]any{}), "ok")
	confirm := func(yield, scrap float64) string {
		return b.do("pi", SchemaProduction, ProductionType, "MO-1", map[string]any{"shopOrder": "SO-7", "yield": yield, "scrap": scrap})
	}
	b.expect("more than ordered", confirm(10, 1), "ERROR_CODE_INVALID_ARGUMENT")
	b.expect("a released order is not edited", b.do("pi", ProductionType+".edit", ProductionType, "MO-1", map[string]any{"quantity": 20}), "ERROR_CODE_POLICY_DENIED")
	b.expect("confirm", confirm(9, 1), "ok")
	b.expect("twice", confirm(9, 1), "ERROR_CODE_CONFLICT")

	m, _ := b.tn.Member("cy")
	v, _ := b.tn.RecordOf(m, ProductionType, "MO-1", b.now)
	o := v.Record.(Production)
	b.expect("confirmed", fmt.Sprint(o.State, " ", o.Number, " ", o.ShopOrder, " ", o.Yield, " ", o.Scrap, " ", b.entry("MO-1-C").Number),
		"confirmed MO/2026/00001 SO-7 9 1 MJ/2026/00001")
	stock, _ := b.tn.Read(m, OnHand)
	b.expect("stock", fmt.Sprint(stock), "[{M-1 Steel kg -20 -10000} {P-100 Bracket pcs 9 9900}]")
	balances, _ := b.tn.Read(m, TrialBalance)
	got := ""
	for _, x := range balances.([]Balance) {
		got += fmt.Sprintf("%s %d; ", x.Account, x.Balance)
	}
	// 20 kg of steel (100.00) into work in progress; 9 brackets out (99.00), 1 scrapped (11.00): 10.00 more than the components, a variance.
	b.expect("books", got, "1403 -10000; 1405 9900; 5001 0; 6404 -1000; 6711 1100; ")
	platformserver.CheckReplay(t, b.tn, b.journal, func() *platformserver.Tenant { return build(t) })
}
