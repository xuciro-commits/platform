package platformserver

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A neutral package fixture verifies assembly rather than implementing WMS.
type assemblyRecord struct {
	platform.Record
	Quantity int    `json:"quantity"`
	Result   int    `json:"result"`
	State    string `json:"state" field:"readonly" choices:"open,done"`
}
type assemblyOwner struct{ ledger *platform.Ledger }

func (a *assemblyOwner) entity() platform.Entity {
	return platform.Entity{Type: "source.item", Title: "Item", Model: assemblyRecord{}, Display: "state", Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"operator": platform.ScopeTenant}}, Seed: []any{assemblyRecord{Record: platform.Record{ID: "positive"}, Quantity: 3, State: "open"}, assemblyRecord{Record: platform.Record{ID: "empty"}, Quantity: 0, State: "open"}}, Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open", States: []platform.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, Transitions: []platform.Transition{{Name: "mark", Title: "Mark result", From: []string{"open"}, To: []string{"done"}, Roles: []string{"operator"}, Payload: []platform.Field{{Name: "result", Type: "integer", Required: true, Description: "Accepted algorithm result"}}, Do: func(_ platform.Caller, record any, raw json.RawMessage, _ time.Time) *kernel.Error {
		var input struct{ Result int }
		if json.Unmarshal(raw, &input) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid result")
		}
		record.(*assemblyRecord).Result = input.Result
		return nil
	}}}}}
}
func newAssemblyOwner(id string) *assemblyOwner {
	a := &assemblyOwner{}
	a.ledger = platform.NewLedger(id, "source", platform.NewCatalog(platform.EntityActions(a.entity())...), "source.item")
	return a
}
func (a *assemblyOwner) Manifest() platform.Manifest {
	return platform.Manifest{ID: "source", Title: "Source", Version: "1", Actions: a.ledger.Catalog, Entities: []platform.Entity{a.entity()}, Queries: []platform.NamedQuery{{Name: "items", Title: "Items", Object: "source.item", Sort: []string{"id"}, Limit: 10}}}
}
func (a *assemblyOwner) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *assemblyOwner) AcceptedLedger() *platform.Ledger         { return a.ledger }
func (a *assemblyOwner) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *assemblyOwner) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }
func (a *assemblyOwner) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	r, err, _ := a.ledger.Generated(c, s, at, nil, a.entity())
	return r, err
}
func (a *assemblyOwner) Read(platform.Caller, string) (any, *kernel.Error) { return nil, unknown() }
func (a *assemblyOwner) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}

func TestAssembledQueryWasmLoopActionAndHumanWork(t *testing.T) {
	if os.Getenv("PLATFORM_ASSEMBLY_WASM") != "1" {
		t.Skip("explicit isolated compiler/worker route is not enabled")
	}
	const id = "assembly-route"
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	store := &memoryFiles{}
	compose := func() *Tenant {
		tenant, err := NewTenant(id, NewConsole(id, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin, "source": "operator"}}}), newAssemblyOwner(id), work.New(id), flow.New(id), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		tenant.Files = store
		return tenant
	}
	tenant := compose()
	member, _ := tenant.Member("builder")
	var journal []Entry
	tenant.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
	key := 0
	submit := func(owner, typ, target, schema string, payload any) {
		t.Helper()
		key++
		raw, _ := json.Marshal(payload)
		if _, refusal := tenant.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: owner, IdempotencyKey: fmt.Sprintf("input-%d", key), Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at); refusal != nil {
			t.Fatalf("%s: %s", schema, refusal.Message)
		}
	}
	input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "integer"}}, Required: []string{"value"}}
	for _, language := range []string{"go", "tinygo"} {
		output := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"result": {Type: "integer"}, "accept": {Type: "boolean"}}, Required: []string{"result", "accept"}}
		source := "package main\nfunc Run(input Input)(Output,error){return Output{Result:input.Value*2,Accept:input.Value>0},nil}"
		if language == "tinygo" {
			source = "package main\nfunc Run(input Input)(Output,error){return Output{Result:input.Value+1,Accept:input.Value>0},nil}"
		}
		submit(build.ID, build.CodeType, language, build.CodeType+".create", map[string]any{"name": language, "title": language, "language": language, "source": source, "input": input, "output": output, "roles": []string{build.Builder}, "limits": platform.OperationLimits{TimeoutMillis: 2000, MemoryPages: 1024, MaxInputBytes: 4096, MaxOutputBytes: 4096}})
		submit(build.ID, build.CodeType, language, build.SchemaCodeCompile, map[string]any{})
		for _, run := range tenant.operationDispatches(at) {
			run()
		}
		code, _ := platform.Get[build.Code](tenant.automation(build.ID, false), language)
		if code.State != "compiled" {
			t.Fatalf("%s build: %s (%s)", language, code.State, code.Diagnostics)
		}
		preview, err := tenant.PreviewRelease(member, platform.AssetCompute, language)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tenant.SaveReleaseCandidate(member, platform.AssetCompute, language, preview.CandidateID, "save-"+language, at); err != nil {
			t.Fatal(err)
		}
		if _, err := tenant.ActivateRelease(member, preview.CandidateID, "activate-"+language, at); err != nil {
			t.Fatal(err)
		}
	}
	from := func(node string, path ...string) *platform.Binding {
		return &platform.Binding{Source: "step", Step: node, Path: path}
	}
	literal := func(raw string) *platform.Binding {
		return &platform.Binding{Source: "literal", Value: json.RawMessage(raw)}
	}
	steps := []build.ProcessStep{
		{Name: "query", Kind: "query", App: "source", Query: "items", Next: "each"},
		{Name: "each", Kind: "foreach", Collection: from("query", "records"), Body: "go", MaxIterations: 4, Concurrency: 2, Next: "output"},
		{Name: "go", Kind: "compute", Operation: &build.OperationRef{App: build.ID, Name: "go", Version: 1}, Inputs: map[string]platform.Binding{"value": {Source: "item", Path: []string{"quantity"}}}, Next: "tinygo"},
		{Name: "tinygo", Kind: "compute", Operation: &build.OperationRef{App: build.ID, Name: "tinygo", Version: 1}, Inputs: map[string]platform.Binding{"value": *from("go", "result")}, Next: "branch"},
		{Name: "branch", Kind: "branch", Condition: &platform.Predicate{Op: "eq", Left: from("tinygo", "accept"), Right: literal("true")}, Cases: map[string]string{"true": "mark", "false": "skip"}},
		{Name: "mark", Kind: "action", Act: "source.item.mark", Target: &platform.Binding{Source: "item", Path: []string{"id"}}, Inputs: map[string]platform.Binding{"result": *from("tinygo", "result")}, Next: "review"},
		{Name: "review", Kind: "ask", Ask: build.Builder, Answers: []string{"continue"}, Next: "itemdone"},
		{Name: "skip", Kind: "transform", Value: literal(`"empty quantity"`), Next: "itemdone"},
		{Name: "itemdone", Kind: "end", Value: from("tinygo")},
		{Name: "output", Kind: "end", Value: from("each")},
	}
	submit(build.ID, build.ProcessType, "P", build.ProcessType+".create", map[string]any{"name": "assembled", "title": "Assembled capability route", "manual": true, "input": json.RawMessage(`{}`), "steps": steps})
	submit(build.ID, build.ProcessType, "P", build.SchemaProcess, map[string]any{})
	submit(build.ID, build.ProcessType, "P", build.SchemaProcessRun, map[string]string{"key": "route"})
	var run flow.FlowInstance
	answered := false
	for tick := 0; tick < 12; tick++ {
		now := at.Add(time.Duration(tick) * time.Second)
		tenant.Work(now)
		for _, dispatch := range tenant.operationDispatches(now) {
			dispatch()
		}
		for _, task := range mustAssemblyTasks(tenant) {
			if task.State != "open" {
				continue
			}
			submit(work.ID, work.TaskType, task.ID, work.TaskType+".claim", map[string]any{})
			submit(work.ID, work.TaskType, task.ID, work.TaskType+".complete", map[string]string{"answer": "continue"})
			answered = true
		}
		list, _, err := platform.Find[flow.FlowInstance](tenant.automation(flow.ID, false), platform.Query{})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 {
			run = list[0]
			if run.State == "done" {
				break
			}
		}
		if tenant.quarantined() {
			t.Fatal("route quarantined during an accepted computation")
		}
	}
	positive, _ := platform.Get[assemblyRecord](tenant.automation("source", false), "positive")
	empty, _ := platform.Get[assemblyRecord](tenant.automation("source", false), "empty")
	var result struct{ Count int }
	if run.State != "done" || json.Unmarshal(run.Outputs["output"], &result) != nil || result.Count != 2 || !answered || positive.State != "done" || positive.Result != 7 || positive.Revision != 1 || empty.State != "open" || empty.Revision != 0 {
		t.Fatalf("route not complete: state=%s output=%s positive=%+v empty=%+v answered=%v", run.State, run.Outputs["output"], positive, empty, answered)
	}
	if len(run.Sources) == 0 || len(journal) < 8 {
		t.Fatal("route lost source provenance or bypassed accepted results")
	}
	page, refusal := tenant.InvokeCapability(member, platform.CapabilityInvocation{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: "tinygo"}, Version: 1, Key: "page-result", Inputs: json.RawMessage(`{}`), Record: "source.item/positive", Bindings: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"result"}}}}, at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	for _, dispatch := range tenant.operationDispatches(at) {
		dispatch()
	}
	pageOutput, refusal := tenant.ReadOperation(member, page.Call)
	var shown struct {
		Result int
		Accept bool
	}
	if refusal != nil || pageOutput.State != "completed" || json.Unmarshal(pageOutput.Output, &shown) != nil || shown.Result != 8 || !shown.Accept {
		t.Fatalf("page scoped input/result did not use the same compute owner: %+v %v", pageOutput, refusal)
	}
	recovered := compose()
	if err := recovered.Replay(journal); err != nil {
		t.Fatal(err)
	}
	saved, ok := platform.Get[flow.FlowInstance](recovered.automation(flow.ID, false), run.ID)
	marked, _ := platform.Get[assemblyRecord](recovered.automation("source", false), "positive")
	if !ok || saved.State != "done" || string(saved.Outputs["output"]) != string(run.Outputs["output"]) || marked.Revision != 1 || len(recovered.operationDispatches(at)) != 0 {
		t.Fatal("whole-route replay lost output or repeated accepted work")
	}
	t.Logf("single route completed Query → Go → TinyGo → Branch/ForEach → native Action + Work → output; %d accepted rows", len(journal))
}
func mustAssemblyTasks(t *Tenant) []work.WorkTask {
	tasks, _, _ := platform.Find[work.WorkTask](t.automation(work.ID, false), platform.Query{})
	return tasks
}
