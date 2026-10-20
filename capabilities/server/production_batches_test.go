package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestProductionBatchPackagesCoordinateIndependentTenants(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	load := func(name string) (PackageDescriptor, *PackageIndex) {
		t.Helper()
		raw, err := os.ReadFile("../../deploy/packages/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var descriptor PackageDescriptor
		if err = json.Unmarshal(raw, &descriptor); err != nil {
			t.Fatal(err)
		}
		directory := t.TempDir()
		if err = os.WriteFile(filepath.Join(directory, name+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		index, err := LoadPackageIndex(directory)
		if err != nil {
			t.Fatal(err)
		}
		return descriptor, index
	}
	providerPackage, providerIndex := load("production-provider")
	consumerPackage, consumerIndex := load("production-consumer")
	compose := func(id string, index *PackageIndex) *Tenant {
		tn := composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("outsider")}, build.New(id), core.New(id))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	provider := compose("production-provider", providerIndex)
	consumer := compose("production-consumer", consumerIndex)
	var providerEntries, consumerEntries []Entry
	provider.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		providerEntries = append(providerEntries, e)
		return e.Body, nil
	}
	consumer.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		consumerEntries = append(consumerEntries, e)
		return e.Body, nil
	}
	host := NewHost(Tokens(map[string]string{"operator": "operator", "outsider": "outsider"}), provider)
	host.Now = func() time.Time { return at }
	handler := host.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	consumer.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer operator"), name == "production-upstream" }
	install := func(tn *Tenant, pkg PackageDescriptor, configure func()) []construction.JointDraftRef {
		t.Helper()
		decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, pkg.ID, map[string]any{"id": pkg.ID, "version": pkg.Version}, at)
		drafts, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), pkg.ID, pkg.Version, "production-drafts", at)
		if err != nil {
			t.Fatal(err)
		}
		if configure != nil {
			configure()
		}
		var selection []construction.JointDraftRef
		for _, draft := range drafts.Drafts {
			selection = append(selection, construction.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID})
		}
		preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), selection)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("production candidate: %v %s", err, preview.Diagnostic)
		}
		candidate, err := tn.SaveReleaseCandidates(memberOf(t, tn, "builder"), selection, preview.CandidateID, "production-candidate", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tn.ActivateRelease(memberOf(t, tn, "builder"), candidate, "activate-production", at); err != nil {
			t.Fatal(err)
		}
		return selection
	}
	install(provider, providerPackage, nil)
	install(consumer, consumerPackage, func() {
		decide(t, consumer, "builder", construction.ID, construction.ConnectionType+".edit", construction.ConnectionType, "productionupstream", map[string]any{"address": server.URL, "secret": "production-upstream", "allowPrivate": true}, at)
		decide(t, consumer, "builder", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "productionupstream", map[string]any{}, at)
		consumer.CheckConnections(at)
	})
	read := func(tn *Tenant, typ, id string) map[string]any {
		t.Helper()
		view, refusal := tn.RecordOf(memberOf(t, tn, "operator"), typ, id, at)
		if refusal != nil {
			t.Fatal(refusal)
		}
		var record map[string]any
		if err := json.Unmarshal(platform.Raw(view.Record), &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	createUpstream := func(id string, quantity int) {
		decide(t, provider, "operator", construction.ID, "build.productionorder.create", "build.productionorder", id, map[string]any{"number": id, "product": "P-100", "quantity": quantity, "unitcost": 10, "yield": 0, "scrap": 0}, at)
	}
	createShop := func(id, upstream string, quantity int) {
		decide(t, consumer, "operator", construction.ID, "build.shoporder.create", "build.shoporder", id, map[string]any{"number": id, "order": upstream, "product": "P-100", "quantity": quantity, "yield": 0, "scrap": 0}, at)
	}
	advance := func() {
		t.Helper()
		at = at.Add(time.Second)
		consumer.Dispatch(at)
		at = at.Add(time.Second)
		runJob(t, consumer, construction.ID, "confirmations", at)
	}
	start := func(shop, order string) {
		decide(t, consumer, "operator", construction.ID, "build.shoporder.start", "build.shoporder", shop, map[string]any{"order": order}, at)
		if state := read(consumer, "build.shoporder", shop)["state"]; state != "pending" {
			t.Fatalf("start lacked independent pending state: %v", state)
		}
		advance()
		if state := read(consumer, "build.shoporder", shop)["state"]; state != "running" {
			t.Fatalf("start not acknowledged: %v fault=%v", state, consumer.fault.Load())
		}
	}
	confirm := func(shop, order, batch string, good, scrap int, final bool) {
		member := memberOf(t, consumer, "operator")
		if _, refusal, issues := consumer.submitDiagnosed(member, submission(consumer, member, construction.ID, "build.shoporder.confirm", "build.shoporder", shop, map[string]any{"order": order, "batch": batch, "yield": good, "scrap": scrap, "final": final}), at); refusal != nil {
			t.Fatalf("local confirmation: %v issues=%+v", refusal, issues)
		}
		advance()
	}
	createUpstream("P1", 10)
	createShop("W1", "P1", 4)
	createShop("W2", "P1", 6)
	start("W1", "P1")
	if got := read(provider, "build.productionorder", "P1"); got["state"] != "running" || got["firstshop"] != "W1" {
		t.Fatalf("first start not retained: %+v", got)
	}
	confirm("W1", "P1", "A", 3, 1, false)
	if got := read(provider, "build.productionorder", "P1"); got["yield"] != float64(3) || got["scrap"] != float64(1) || got["remaining"] != float64(6) {
		t.Fatalf("partial quantities: %+v", got)
	}
	stockQuantity := func() float64 {
		t.Helper()
		page, refusal := provider.Records(memberOf(t, provider, "operator"), "build.finishedstock", platform.Query{}, at)
		if refusal != nil {
			t.Fatal(refusal)
		}
		var rows []map[string]any
		if err := json.Unmarshal(platform.Raw(page.Records), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("stock balances: %+v", rows)
		}
		return rows[0]["quantity"].(float64)
	}
	if stockQuantity() != 3 {
		t.Fatal("partial good units did not enter stock immediately")
	}
	receipts, refusal := provider.Records(memberOf(t, provider, "operator"), "build.productionbatch", platform.Query{}, at)
	if refusal != nil || !strings.Contains(string(platform.Raw(receipts.Records)), `"receipt":`) {
		t.Fatalf("batch stock receipt missing: %+v %v", receipts, refusal)
	}
	start("W2", "P1")
	confirm("W2", "P1", "B", 5, 1, false)
	if got := read(provider, "build.productionorder", "P1"); got["state"] != "closed" || got["yield"] != float64(8) || got["scrap"] != float64(2) {
		t.Fatalf("multi-shop accumulation: %+v", got)
	}
	if stockQuantity() != 8 {
		t.Fatal("multi-shop good stock differs")
	}
	provider.Dispatch(at.Add(time.Second))
	provider.Dispatch(at.Add(2 * time.Second))
	journals := platform.Records[platform.Journal](provider.automation(core.ID, false))
	if len(journals) != 2 || journals[0].Lines[0].Debit+journals[1].Lines[0].Debit != 80 {
		t.Fatalf("partial receipt accounting was not independently posted: %+v effects=%s", journals, platform.Raw(provider.Effects(at)))
	}
	createUpstream("P2", 10)
	createShop("W3", "P2", 10)
	start("W3", "P2")
	confirm("W3", "P2", "C", 2, 1, true)
	if got := read(provider, "build.productionorder", "P2"); got["state"] != "closed" || got["remaining"] != float64(7) {
		t.Fatalf("explicit final erased or refused shortage: %+v", got)
	}

	// Lose a POST reply after the upstream has accepted it. Read the exact
	// batch identity through the declared Source; never resend that request.
	createUpstream("P5", 10)
	createShop("W7", "P5", 10)
	start("W7", "P5")
	posts := 0
	consumer.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		response, err := server.Client().Do(req)
		if req.Method == http.MethodPost {
			posts++
			if err == nil {
				response.Body.Close()
				return nil, io.ErrUnexpectedEOF
			}
		}
		return response, err
	}
	confirm("W7", "P5", "lost-G", 2, 0, false)
	if got := read(provider, "build.productionorder", "P5"); got["yield"] != float64(2) {
		t.Fatalf("lost reply did not follow upstream acceptance: %+v", got)
	}
	if read(consumer, "build.shoporder", "W7")["state"] != "pending" {
		t.Fatal("lost reply claimed upstream success")
	}
	at = at.Add(time.Second)
	consumer.ReconcileConfirmations(at)
	at = at.Add(time.Second)
	runJob(t, consumer, construction.ID, "confirmations", at)
	if posts != 1 || read(consumer, "build.shoporder", "W7")["state"] != "running" {
		t.Fatalf("batch readback did not reconcile without resend: posts=%d confirmations=%s", posts, platform.Raw(platform.Records[platform.ConfirmationRun](consumer.automation(construction.ID, false))))
	}
	consumer.Outbound = nil

	// The upstream quota is independent of local shop allowances. A second
	// shop can report a local fact which the upstream authority then refuses.
	createUpstream("P3", 5)
	createShop("W4", "P3", 4)
	createShop("W5", "P3", 4)
	start("W4", "P3")
	confirm("W4", "P3", "D", 4, 0, false)
	start("W5", "P3")
	confirm("W5", "P3", "E", 2, 0, false)
	if got := read(provider, "build.productionorder", "P3"); got["yield"] != float64(4) || got["remaining"] != float64(1) {
		t.Fatalf("upstream accepted overflow: %+v", got)
	}
	if got := read(consumer, "build.shoporder", "W5"); got["yield"] != float64(2) || got["state"] != "pending" {
		t.Fatalf("refusal rewrote local facts or claimed success: %+v", got)
	}
	refused := false
	for _, run := range platform.Records[platform.ConfirmationRun](consumer.automation(construction.ID, false)) {
		if run.Subject == "build.shoporder/W5" && len(run.Parties) == 1 && run.Parties[0].State == "rejected" {
			refused = true
		}
	}
	if !refused {
		for _, run := range platform.Records[platform.ConfirmationRun](consumer.automation(construction.ID, false)) {
			if run.Subject == "build.shoporder/W5" {
				t.Errorf("upstream receipt: state=%s parties=%s", run.State, platform.Raw(run.Parties))
			}
		}
		t.Fatal("upstream refusal did not retain its own receipt")
	}

	call := func(token, target, key, raw string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/protocols/production.orders/2/confirm/"+target, strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response.StatusCode
	}
	createUpstream("P4", 10)
	if _, _, refusal := provider.Invoke(memberOf(t, provider, "operator"), "production.orders/2", "start", "P4", []byte(`{"shoporder":"W6"}`), "start-P4", at); refusal != nil {
		t.Fatal(refusal)
	}
	body := `{"shoporder":"W6","batch":"F","yield":1,"scrap":0,"final":false}`
	if status := call("operator", "P4", "batch-F", body); status != 200 {
		t.Fatalf("protocol HTTP confirmation: %d", status)
	}
	accepted := len(providerEntries)
	if status := call("operator", "P4", "batch-F", body); status != 200 || len(providerEntries) != accepted {
		t.Fatalf("same key repeated a batch: %d entries=%d/%d", status, len(providerEntries), accepted)
	}
	if status := call("operator", "P4", "batch-F", strings.Replace(body, `"yield":1`, `"yield":2`, 1)); status < 400 {
		t.Fatal("same key accepted another payload")
	}
	if status := call("operator", "P4", "", body); status != 400 {
		t.Fatalf("missing idempotency key: %d", status)
	}
	if status := call("outsider", "P4", "forbidden", body); status < 400 {
		t.Fatal("protocol bypassed the original action role")
	}
	if _, refusal := provider.Query(memberOf(t, provider, "operator"), "production.orders/2", "orders"); refusal != nil {
		t.Fatalf("declared protocol read not registered: %v", refusal)
	}
	if len(providerEntries) == 0 || len(consumerEntries) == 0 {
		t.Fatal("independent tenants did not save independent results")
	}
	CheckReplay(t, provider, providerEntries, func() *Tenant { return compose("production-provider", providerIndex) })
	CheckReplay(t, consumer, consumerEntries, func() *Tenant { return compose("production-consumer", consumerIndex) })

	// The portable plan runs the same published declarations without touching
	// either tenant's live rows or calling the other deployment.
	for _, asset := range providerPackage.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetTestPlan {
			continue
		}
		plan, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			t.Fatal(err)
		}
		request := CandidateSimulationRequest{Roots: plan.Roots, At: plan.At, As: "operator"}
		for _, step := range plan.Steps {
			request.Steps = append(request.Steps, SimulationStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect})
		}
		result, refusal := provider.SimulateCandidate(memberOf(t, provider, "builder"), request)
		if refusal != nil || result.Passed == nil || !*result.Passed || !result.Recovered {
			var outcomes []string
			for _, step := range result.Steps {
				outcomes = append(outcomes, step.Refusal)
			}
			t.Fatalf("portable partial-production plan: recovered=%v passed=%v refusals=%v error=%v", result.Recovered, result.Passed, outcomes, refusal)
		}
	}
}

func TestDeclarativeProtocolPublicationAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "declared-protocol", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("alice", "build:user"), seatOf("bob", "build:user")}, build.New("declared-protocol"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	object := map[string]any{"name": "job", "title": "Job", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}, "access": []map[string]any{{"role": "user", "read": "own", "create": true, "edit": true}}, "states": []map[string]any{{"name": "open", "title": "Open"}}, "actions": []map[string]any{{"name": "first", "title": "First", "from": []string{"open"}, "inputs": []map[string]any{{"name": "batch", "title": "Batch", "type": "text", "required": true}}, "transaction": map[string]any{"subject": "build.job", "steps": []map[string]any{{"name": "update", "kind": "update", "target": map[string]any{"type": "build.job", "id": "$subject"}, "changes": []map[string]any{{"field": "title", "expr": platform.ExprReference("text", "input.batch")}}}}}}, {"name": "second", "title": "Second", "from": []string{"open"}, "inputs": []map[string]any{{"name": "batch", "title": "Batch", "type": "text", "required": true}}, "transaction": map[string]any{"subject": "build.job", "steps": []map[string]any{{"name": "update", "kind": "update", "target": map[string]any{"type": "build.job", "id": "$subject"}, "changes": []map[string]any{{"field": "title", "expr": platform.ExprLiteralValue("text", "second")}}}}}}}, "protocols": []platform.ProtocolBinding{{Name: "example.jobs", Version: 1, Actions: map[string]string{"confirm": "first"}, Reads: []string{"jobs"}}}}
	publishObject(t, tn, "builder", "job", object, at)
	decide(t, tn, "alice", construction.ID, "build.job.create", "build.job", "A", map[string]any{"title": "Alice"}, at)
	for i := 0; i < 201; i++ {
		decide(t, tn, "bob", construction.ID, "build.job.create", "build.job", "B"+time.Duration(i).String(), map[string]any{"title": "Bob"}, at)
	}
	result, refusal := tn.Query(memberOf(t, tn, "alice"), "example.jobs/1", "jobs")
	if refusal != nil {
		t.Fatal(refusal)
	}
	var rows []map[string]any
	if err := json.Unmarshal(platform.Raw(result), &rows); err != nil || len(rows) != 1 || rows[0]["id"] != "A" {
		t.Fatalf("protocol did not use visible records: %s %v", platform.Raw(result), err)
	}
	decide(t, tn, "builder", PlatformApp, "platform.protocol.bind", "platform.protocol", "example.jobs/1", map[string]any{"provider": construction.ID}, at)
	bindings := []platform.ProtocolBinding{{Name: "example.jobs", Version: 1, Actions: map[string]string{"confirm": "second"}, Reads: []string{"jobs"}}}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "job", map[string]any{"protocols": bindings}, at)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "job", map[string]any{}, at)
	if _, _, refusal := tn.Invoke(memberOf(t, tn, "alice"), "example.jobs/1", "confirm", "A", []byte(`{"batch":"one"}`), "mapped-second", at); refusal != nil {
		t.Fatal(refusal)
	}
	view, refusal := tn.RecordOf(memberOf(t, tn, "alice"), "build.job", "A", at)
	if refusal != nil || !bytes.Contains(platform.Raw(view.Record), []byte(`"title":"second"`)) {
		t.Fatalf("bound protocol retained its old mapping: %s %v", platform.Raw(view.Record), refusal)
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "job", map[string]any{"protocols": []platform.ProtocolBinding{}}, at)
	if got := refuse(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "job", map[string]any{}, at); got == "ok" {
		t.Fatal("removed a bound provider needed by control recovery")
	}
	CheckReplay(t, tn, entries, compose)

	// An explicit production contract cannot be weakened by an action mapping.
	var pkg PackageDescriptor
	raw, err := os.ReadFile("../../deploy/packages/production-provider.json")
	if err != nil || json.Unmarshal(raw, &pkg) != nil {
		t.Fatalf("provider package: %v", err)
	}
	var declared construction.Object
	if err := json.Unmarshal(pkg.Blueprint.Assets[0].Body, &declared); err != nil {
		t.Fatal(err)
	}
	if declared.Protocols[0].Contract == nil {
		t.Fatal("production protocol did not retain its contract")
	}
	for i := range declared.Actions {
		declared.Actions[i].Transaction = nil
		declared.Actions[i].To = "running"
		if declared.Actions[i].Name == "confirm" {
			inputs := declared.Actions[i].Inputs[:0]
			for _, input := range declared.Actions[i].Inputs {
				if input.Name != "batch" && input.Name != "final" {
					inputs = append(inputs, input)
				}
			}
			declared.Actions[i].Inputs = inputs
		}
	}
	badPayload := map[string]any{"name": declared.Name, "title": declared.Title, "fields": declared.Fields, "access": declared.Access, "states": declared.States, "actions": declared.Actions, "protocols": declared.Protocols}
	bad := builderTenant(t, "bad-production-protocol")
	if got := refuse(t, bad, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "bad", badPayload, at); !strings.Contains(got, "incompatible payload") {
		t.Fatalf("production contract must reject confirm without batch/final: %s", got)
	}
}

func TestDeclarativeProtocolReleaseMatchesInstalledAndRestored(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "protocol-release") }
	tenant := compose()
	var entries []Entry
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	bindings := []platform.ProtocolBinding{{Name: "example.release", Version: 2, Actions: map[string]string{"release": "complete"}, Reads: []string{"records"}, Events: map[string]string{"released": "complete"}}}
	decide(t, tenant, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "provider", map[string]any{
		"name": "provider", "title": "Protocol provider", "fields": []construction.Field{{Name: "note", Title: "Order note", Type: "text"}},
		"states":  []construction.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}},
		"actions": []construction.Action{{Name: "complete", Title: "Complete order", From: []string{"open"}, To: "done"}}, "protocols": bindings,
	}, at)
	assertReview := func(target *Tenant, candidate string) {
		t.Helper()
		review, err := target.ReviewSavedRelease(memberOf(t, target, "builder"), candidate)
		if err != nil || !review.Active || !review.RunningMatches || review.RunningDiagnostic != "" || review.Preview.CurrentID != candidate || len(review.Preview.Changed) != 0 {
			t.Fatalf("activated protocol release differs from installed definitions: %+v %v", review, err)
		}
	}
	for _, pinned := range []bool{false, true} {
		if pinned {
			bindings[0].Contract = &tenant.app(construction.ID).Manifest().Provides[0].Protocol
			decide(t, tenant, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "provider", map[string]any{"protocols": bindings}, at)
		}
		builder := memberOf(t, tenant, "builder")
		preview, err := tenant.PreviewRelease(builder, platform.AssetObject, "provider")
		if err != nil {
			t.Fatal(err)
		}
		suffix := "derived"
		if pinned {
			suffix = "contract"
		}
		candidate, err := tenant.SaveReleaseCandidate(builder, platform.AssetObject, "provider", preview.CandidateID, "save-"+suffix, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tenant.ActivateRelease(builder, candidate, "activate-"+suffix, at); err != nil {
			t.Fatal(err)
		}
		assertReview(tenant, candidate)
		restored := compose()
		saved, _, err := tenant.Snapshot(func() int64 { return int64(len(entries)) })
		if err != nil {
			t.Fatal(err)
		}
		if err = restored.Restore(saved); err != nil {
			t.Fatal(err)
		}
		assertReview(restored, candidate)
	}
	CheckReplay(t, tenant, entries, compose)
}
