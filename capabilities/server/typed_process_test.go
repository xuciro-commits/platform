package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestTypedManualProcessResumesSavedIterationScopes(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tenant, err := NewTenant("typed-scopes", NewConsole("typed-scopes", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin}}}), work.New("typed-scopes"), flow.New("typed-scopes"), build.New("typed-scopes"))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	tenant := compose()
	member, _ := tenant.Member("builder")
	var journal []Entry
	tenant.Record = func(entry Entry) { journal = append(journal, entry) }
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		journal = append(journal, entry)
		return entry.Body, nil
	}
	counter := 0
	submit := func(schema string, payload any) *pb.Submission {
		t.Helper()
		counter++
		raw, _ := json.Marshal(payload)
		s := &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(counter), Target: &pb.EntityRef{Type: build.ProcessType, Id: "P"}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
		if _, err := tenant.Submit(member, s, at); err != nil {
			t.Fatalf("%s: %s", schema, err.Error())
		}
		return s
	}
	stepBinding := func(node string, path ...string) *platform.Binding {
		return &platform.Binding{Source: "step", Step: node, Path: path}
	}
	schema := platform.ValueSchema{Type: "object", Required: []string{"items"}, Properties: map[string]platform.ValueSchema{"items": {Type: "array", MaxItems: 8, Items: &platform.ValueSchema{Type: "string"}}}}
	steps := []build.ProcessStep{
		{Name: "input", Kind: "payload", Next: "each"},
		{Name: "each", Kind: "foreach", Collection: stepBinding("input", "items"), Body: "map", MaxIterations: 8, Concurrency: 2, Next: "return"},
		{Name: "map", Kind: "transform", Inputs: map[string]platform.Binding{"item": {Source: "item"}, "index": {Source: "index"}}, Next: "pause"},
		{Name: "pause", Kind: "wait", UntilSeconds: 1, Next: "itemdone"},
		{Name: "itemdone", Kind: "end", Value: stepBinding("map")},
		{Name: "return", Kind: "end", Value: stepBinding("each")},
	}
	submit(build.ProcessType+".create", map[string]any{"name": "scope", "title": "Scoped flow", "manual": true, "input": json.RawMessage(`{"items":["a","b","c"]}`), "inputSchema": schema, "steps": steps})
	beforeProduction := snapshot(tenant)
	runPayload, _ := json.Marshal(map[string]string{"key": "simulation"})
	simulation, problem := tenant.SimulateCandidate(member, CandidateSimulationRequest{ProcessID: "P", At: at, Steps: []SimulationStep{{Type: build.ProcessType, ID: "P", Action: build.SchemaProcessRun, Payload: runPayload, Expect: "accepted", AdvanceSeconds: 1}, {Type: build.ProcessType, ID: "simulation", Payload: json.RawMessage(`{}`), Expect: "accepted", AdvanceSeconds: 1}}})
	if problem != nil || !simulation.Recovered || simulation.Passed == nil || !*simulation.Passed || len(simulation.Steps[0].Flows) != 1 || snapshot(tenant) != beforeProduction {
		var reason string
		if problem != nil {
			reason = problem.Message
		}
		t.Fatalf("manual candidate test leaked state or did not recover: %+v %s", simulation, reason)
	}
	submit(build.SchemaProcess, map[string]any{})
	request := submit(build.SchemaProcessRun, map[string]string{"key": "sample"})
	if _, err := tenant.Submit(member, request, at); err != nil {
		t.Fatal(err)
	}
	tenant.Work(at)
	page, err := tenant.Records(member, flow.InstanceType, platform.Query{}, at)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("manual start was not accepted once: %+v %v", page, err)
	}
	pending := page.Records[0].(flow.FlowInstance)
	if pending.State != "waiting" || len(pending.Tokens) != 3 || pending.Tokens[0].Loop == nil || pending.Tokens[0].Loop.Next != 2 {
		t.Fatalf("manual scoped body was not suspended: %+v", pending)
	}
	image, _, snapshotErr := tenant.Snapshot(func() int64 { return 0 })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	restored := compose()
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	// A later draft change is not the input of the running version.
	raw, _ := json.Marshal(map[string]any{"input": json.RawMessage(`{"items":["different"]}`)})
	patch := &pb.Submission{TenantId: restored.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "draft", Target: &pb.EntityRef{Type: build.ProcessType, Id: "P"}, Schema: &pb.SchemaRef{Name: build.ProcessType + ".edit", Version: 1}, Payload: raw}
	if _, err := restored.Submit(member, patch, at); err != nil {
		t.Fatal(err)
	}
	for second := 1; second < 8; second++ {
		restored.Work(at.Add(time.Duration(second) * time.Second))
	}
	result, problem := restored.RecordOf(member, flow.InstanceType, pending.ID, at.Add(8*time.Second))
	if problem != nil {
		t.Fatal(problem)
	}
	done := result.Record.(flow.FlowInstance)
	var output struct {
		Count int                          `json:"count"`
		Items []map[string]json.RawMessage `json:"items"`
	}
	if done.State != "done" || json.Unmarshal(done.Outputs["return"], &output) != nil || output.Count != 3 {
		t.Fatalf("restored scoped flow changed its saved input or did not finish: %+v jobs=%+v quarantined=%v", done, restored.Tasks(), restored.quarantined())
	}
	var first struct {
		Item  string `json:"item"`
		Index int    `json:"index"`
	}
	json.Unmarshal(output.Items[0]["map"], &first)
	if first.Item != "a" || first.Index != 0 {
		t.Fatalf("restored item/index changed: %+v", first)
	}
	if len(journal) == 0 {
		t.Fatal("start/timer progress bypassed accepted-result journal")
	}
}

type querySampleRow struct {
	platform.Record
	Label string `json:"label"`
}

func TestManualCandidateUsesExplicitNativeQuerySamples(t *testing.T) {
	at := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	source := newSampleOwner("query-sample", platform.Manifest{ID: "source", Title: "Source", Version: "1", Actions: platform.NewCatalog(), Roles: []string{"viewer"}, Entities: []platform.Entity{{Type: "source.item", Title: "Item", Model: querySampleRow{}, Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"viewer": platform.ScopeTenant}}, Seed: []any{querySampleRow{Record: platform.Record{ID: "LIVE"}, Label: "live-only"}}}}, Queries: []platform.NamedQuery{{Name: "items", Title: "Items", Object: "source.item", Limit: 20}}})
	tenant, err := NewTenant("query-sample", NewConsole("query-sample", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin, "source": "viewer"}}}), source, work.New("query-sample"), flow.New("query-sample"), build.New("query-sample"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tenant.Member("builder")
	raw, _ := json.Marshal(map[string]any{"name": "query", "title": "Query sample", "manual": true, "input": json.RawMessage(`{}`), "steps": []build.ProcessStep{{Name: "query", Kind: "query", App: "source", Query: "items", Next: "return"}, {Name: "return", Kind: "end", Value: &platform.Binding{Source: "step", Step: "query"}}}})
	if _, problem := tenant.Submit(member, &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "process", Target: &pb.EntityRef{Type: build.ProcessType, Id: "P"}, Schema: &pb.SchemaRef{Name: build.ProcessType + ".create", Version: 1}, Payload: raw}, at); problem != nil {
		t.Fatal(problem.Message)
	}
	before := snapshot(tenant)
	result, problem := tenant.SimulateCandidate(member, CandidateSimulationRequest{ProcessID: "P", At: at, Samples: []SimulationSample{{Type: "source.item", Records: []json.RawMessage{json.RawMessage(`{"id":"S","label":"synthetic"}`)}}}, Steps: []SimulationStep{{Type: build.ProcessType, ID: "P", Action: build.SchemaProcessRun, Payload: json.RawMessage(`{"key":"sample"}`), Expect: "accepted", AdvanceSeconds: 1}}})
	if problem != nil {
		t.Fatal(problem.Message)
	}
	if result.Passed == nil || !*result.Passed || !result.Recovered || len(result.Steps[0].Flows) != 1 {
		t.Fatalf("native Query did not use original candidate runtime: %+v", result)
	}
	output := string(result.Steps[0].Flows[0].Outputs["return"])
	if !strings.Contains(output, "synthetic") || strings.Contains(output, "live-only") || snapshot(tenant) != before {
		t.Fatalf("native Query sample copied live data or mutated production: %s", output)
	}
}

func TestFlowCancellationRevokesOriginalOperationGeneration(t *testing.T) {
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	app := &computeStock{stock: newStock("flow-cancel")}
	tenant, err := NewTenant("flow-cancel", NewConsole("flow-cancel", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin, "stock": "clerk"}}}), app, work.New("flow-cancel"), flow.New("flow-cancel"), build.New("flow-cancel"))
	if err != nil {
		t.Fatal(err)
	}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	member, _ := tenant.Member("builder")
	submit := func(app, key, schema, typ, id string, payload any) {
		t.Helper()
		if _, problem := tenant.Submit(member, &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID, Authority: app, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at); problem != nil {
			t.Fatal(problem.Message)
		}
	}
	submit(build.ID, "create", build.ProcessType+".create", build.ProcessType, "P", map[string]any{"name": "calculate", "title": "Calculate", "manual": true, "input": json.RawMessage(`{}`), "steps": []build.ProcessStep{{Name: "calc", Kind: "compute", Operation: &build.OperationRef{App: "stock", Name: "double"}, Inputs: map[string]platform.Binding{"value": {Source: "literal", Value: json.RawMessage(`5`)}}, Next: "end"}, {Name: "end", Kind: "end", Value: &platform.Binding{Source: "step", Step: "calc"}}}})
	submit(build.ID, "publish", build.SchemaProcess, build.ProcessType, "P", struct{}{})
	submit(build.ID, "run", build.SchemaProcessRun, build.ProcessType, "P", map[string]string{"key": "sample"})
	tenant.Work(at)
	instance, _ := platform.Get[flow.FlowInstance](tenant.automation(flow.ID, false), "build.calculate:sample")
	if len(instance.Tokens) != 1 || instance.Tokens[0].Operation == "" {
		t.Fatalf("flow created no original operation token: %+v", instance)
	}
	callID := instance.Tokens[0].Operation
	claimed, ok := tenant.claimOperation(callID, at)
	if !ok {
		t.Fatal("operation claim failed")
	}
	submit(flow.ID, "cancel", flow.SchemaFlowStop, flow.InstanceType, instance.ID, struct{}{})
	result, problem := tenant.operationResult(platform.NewCaller(runtime{tenant}, member, PlatformApp, false, false), callID)
	if problem != nil || result.State != "cancelled" || tenant.operationCurrent(callID, claimed.Generation) {
		t.Fatalf("Flow cancellation did not revoke its accepted worker generation: %+v %v", result, problem)
	}
	tenant.settleWithUsage(callID, platform.Outcome{Effect: callID, Result: "delivered", Generation: claimed.Generation, Answer: json.RawMessage(`{"doubled":10}`)}, nil, at.Add(time.Second))
	final, _ := platform.Get[flow.FlowInstance](tenant.automation(flow.ID, false), instance.ID)
	if final.State != "canceled" || final.Outputs["calc"] != nil {
		t.Fatal("late completion moved a cancelled token")
	}
}
