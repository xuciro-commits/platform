package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A Flow step builds its operation input inside its own accepting decision,
// where artifact I/O is forbidden (ADR-0047 §13.3): the declared data channel
// stays reachable only through the native prepared call. A step input above
// the operation's inline budget is therefore refused with that reason - never
// trimmed, never sealed - and nothing of the refused step is committed: the
// instance stays on its own revision and the Flow owner's run reports the
// refusal, so the same revision retries the same answer.
func TestFlowStepOperationInputStaysInline(t *testing.T) {
	const id = "flow-inline"
	store := &memoryFiles{}
	tn := builderTenant(t, id, work.New(id), flow.New(id))
	tn.Files = store
	tn.Compiler = fixtureCodeCompiler{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "O", map[string]any{
		"name": "sample", "title": "Sample", "states": []build.State{{Name: "open", Title: "Open"}},
		"fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "blob", Title: "Blob", Type: "text"}}}, at)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "O", map[string]any{}, at)

	// One v2 operation whose read channel is far larger than its inline budget.
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "string"}}, Required: []string{"value"}}
	decide(t, tn, "builder", build.ID, "build.code.create", build.CodeType, "C", map[string]any{
		"name": "echo", "title": "Echo", "abi": platform.WasmDataABI, "language": "go",
		"source": "package main\nfunc Run(input Input)(Output,error){return Output{Value:input.Value},nil}",
		"input":  schema, "output": schema, "roles": []string{build.Builder},
		"limits": platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 512, MaxInputBytes: 32 << 10, MaxOutputBytes: 4096, StagedOutputBytes: 64 << 10, DataInputBytes: 1 << 20}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaCodeCompile, build.CodeType, "C", map[string]any{}, at)
	for _, run := range tn.operationDispatches(at) {
		run()
	}
	member := memberOf(t, tn, "builder")
	preview, err := tn.PreviewRelease(member, platform.AssetCompute, "C")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.SaveReleaseCandidate(member, platform.AssetCompute, "C", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}

	decide(t, tn, "builder", build.ID, build.ProcessType+".create", build.ProcessType, "P", map[string]any{
		"name": "carry", "title": "Carry", "object": build.ID + ".sample", "manual": true,
		"steps": []build.ProcessStep{
			{Name: "compute", Kind: "compute", Operation: &build.OperationRef{App: build.ID, Name: "echo", Version: 1},
				Inputs: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"blob"}}}, Next: "end"},
			{Name: "end", Kind: "end"}}}, at)
	decide(t, tn, "builder", build.ID, build.ProcessType+".publish", build.ProcessType, "P", map[string]any{}, at)

	// A record inside the inline budget: the step plans the very operation that
	// declares a data channel, and its intent carries the bytes inline.
	decide(t, tn, "user", build.ID, build.ID+".sample.create", build.ID+".sample", "SMALL", map[string]any{"name": "Small", "blob": strings.Repeat("s", 128)}, at)
	decide(t, tn, "builder", build.ID, build.SchemaProcessRun, build.ProcessType, "P", map[string]any{"key": "SMALL"}, at)
	tn.Work(at.Add(time.Second))
	small, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "build.carry:SMALL")
	if !ok || small.Tokens[0].Operation == "" || small.State != "waiting" {
		t.Fatalf("the inline step did not wait on its operation: %+v", small)
	}
	if bindings := operationIntents(tn, ":operation:flow:"); len(bindings) != 1 || bindings[0].SealedInput != nil || len(bindings[0].Inputs) == 0 {
		t.Fatalf("the inline step did not carry its input in the intent: %+v", bindings)
	}

	// The same step over the inline budget: refused with the reason, and the
	// decision leaves no intent, no artifact and no advanced token behind.
	decide(t, tn, "user", build.ID, build.ID+".sample.create", build.ID+".sample", "BIG", map[string]any{"name": "Big", "blob": strings.Repeat("b", 96<<10)}, at)
	decide(t, tn, "builder", build.ID, build.SchemaProcessRun, build.ProcessType, "P", map[string]any{"key": "BIG"}, at)
	tn.Work(at.Add(2 * time.Second))
	tn.Work(at.Add(3 * time.Second))
	big, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "build.carry:BIG")
	if !ok || big.Tokens[0].Operation != "" {
		t.Fatalf("the over-budget step planned an operation: %+v", big)
	}
	hint, reported := flowJobError(tn)
	if !reported || hint != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("the Flow owner did not report the refusal: %q %v", hint, reported)
	}
	if keys := computeInputKeys(t, tn); len(keys) != 0 {
		t.Fatalf("a Flow step sealed an operation input: %v", keys)
	}
	if bindings := operationIntents(tn, ":operation:flow:"); len(bindings) != 1 || bindings[0].SealedInput != nil {
		t.Fatalf("the refused step changed how the intent carries inputs: %+v", bindings)
	}
	if now, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "build.carry:SMALL"); now.Tokens[0].Operation != small.Tokens[0].Operation {
		t.Fatalf("the refused step disturbed the inline instance: %+v", now)
	}

	// The reason itself, as the Flow owner's step reads it: the boundary is
	// named rather than left as a generic schema mismatch.
	caller := tn.automation(flow.ID, false)
	record := &pb.ChangeRecord{ChangeId: "flow-step-big", RecordedTime: timestamppb.New(at), Revision: 2,
		Submission: &pb.Submission{TenantId: tn.ID, PrincipalId: caller.ID, Authority: flow.ID,
			Target: &pb.EntityRef{Type: flow.InstanceType, Id: "build.carry:BIG"}, Schema: &pb.SchemaRef{Name: "flow.instance.step"}}}
	_, _, refusal := tn.planOperation(caller, record, platform.OperationRequest{App: build.ID, Name: "echo", Key: "flow-big",
		OnBehalf: "builder", Inputs: json.RawMessage(`{"value":"` + strings.Repeat("b", 96<<10) + `"}`)}, nil, nil)
	if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || !strings.Contains(refusal.Message, "inline budget") {
		t.Fatalf("the step's refusal does not name the inline boundary: %+v", refusal)
	}
}

// operationIntents reads the accepted operation outbox as bindings, keeping
// the calls whose ID holds the given substring ("" keeps every call).
func operationIntents(tn *Tenant, holding string) []operationBinding {
	tn.opsMu.Lock()
	defer tn.opsMu.Unlock()
	out := []operationBinding{}
	for _, x := range tn.outbound {
		if x.Endpoint != operationEndpoint || !strings.Contains(x.ID, holding) {
			continue
		}
		var binding operationBinding
		if json.Unmarshal([]byte(x.Body), &binding) == nil {
			out = append(out, binding)
		}
	}
	return out
}

// flowJobError is the outcome the Flow owner's own job last reported: a step
// whose decision is refused advances no token and comes back to its run as
// this code, which the work registry and the host's runs surface to an
// administrator.
func flowJobError(tn *Tenant) (string, bool) {
	tn.opsMu.Lock()
	defer tn.opsMu.Unlock()
	for _, task := range tn.work.jobs {
		if task.App == flow.ID && task.Error != "" {
			return task.Error, true
		}
	}
	return "", false
}

// computeInputKeys names the sealed operation inputs a tenant's store holds.
func computeInputKeys(t *testing.T, tn *Tenant) []string {
	t.Helper()
	keys, err := tn.files().List(t.Context(), tn.ID+"/artifacts/compute-input/")
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
