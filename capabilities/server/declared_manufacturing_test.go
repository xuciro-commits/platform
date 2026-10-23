package platformserver

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"platformserver/apps/core"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestDeclaredAccountingDocumentsRetainTheirOriginalLinesAndCoreReceipts(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("accountant", "build:accountant", "core:accountant"), seatOf("clerk", "build:accountant"), seatOf("controller", "build:controller", "core:accountant"), seatOf("outsider")}
	var entries []Entry
	tn := declaredIndustryTenant(t, "accounting-declared", "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries})
	draft := func(id, date string) {
		t.Helper()
		decide(t, tn, "accountant", "build", "build.erpentry.create", "build.erpentry", id, map[string]any{"date": date, "currency": "USD", "reference": id}, at)
	}
	line := func(id, doc, account, side string, amount float64) {
		t.Helper()
		decide(t, tn, "accountant", "build", "build.erpline.create", "build.erpline", id, map[string]any{"document": doc, "account": "acc-" + account, "side": side, "amount": amount}, at)
	}
	draft("invoice", "2026-10-12")
	line("debitone", "invoice", "1403", "debit", 0.1)
	line("debittwo", "invoice", "1403", "debit", 0.2)
	line("credit", "invoice", "2290", "credit", 0.2)
	before := txnState(tn)
	if got := refuse(t, tn, "accountant", "build", "build.erpentry.post", "build.erpentry", "invoice", nil, at); !strings.Contains(got, "Accounting debits and credits must balance") || txnState(tn) != before {
		t.Fatal("unbalanced draft changed its document, lines or journal intent")
	}
	decide(t, tn, "accountant", "build", "build.erpline.edit", "build.erpline", "credit", map[string]any{"amount": 0.3}, at)
	if got := refuse(t, tn, "outsider", "build", "build.erpentry.post", "build.erpentry", "invoice", nil, at); got == "ok" {
		t.Fatal("an ungranted member submitted a financial document")
	}
	request := submission(tn, memberOf(t, tn, "accountant"), "build", "build.erpentry.post", "build.erpentry", "invoice", map[string]any{})
	persist := tn.AcceptResult
	fail := true
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if fail {
			fail = false
			return nil, errors.New("injected accounting append failure")
		}
		return persist(entry, key, hash)
	}
	before = txnState(tn)
	if _, err := tn.Submit(memberOf(t, tn, "accountant"), request, at); err == nil || fail || txnState(tn) != before {
		t.Fatal("uncommitted accounting submission froze or posted its draft")
	}
	receipt, failure := tn.Submit(memberOf(t, tn, "accountant"), request, at)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	again, failure := tn.Submit(memberOf(t, tn, "accountant"), request, at)
	if failure != nil || receipt.GetChangeId() != again.GetChangeId() {
		t.Fatal("the retried business submission lost its original receipt")
	}
	if declaredRecord(t, tn, "accountant", "build.erpentry", "invoice", at)["state"] != "submitted" {
		t.Fatal("the document did not retain its accepted submission state")
	}
	if journals, _, _ := platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{}); len(journals) != 0 {
		t.Fatal("a Build document wrote into Core before its independent delivery")
	}
	pending, failure := tn.RecordOf(memberOf(t, tn, "clerk"), "build.erpentry", "invoice", at)
	if failure != nil || len(pending.Deliveries) != 1 || pending.Deliveries[0].State != "pending" || pending.Deliveries[0].Receipt != "" {
		t.Fatal("the ordinary business page did not distinguish pending delivery from acceptance")
	}
	for _, change := range []struct {
		schema  string
		payload map[string]any
	}{{"edit", map[string]any{"amount": 0.4}}, {"edit", map[string]any{"document": "other"}}, {"archive", map[string]any{}}} {
		before = txnState(tn)
		if got := refuse(t, tn, "accountant", "build", "build.erpline."+change.schema, "build.erpline", "credit", change.payload, at); got == "ok" || txnState(tn) != before {
			t.Fatal("submitted financial lines could be changed or removed")
		}
	}
	if got := refuse(t, tn, "accountant", "build", "build.erpline.create", "build.erpline", "late", map[string]any{"document": "invoice", "account": "acc-1403", "side": "debit", "amount": 1}, at); got == "ok" {
		t.Fatal("a submitted accounting document accepted another line")
	}
	tn.Dispatch(at)
	journalID := "jnl-" + receipt.GetChangeId()
	journal, exists := platform.Get[platform.Journal](tn.automation(core.ID, false), journalID)
	if !exists || journal.Total != 0.3 || len(journal.Lines) != 3 || journal.Number == "" || journal.Source != "build/"+receipt.GetChangeId() || journal.ClosedPeriod != "reject" {
		t.Fatalf("the independent Core receipt does not match the original submitted lines: %+v", journal)
	}
	posted, failure := tn.RecordOf(memberOf(t, tn, "accountant"), "build.erpentry", "invoice", at)
	if failure != nil || len(posted.Deliveries) != 1 || posted.Deliveries[0].State != "delivered" || posted.Deliveries[0].Receipt != core.JournalType+"/"+journalID {
		t.Fatal("an authorized accountant could not open the original financial receipt")
	}
	restricted, failure := tn.RecordOf(memberOf(t, tn, "clerk"), "build.erpentry", "invoice", at)
	if failure != nil || len(restricted.Deliveries) != 1 || restricted.Deliveries[0].State != "delivered" || restricted.Deliveries[0].Receipt != "" {
		t.Fatal("the delivery projection bypassed the independent Core read permission")
	}
	decide(t, tn, "accountant", core.ID, core.JournalType+".reverse", core.JournalType, journalID, map[string]any{"date": "2026-10-21"}, at)
	journal, _ = platform.Get[platform.Journal](tn.automation(core.ID, false), journalID)
	if journal.State != "reversed" || journal.Reversed == "" {
		t.Fatal("Core did not keep the reversal as its own financial receipt")
	}
	decide(t, tn, "controller", core.ID, core.PeriodType+".close", core.PeriodType, "per-2026-10", nil, at)
	draft("closed", "2026-10-13")
	line("closeddebit", "closed", "1403", "debit", 1)
	line("closedcredit", "closed", "2290", "credit", 1)
	decide(t, tn, "accountant", "build", "build.erpentry.post", "build.erpentry", "closed", nil, at)
	tn.Dispatch(at)
	refused := false
	for _, effect := range tn.Effects(at) {
		if effect.Target == "build.erpentry/closed" && effect.Event == "build/journal" {
			refused = effect.State == "rejected"
		}
	}
	if !refused || declaredRecord(t, tn, "accountant", "build.erpentry", "closed", at)["state"] != "submitted" {
		t.Fatal("a closed-period refusal erased the original business fact or reported it as posted")
	}
	closed, failure := tn.RecordOf(memberOf(t, tn, "clerk"), "build.erpentry", "closed", at)
	if failure != nil || len(closed.Deliveries) != 1 || closed.Deliveries[0].State != "rejected" || closed.Deliveries[0].Message == "" || closed.Deliveries[0].Receipt != "" {
		t.Fatal("the ordinary business page hid a rejected financial delivery")
	}
	recovered := composeDeclaredTenant(t, tn.ID, seats)
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("accounting recovery changed a frozen line, intent or receipt: %v", err)
	}
}

func TestDeclaredPurchasingApprovesAndReceivesRepeatedItemLinesAtomically(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("buyer", "build:buyer", "core:"+core.ReferenceReader), seatOf("accountant", "build:accountant", "core:accountant"), seatOf("controller", "build:controller", "core:accountant")}
	var entries []Entry
	tn := declaredIndustryTenant(t, "purchasing-declared", "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries})
	decide(t, tn, "controller", core.ID, core.AccountType+".create", core.AccountType, "acc-6403", map[string]any{"code": "6403", "name": "Price variance", "kind": "expense", "parent": "6400", "posting": true, "active": true}, at)
	decide(t, tn, "controller", "build", "build.erpitem.create", "build.erpitem", "steel", map[string]any{"name": "Steel sheet", "kind": "material", "unit": "kg", "currency": "USD", "cost": 5, "account": "acc-1403", "active": true}, at)
	decide(t, tn, "controller", "build", "build.erpitem.create", "build.erpitem", "assembly", map[string]any{"name": "Finished assembly", "kind": "finished", "unit": "piece", "currency": "USD", "cost": 8, "account": "acc-1403", "active": true}, at)
	decide(t, tn, "controller", "build", "build.erpcomponent.create", "build.erpcomponent", "steelcomponent", map[string]any{"parent": "assembly", "material": "steel", "quantity": 0.5}, at)
	if got := refuse(t, tn, "controller", "build", "build.erpcomponent.create", "build.erpcomponent", "selfcomponent", map[string]any{"parent": "assembly", "material": "assembly", "quantity": 1}, at); got == "ok" {
		t.Fatal("an item could consume itself as its component")
	}
	decide(t, tn, "controller", "build", "build.erppolicy.create", "build.erppolicy", "purchasing", map[string]any{"name": "Purchasing controls", "limit": 10000, "clearing": "acc-2290", "payables": "acc-2202", "variance": "acc-6403"}, at)
	for _, kind := range []string{"supplier", "customer"} {
		decide(t, tn, "buyer", "build", "build.erppartner.create", "build.erppartner", kind, map[string]any{"name": kind, "kind": kind}, at)
	}
	draft := func(id, supplier string) {
		t.Helper()
		decide(t, tn, "buyer", "build", "build.erppurchase.create", "build.erppurchase", id, map[string]any{"supplier": supplier, "policy": "purchasing", "date": "2026-10-21", "currency": "USD"}, at)
	}
	line := func(id, purchase string, quantity, price float64) {
		t.Helper()
		decide(t, tn, "buyer", "build", "build.erppurchaseline.create", "build.erppurchaseline", id, map[string]any{"purchase": purchase, "product": "steel", "quantity": quantity, "price": price}, at)
	}
	draft("wrong", "customer")
	line("wrongline", "wrong", 10, 5)
	if got := refuse(t, tn, "buyer", "build", "build.erppurchase.order", "build.erppurchase", "wrong", nil, at); got == "ok" {
		t.Fatal("a customer was accepted as a supplier")
	}
	draft("small", "supplier")
	line("first", "small", 100, 5.2)
	line("second", "small", 20, 4.8)
	decide(t, tn, "buyer", "build", "build.erppurchase.order", "build.erppurchase", "small", nil, at)
	before := txnState(tn)
	if got := refuse(t, tn, "buyer", "build", "build.erppurchaseline.edit", "build.erppurchaseline", "first", map[string]any{"quantity": 101}, at); got == "ok" || txnState(tn) != before {
		t.Fatal("an ordered purchase line could still change")
	}
	receive := submission(tn, memberOf(t, tn, "buyer"), "build", "build.erppurchase.receive", "build.erppurchase", "small", map[string]any{})
	revision := uint32(declaredRecord(t, tn, "buyer", "build.erppurchase", "small", at)["revision"].(float64))
	receive.ExpectedRevision = &revision
	receipt, failure := tn.Submit(memberOf(t, tn, "buyer"), receive, at)
	if failure != nil {
		t.Fatalf("whole receipt: %s: %s", failure.Code, failure.Message)
	}
	again, failure := tn.Submit(memberOf(t, tn, "buyer"), receive, at)
	if failure != nil || again.GetChangeId() != receipt.GetChangeId() {
		t.Fatal("receipt retry did not preserve its original decision")
	}
	quantities := map[string]float64{}
	stock, err := tn.Records(memberOf(t, tn, "buyer"), "build.erpstock", platform.Query{Domain: platform.Raw([]any{[]any{"product", "=", "steel"}})}, at)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range stock.Records {
		var balance struct {
			Bucket string
			Amount float64
		}
		_ = json.Unmarshal(platform.Raw(record), &balance)
		quantities[balance.Bucket] = balance.Amount
	}
	if quantities["quantity"] != 120 || quantities["value"] != 600 {
		t.Fatalf("repeated item lines did not post their complete quantities and values: %+v", quantities)
	}
	if page, err := tn.Records(memberOf(t, tn, "buyer"), "build.erpmove", platform.Query{}, at); err != nil || page.Total != 2 {
		t.Fatalf("receipt movements: %d %v", page.Total, err)
	}
	if got := refuse(t, tn, "buyer", "build", "build.erppurchase.bill", "build.erppurchase", "small", map[string]any{"invoice": "INV-7"}, at); got == "ok" {
		t.Fatal("a buyer booked an accountant's supplier invoice")
	}
	decide(t, tn, "accountant", "build", "build.erppurchase.bill", "build.erppurchase", "small", map[string]any{"invoice": "INV-7"}, at)
	tn.Dispatch(at)
	for n := 1; n < 4 && len(platform.Records[platform.Journal](tn.automation(core.ID, false))) < 2; n++ {
		tn.Dispatch(at.Add(time.Duration(n) * time.Second))
	}
	balances := map[string]float64{}
	for _, journal := range platform.Records[platform.Journal](tn.automation(core.ID, false)) {
		for _, line := range journal.Lines {
			balances[line.Account] += line.Debit - line.Credit
		}
	}
	if balances["1403"] != 600 || balances["2290"] != 0 || balances["2202"] != -616 || balances["6403"] != 16 {
		t.Fatalf("receipt and invoice did not clear GR/IR and retain the price variance: %+v", balances)
	}
	draft("large", "supplier")
	line("largefirst", "large", 3000, 5)
	if got := refuse(t, tn, "buyer", "build", "build.erppurchase.order", "build.erppurchase", "large", nil, at); got == "ok" {
		t.Fatal("a large purchase bypassed its declared approval threshold")
	}
	decide(t, tn, "buyer", "build", "build.erppurchase.request", "build.erppurchase", "large", nil, at)
	if declaredRecord(t, tn, "buyer", "build.erppurchase", "large", at)["state"] != "awaiting" {
		t.Fatal("the purchase did not wait for its actual controller")
	}
	var approval work.ApprovalRequest
	for _, request := range platform.Records[work.ApprovalRequest](tn.automation(work.ID, false)) {
		if request.Target == "build.erppurchase/large" {
			approval = request
		}
	}
	if approval.ID == "" {
		t.Fatal("the controller has no original approval request")
	}
	decide(t, tn, "controller", work.ID, "work.approval.approve", work.ApprovalType, approval.ID, nil, at)
	if declaredRecord(t, tn, "buyer", "build.erppurchase", "large", at)["state"] != "ordered" {
		t.Fatal("the controller's approval did not place the original order")
	}
	decide(t, tn, "controller", "build", "build.erpitem.recost", "build.erpitem", "steel", map[string]any{"cost": 9}, at)
	moves, failure := tn.Records(memberOf(t, tn, "buyer"), "build.erpmove", platform.Query{}, at)
	if failure != nil || moves.Total != 2 {
		t.Fatal("changing standard cost changed the original receipt movements")
	}
	for _, record := range moves.Records {
		var movement struct{ Unitcost float64 }
		_ = json.Unmarshal(platform.Raw(record), &movement)
		if movement.Unitcost != 5 {
			t.Fatal("a later standard cost repriced an accepted purchase movement")
		}
	}
	recovered := composeDeclaredTenant(t, tn.ID, seats)
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("purchasing recovery changed its goods, money or approval results: %v", err)
	}
}

func TestDeclaredManufacturingPortablePlansStayInCandidateTrials(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tn := declaredIndustryTenant(t, "manufacturing-plans", "manufacturing", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("planner", "build:supervisor"), seatOf("accountant", "build:accountant", "core:accountant")}, at)
	raw, err := os.ReadFile("../../deploy/packages/manufacturing.json")
	if err != nil {
		t.Fatal(err)
	}
	var descriptor PackageDescriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		t.Fatal(err)
	}
	before := snapshot(tn)
	plans := 0
	for _, asset := range descriptor.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetTestPlan {
			continue
		}
		plan, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			t.Fatal(err)
		}
		actor := "planner"
		if plan.Name == "manufacturingaccounting" {
			actor = "accountant"
		}
		request := CandidateSimulationRequest{Roots: plan.Roots, Actors: []string{actor}, At: plan.At, Organization: plan.Organization}
		for _, step := range plan.Steps {
			request.Steps = append(request.Steps, SimulationStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect})
		}
		result, refusal := tn.SimulateCandidate(memberOf(t, tn, "builder"), request)
		if refusal != nil {
			t.Fatalf("portable plan %s: %s: %s", plan.Name, refusal.Code, refusal.Message)
		}
		if len(result.Actors) != 1 || !result.Actors[0].Recovered || len(result.Actors[0].Steps) != len(plan.Steps) {
			t.Fatalf("portable plan %s: %s", plan.Name, platform.Raw(result))
		}
		for index, step := range result.Actors[0].Steps {
			if step.Accepted != (plan.Steps[index].Expect == "accepted") {
				t.Fatalf("portable plan %s step %d: %s", plan.Name, index+1, platform.Raw(step))
			}
		}
		plans++
	}
	if plans != 3 {
		t.Fatalf("expected three portable owner plans, got %d", plans)
	}
	if snapshot(tn) != before {
		t.Fatal("candidate trials changed live business rows")
	}
}
