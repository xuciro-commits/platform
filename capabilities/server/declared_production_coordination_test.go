package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"platformserver/apps/enterprise"
	"platformserver/apps/flow"
	"platformserver/platform"
)

func TestDeclaredManufacturingCoordinatesActualBatchesAcrossTenants(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	providerSeats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("controller", "build:controller", "core:accountant"), seatOf("production", "build:production", "core:steward")}
	consumerSeats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("planner", "build:supervisor"), seatOf("operator", "build:operator"), seatOf("quality", "build:quality"), seatOf("quality2", "build:quality"), seatOf("outsider")}
	consumerSeats[1].Units = []platform.Membership{{Unit: "plant", Role: "supervisor"}}
	consumerSeats[2].Units = []platform.Membership{{Unit: "A", Role: "operator"}}
	organization := declaredPlantOrganization(consumerSeats, "A")
	var providerEntries, consumerEntries []Entry
	provider := declaredIndustryTenant(t, "erp-production-owner", "manufacturing", providerSeats, at, declaredFixtureOptions{Entries: &providerEntries})
	declaredProductionMasters(t, provider, at)
	decide(t, provider, "production", "build", "build.productionorder.create", "build.productionorder", "upstream", map[string]any{"number": "ERP-100", "product": "assembly", "policy": "standard", "quantity": 8}, at)
	for _, id := range []string{"congested", "replacement"} {
		decide(t, provider, "production", "build", "build.productionorder.create", "build.productionorder", id, map[string]any{"number": id, "product": "assembly", "policy": "standard", "quantity": 2}, at)
	}
	decide(t, provider, "controller", "build", "build.erpitem.create", "build.erpitem", "otherassembly", map[string]any{"name": "Other product", "kind": "finished", "unit": "piece", "currency": "USD", "cost": 11, "account": "acc-1405", "active": true}, at)
	decide(t, provider, "controller", "build", "build.erpcomponent.create", "build.erpcomponent", "othersteel", map[string]any{"parent": "otherassembly", "material": "steel", "quantity": 2}, at)
	decide(t, provider, "production", "build", "build.productionorder.create", "build.productionorder", "wrongproduct", map[string]any{"number": "Other product order", "product": "otherassembly", "policy": "standard", "quantity": 2}, at)
	host := NewHost(Tokens(map[string]string{"production-service": "production"}), provider)
	host.Now = func() time.Time { return at }
	handler := host.Handler()
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		handler.ServeHTTP(w, r)
	})
	consumer := declaredIndustryTenant(t, "mes-production-owner", "manufacturing", consumerSeats, at, declaredFixtureOptions{Entries: &consumerEntries, Production: upstream, Organization: &organization})
	consumer.Files = &memoryFiles{}
	consumer.PullSources(at)
	plans, failure := consumer.Records(memberOf(t, consumer, "planner"), "build.mesupstreamorder", platform.Query{Limit: 20}, at)
	if failure != nil || plans.Total != 4 {
		t.Fatalf("upstream production directory: %+v %v", plans, failure)
	}
	planIDs := map[string]string{}
	for _, row := range plans.Records {
		var plan struct{ ID, Providerid string }
		if err := json.Unmarshal(platform.Raw(row), &plan); err != nil {
			t.Fatal(err)
		}
		planIDs[plan.Providerid] = plan.ID
	}
	create := func(typ, id string, values map[string]any) {
		t.Helper()
		decide(t, consumer, "planner", "build", "build.mes"+typ+".create", "build.mes"+typ, id, values, at)
	}
	create("center", "assembly", map[string]any{"name": "Assembly", "line": "A"})
	create("resource", "station", map[string]any{"name": "Assembly station", "center": "assembly", "active": true})
	create("routing", "route", map[string]any{"code": "R-100", "name": "Assembly", "version": 1, "from": "2026-01-01"})
	create("operation", "assemble", map[string]any{"routing": "route", "number": 10, "name": "Assemble and inspect", "center": "assembly"})
	decide(t, consumer, "planner", "build", "build.mesrouting.edit", "build.mesrouting", "route", map[string]any{"first": "assemble"}, at)
	decide(t, consumer, "planner", "build", "build.mesrouting.release", "build.mesrouting", "route", nil, at)
	create("order", "shop", map[string]any{"number": "MES-100", "product": "assembly", "routing": "route", "quantity": 4, "upstream": "upstream"})
	for _, batch := range []struct {
		id  string
		qty int
	}{{"good", 3}, {"scrap", 1}} {
		create("batch", batch.id, map[string]any{"name": batch.id, "order": "shop", "quantity": batch.qty, "routing": "route", "operation": "assemble"})
	}
	decide(t, consumer, "planner", "build", "build.mesorder.release", "build.mesorder", "shop", nil, at)
	pump := func(until func() bool) {
		t.Helper()
		for i := 0; i < 16; i++ {
			if until() {
				return
			}
			at = at.Add(time.Second)
			consumer.Work(at)
			consumer.Dispatch(at)
		}
		starts, _ := consumer.Records(memberOf(t, consumer, "planner"), "build.mesproductionstart", platform.Query{}, at)
		t.Logf("starts=%s", platform.Raw(starts))
		for _, run := range platform.Records[flow.FlowInstance](consumer.automation(flow.ID, false)) {
			t.Logf("flow %s: %s, tokens=%s", run.ID, run.State, platform.Raw(run.Tokens))
		}
		for _, run := range platform.Records[platform.ConfirmationRun](consumer.automation("build", false)) {
			t.Logf("confirmation %s: %s, completion=%s", run.ID, run.State, run.CompletionError)
			for _, party := range run.Parties {
				t.Logf("party %s: %s, reason=%s", party.Name, party.State, party.Reason)
			}
		}
		t.Fatalf("coordination did not settle; order=%s batch=%s fault=%v", platform.Raw(declaredRecord(t, consumer, "planner", "build.mesorder", "shop", at)), platform.Raw(declaredRecord(t, consumer, "planner", "build.mesbatch", "good", at)), consumer.fault.Load())
	}
	start := func(id string) {
		t.Helper()
		decide(t, consumer, "operator", "build", "build.mesbatch.start", "build.mesbatch", id, map[string]any{"operation": "assemble", "resource": "station"}, at)
	}
	start("good")
	pump(func() bool {
		order := declaredRecord(t, consumer, "planner", "build.mesorder", "shop", at)
		id, _ := order["startrequest"].(string)
		return id != "" && declaredRecord(t, consumer, "planner", "build.mesproductionstart", id, at)["state"] == "confirmed"
	})
	shopkey := declaredRecord(t, consumer, "planner", "build.mesorder", "shop", at)["deliverykey"]
	if got := declaredRecord(t, provider, "production", "build.productionorder", "upstream", at); got["firstshop"] != shopkey || got["state"] != "running" {
		t.Fatalf("actual first batch did not start its independent provider: %+v", got)
	}
	decide(t, consumer, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "good", nil, at)
	posts := 0
	consumer.Outbound = func(req *http.Request, private bool) (*http.Response, error) {
		response, err := http.DefaultClient.Do(req)
		if req.Method == http.MethodPost {
			posts++
			if err == nil {
				response.Body.Close()
				return nil, io.ErrUnexpectedEOF
			}
		}
		return response, err
	}
	receipt := decide(t, consumer, "planner", "build", "build.mesorder.reportbatch", "build.mesorder", "shop", map[string]any{"execution": "good", "final": false}, at)
	resultID := receipt.GetChangeId() + "-result"
	pump(func() bool {
		return declaredRecord(t, provider, "production", "build.productionorder", "upstream", at)["yield"] == float64(3)
	})
	if got := declaredRecord(t, consumer, "planner", "build.mesproductionresult", resultID, at); got["state"] != "pending" {
		t.Fatalf("a lost answer claimed upstream confirmation: %+v", got)
	}
	beforeUnknown := txnState(consumer)
	if got := refuse(t, consumer, "planner", "build", "build.mesproductionresult.acknowledge", "build.mesproductionresult", resultID, nil, at); got == "ok" || txnState(consumer) != beforeUnknown {
		t.Fatalf("a human completed an unknown coordination without its original receipt: %s", got)
	}
	if got := refuse(t, consumer, "planner", "build", "build.mesproductionresult.correct", "build.mesproductionresult", resultID, map[string]any{"replacement": planIDs["upstream"], "correction": "An unknown reply cannot be replaced"}, at); got == "ok" || txnState(consumer) != beforeUnknown {
		t.Fatal("an unknown accepted batch changed its target before original-key reconciliation")
	}
	consumer.ReconcileConfirmations(at.Add(time.Second))
	at = at.Add(2 * time.Second)
	pump(func() bool {
		return declaredRecord(t, consumer, "planner", "build.mesproductionresult", resultID, at)["state"] == "confirmed"
	})
	if posts != 1 {
		t.Fatalf("unknown delivery resent the original accepted batch: %d posts", posts)
	}
	consumer.Outbound = nil
	before := txnState(consumer)
	if got := refuse(t, consumer, "planner", "build", "build.mesorder.reportbatch", "build.mesorder", "shop", map[string]any{"execution": "good", "final": false}, at); got == "ok" || txnState(consumer) != before {
		t.Fatal("another report repeated an accepted physical batch")
	}
	start("scrap")
	decide(t, consumer, "operator", "build", "build.mesbatch.report", "build.mesbatch", "scrap", map[string]any{"code": "Surface damage"}, at)
	decide(t, consumer, "quality", "build", "build.mesbatch.review", "build.mesbatch", "scrap", map[string]any{"decision": "scrap"}, at)
	decide(t, consumer, "quality2", "build", "build.mesbatch.approve", "build.mesbatch", "scrap", nil, at)
	receipt = decide(t, consumer, "planner", "build", "build.mesorder.reportbatch", "build.mesorder", "shop", map[string]any{"execution": "scrap", "final": true}, at)
	resultID = receipt.GetChangeId() + "-result"
	pump(func() bool {
		return declaredRecord(t, consumer, "planner", "build.mesproductionresult", resultID, at)["state"] == "confirmed"
	})
	if got := declaredRecord(t, provider, "production", "build.productionorder", "upstream", at); got["state"] != "closed" || got["yield"] != float64(3) || got["scrap"] != float64(1) || got["remaining"] != float64(4) {
		t.Fatalf("final actual batch erased shortage or physical results: %+v", got)
	}
	starts, failure := consumer.Records(memberOf(t, consumer, "planner"), "build.mesproductionstart", platform.Query{}, at)
	if failure != nil || starts.Total != 1 {
		t.Fatalf("the second execution batch restarted upstream production: %+v %v", starts, failure)
	}
	decide(t, consumer, "planner", "build", "build.mesorder.complete", "build.mesorder", "shop", nil, at)
	// Another producer consumes the shared allowance after the local plan was
	// read. ERP refuses the actual report; a supervisor creates a linked attempt.
	create("order", "correctshop", map[string]any{"number": "MES-200", "product": "assembly", "routing": "route", "quantity": 2, "upstream": "congested"})
	create("batch", "correctbatch", map[string]any{"name": "Correctable batch", "order": "correctshop", "quantity": 2, "routing": "route", "operation": "assemble"})
	decide(t, consumer, "planner", "build", "build.mesorder.release", "build.mesorder", "correctshop", nil, at)
	start("correctbatch")
	pump(func() bool {
		id, _ := declaredRecord(t, consumer, "planner", "build.mesorder", "correctshop", at)["startrequest"].(string)
		return id != "" && declaredRecord(t, consumer, "planner", "build.mesproductionstart", id, at)["state"] == "confirmed"
	})
	decide(t, provider, "production", "build", "build.productionorder.confirm", "build.productionorder", "congested", map[string]any{"shoporder": "another-producer", "batch": "external", "yield": 1, "scrap": 0, "final": false}, at)
	decide(t, consumer, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "correctbatch", nil, at)
	receipt = decide(t, consumer, "planner", "build", "build.mesorder.reportbatch", "build.mesorder", "correctshop", map[string]any{"execution": "correctbatch", "final": true}, at)
	refusedID := receipt.GetChangeId() + "-result"
	pump(func() bool {
		for _, run := range platform.Records[platform.ConfirmationRun](consumer.automation("build", false)) {
			if run.Subject == "build.mesproductionresult/"+refusedID && run.Partial {
				return true
			}
		}
		return false
	})
	before = txnState(consumer)
	for _, who := range []string{"operator", "outsider"} {
		if got := refuse(t, consumer, who, "build", "build.mesproductionresult.correct", "build.mesproductionresult", refusedID, map[string]any{"replacement": planIDs["replacement"], "correction": "Unauthorized correction"}, at); got == "ok" || txnState(consumer) != before {
			t.Fatal("an ungranted member corrected the upstream association")
		}
	}
	if got := refuse(t, consumer, "planner", "build", "build.mesproductionresult.correct", "build.mesproductionresult", refusedID, map[string]any{"replacement": planIDs["wrongproduct"], "correction": "Wrong product"}, at); got == "ok" || txnState(consumer) != before {
		t.Fatal("the correction selected an order for a different product")
	}
	beforeDraft := platform.Raw(declaredRecord(t, consumer, "planner", "build.mesproductionresult", refusedID, at))
	declaredAgentDraft(t, consumer, "planner", "CORRECTION", "build.erpcorrection", "build.mesproductionresult.correct", refusedID, map[string]any{"replacement": planIDs["replacement"], "correction": "Shared allowance consumed; use the replacement production plan"}, at)
	if string(beforeDraft) != string(platform.Raw(declaredRecord(t, consumer, "planner", "build.mesproductionresult", refusedID, at))) {
		t.Fatal("the AI draft corrected a business result before human confirmation")
	}
	decide(t, consumer, "planner", AgentApp, SchemaRunConfirm, RunType, "CORRECTION", map[string]any{}, at)
	corrections, failure := consumer.Records(memberOf(t, consumer, "planner"), "build.mesproductionresult", platform.Query{Domain: platform.Raw([]any{[]any{"previous", "=", refusedID}})}, at)
	if failure != nil || corrections.Total != 1 {
		t.Fatalf("human confirmation did not create one linked correction: %+v %v", corrections, failure)
	}
	var identity struct{ ID string }
	if err := json.Unmarshal(platform.Raw(corrections.Records[0]), &identity); err != nil {
		t.Fatal(err)
	}
	correctedID := identity.ID
	pump(func() bool {
		return declaredRecord(t, consumer, "planner", "build.mesproductionresult", correctedID, at)["state"] == "confirmed"
	})
	old, next := declaredRecord(t, consumer, "planner", "build.mesproductionresult", refusedID, at), declaredRecord(t, consumer, "planner", "build.mesproductionresult", correctedID, at)
	if old["state"] != "superseded" || old["upstream"] != "congested" || next["previous"] != refusedID || next["upstream"] != "replacement" || old["batch"] != next["batch"] || next["attempt"] != float64(2) {
		t.Fatalf("correction lost the rejected association or actual batch identity: old=%+v next=%+v", old, next)
	}
	if got := declaredRecord(t, consumer, "planner", "build.mesbatch", "correctbatch", at); got["good"] != float64(2) || got["scrap"] != float64(0) || got["state"] != "done" {
		t.Fatal("upstream correction rewrote physical execution")
	}
	if got := declaredRecord(t, provider, "production", "build.productionorder", "congested", at); got["yield"] != float64(1) || got["remaining"] != float64(1) {
		t.Fatal("a refused attempt changed the original provider order")
	}
	if got := declaredRecord(t, provider, "production", "build.productionorder", "replacement", at); got["yield"] != float64(2) || got["state"] != "closed" {
		t.Fatalf("the corrected provider did not accept the actual batch: %+v", got)
	}
	for _, pair := range []struct {
		tn      *Tenant
		seats   []Seat
		entries []Entry
	}{{provider, providerSeats, providerEntries}, {consumer, consumerSeats, consumerEntries}} {
		var owners []platform.App
		if pair.tn == consumer {
			owners = append(owners, enterprise.New(pair.tn.ID, organization))
		}
		restored := composeDeclaredTenant(t, pair.tn.ID, pair.seats, owners...)
		if err := restored.Replay(pair.entries); err != nil || snapshot(pair.tn) != snapshot(restored) {
			t.Fatalf("independent recovery changed accepted production facts: %v", err)
		}
	}
}
