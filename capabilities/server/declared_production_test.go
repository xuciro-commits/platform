package platformserver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"platformserver/apps/core"
	"platformserver/platform"
)

func TestDeclaredProductionFreezesComponentsAndBooksEachAcceptedBatch(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("controller", "build:controller", "core:accountant"), seatOf("production", "build:production", "core:steward"), seatOf("outsider")}
	var entries []Entry
	tn := declaredIndustryTenant(t, "production-declared", "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries})
	declaredProductionMasters(t, tn, at)
	create := func(id string) {
		t.Helper()
		decide(t, tn, "production", "build", "build.productionorder.create", "build.productionorder", id, map[string]any{"number": id, "product": "assembly", "policy": "standard", "quantity": 10}, at)
	}
	create("first")
	// A master change applies only to new orders, never to the saved material issue basis.
	decide(t, tn, "controller", "build", "build.erpcomponent.edit", "build.erpcomponent", "steelpart", map[string]any{"quantity": 3}, at)
	decide(t, tn, "controller", "build", "build.erpitem.recost", "build.erpitem", "steel", map[string]any{"cost": 9}, at)
	create("second")
	snapshots, failure := tn.Records(memberOf(t, tn, "production"), "build.erpproductioncomponent", platform.Query{Limit: 100}, at)
	if failure != nil || snapshots.Total != 2 {
		t.Fatalf("component snapshots: %+v %v", snapshots, failure)
	}
	for _, record := range snapshots.Records {
		var component struct {
			Order          string
			Quantity, Cost float64
		}
		if err := json.Unmarshal(platform.Raw(record), &component); err != nil {
			t.Fatal(err)
		}
		quantity, cost := 2.0, 5.0
		if component.Order == "second" {
			quantity, cost = 3, 9
		}
		if component.Quantity != quantity || component.Cost != cost {
			t.Fatalf("new and in-flight orders did not retain their own material basis: %+v", component)
		}
	}
	first := declaredRecord(t, tn, "production", "build.productionorder", "first", at)
	if first["componentcount"] != float64(1) || first["unitcost"] != float64(11) {
		t.Fatalf("creation did not freeze original costing: %+v", first)
	}
	before := txnState(tn)
	if got := refuse(t, tn, "production", "build", "build.erpproductioncomponent.create", "build.erpproductioncomponent", "extra", map[string]any{"order": "first", "source": "steelpart", "material": "steel", "quantity": 10, "cost": 5, "unit": "kg", "currency": "USD", "account": "acc-1403"}, at); got == "ok" || before != txnState(tn) {
		t.Fatal("a manually appended component changed a frozen order")
	}
	if got := refuse(t, tn, "production", "build", "build.productionorder.confirm", "build.productionorder", "first", map[string]any{"shoporder": "W1", "batch": "A", "yield": 3, "scrap": 1, "final": false}, at); got == "ok" {
		t.Fatal("production was reported before its first shop started")
	}
	decide(t, tn, "production", "build", "build.productionorder.start", "build.productionorder", "first", map[string]any{"shoporder": "W1"}, at)
	decide(t, tn, "production", "build", "build.productionorder.start", "build.productionorder", "first", map[string]any{"shoporder": "W2"}, at)
	if declaredRecord(t, tn, "production", "build.productionorder", "first", at)["firstshop"] != "W1" {
		t.Fatal("another shop overwrote the first starter")
	}
	request := submission(tn, memberOf(t, tn, "production"), "build", "build.productionorder.confirm", "build.productionorder", "first", map[string]any{"shoporder": "W1", "batch": "A", "yield": 3, "scrap": 1, "final": false})
	persist := tn.AcceptResult
	fail := true
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if fail {
			fail = false
			return nil, errors.New("injected production append failure")
		}
		return persist(e, key, hash)
	}
	before = txnState(tn)
	if _, err := tn.Submit(memberOf(t, tn, "production"), request, at); err == nil || fail || txnState(tn) != before {
		t.Fatal("an unaccepted batch left production, inventory or financial facts")
	}
	accepted, err, issues := tn.submitDiagnosed(memberOf(t, tn, "production"), request, at)
	if err != nil {
		t.Fatalf("%s issues=%+v", err.Message, issues)
	}
	again, err := tn.Submit(memberOf(t, tn, "production"), request, at)
	if err != nil || accepted.GetChangeId() != again.GetChangeId() {
		t.Fatal("the same batch request did not return its original receipt")
	}
	before = txnState(tn)
	if got := refuse(t, tn, "production", "build", "build.productionorder.confirm", "build.productionorder", "first", map[string]any{"shoporder": "W1", "batch": "A", "yield": 3, "scrap": 1, "final": false}, at); got == "ok" || txnState(tn) != before {
		t.Fatal("a new request repeated an accepted batch identity")
	}
	if got := refuse(t, tn, "production", "build", "build.productionorder.confirm", "build.productionorder", "first", map[string]any{"shoporder": "W2", "batch": "B", "yield": 7, "scrap": 0, "final": false}, at); got == "ok" || txnState(tn) != before {
		t.Fatal("an over-remaining report left inventory or accounting facts")
	}
	decide(t, tn, "production", "build", "build.productionorder.confirm", "build.productionorder", "first", map[string]any{"shoporder": "W2", "batch": "A", "yield": 6, "scrap": 0, "final": false}, at)
	first = declaredRecord(t, tn, "production", "build.productionorder", "first", at)
	if first["state"] != "closed" || first["yield"] != float64(9) || first["scrap"] != float64(1) || first["remaining"] != float64(0) {
		t.Fatalf("cumulative batch result: %+v", first)
	}
	balances, failure := tn.Records(memberOf(t, tn, "production"), "build.erpstock", platform.Query{Limit: 100}, at)
	if failure != nil || balances.Total != 4 {
		t.Fatalf("unified inventory: %+v %v", balances, failure)
	}
	for _, raw := range balances.Records {
		var value map[string]any
		if e := json.Unmarshal(platform.Raw(raw), &value); e != nil {
			t.Fatal(e)
		}
		expected := map[string]float64{"steel/quantity": -20, "steel/value": -100, "assembly/quantity": 9, "assembly/value": 99}[value["product"].(string)+"/"+value["bucket"].(string)]
		if value["amount"] != expected {
			t.Fatalf("inventory used changed master data: %+v", value)
		}
	}
	tn.Dispatch(at)
	for n := 1; n < 4 && len(platform.Records[platform.Journal](tn.automation(core.ID, false))) < 2; n++ {
		tn.Dispatch(at.Add(time.Duration(n) * time.Second))
	}
	journals, _, _ := platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{})
	if len(journals) != 2 {
		t.Fatalf("want one Core journal per accepted batch, got %+v", journals)
	}
	totals := map[string]float64{}
	for _, journal := range journals {
		for _, line := range journal.Lines {
			totals[line.Account] += line.Debit - line.Credit
		}
	}
	for code, want := range map[string]float64{"1403": -100, "1405": 99, "5001": 0, "6711": 11, "6404": -10} {
		if totals[code] != want {
			t.Fatalf("account %s = %v, want %v", code, totals[code], want)
		}
	}
	restored := composeDeclaredTenant(t, tn.ID, seats)
	if e := restored.Replay(entries); e != nil || snapshot(tn) != snapshot(restored) {
		t.Fatalf("production recovery changed saved snapshots or batch facts: %v", e)
	}
}

func declaredProductionMasters(t *testing.T, tn *Tenant, at time.Time) {
	t.Helper()
	for _, account := range []struct{ code, name, kind, parent string }{{"5001", "Work in progress", "asset", ""}, {"6711", "Production scrap", "expense", "6400"}, {"6404", "Production variance", "expense", "6400"}} {
		decide(t, tn, "controller", core.ID, core.AccountType+".create", core.AccountType, "acc-"+account.code, map[string]any{"code": account.code, "name": account.name, "kind": account.kind, "parent": account.parent, "posting": true, "active": true}, at)
	}
	for _, item := range []struct {
		id, kind, unit, account string
		cost                    float64
	}{{"steel", "material", "kg", "1403", 5}, {"assembly", "finished", "piece", "1405", 11}} {
		decide(t, tn, "controller", "build", "build.erpitem.create", "build.erpitem", item.id, map[string]any{"name": item.id, "kind": item.kind, "unit": item.unit, "currency": "USD", "cost": item.cost, "account": "acc-" + item.account, "active": true}, at)
	}
	decide(t, tn, "controller", "build", "build.erpcomponent.create", "build.erpcomponent", "steelpart", map[string]any{"parent": "assembly", "material": "steel", "quantity": 2}, at)
	decide(t, tn, "controller", "build", "build.erpproductionpolicy.create", "build.erpproductionpolicy", "standard", map[string]any{"name": "Standard cost accounting", "wip": "acc-5001", "scrap": "acc-6711", "variance": "acc-6404"}, at)
}
