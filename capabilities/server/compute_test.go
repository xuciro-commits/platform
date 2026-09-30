package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"testing"
	"time"

	"platformserver/platform"
)

type computeStock struct {
	*stock
	calls int
}

type fixtureCodeCompiler struct{}

func (fixtureCodeCompiler) Compile(_ context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error) {
	hash := sha256.Sum256([]byte(q.Source))
	buildHash, _ := canonicalDigest(q)
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 96, 0, 0, 3, 2, 1, 0, 7, 10, 1, 6, 95, 115, 116, 97, 114, 116, 0, 0, 10, 4, 1, 2, 0, 11}
	return platform.CodeBuildResult{SourceHash: hex.EncodeToString(hash[:]), BuildHash: buildHash, Toolchain: "fixture@sha256:123"}, module, nil
}
func TestCodeBuildUsesArtifactOwnerAndCandidate(t *testing.T) {
	tn, err := NewTenant("code-owner", NewConsole("code-owner", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("code-owner"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Compiler = fixtureCodeCompiler{}
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member, _ := tn.Member("builder")
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "integer"}}, Required: []string{"value"}}
	code := build.Code{Name: "code-double", Title: "Double", Language: "go", Source: "package main\nfunc Run(input Input)(Output,error){return Output{Value:input.Value},nil}", Input: schema, Output: schema, Roles: []string{build.Builder}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 512, MaxInputBytes: 4096, MaxOutputBytes: 4096}}
	create, _ := json.Marshal(map[string]any{"name": code.Name, "title": code.Title, "language": code.Language, "source": code.Source, "input": code.Input, "output": code.Output, "roles": code.Roles, "limits": code.Limits})
	submit := func(key, schema string, payload []byte) {
		t.Helper()
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: build.CodeType, Id: "C"}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: payload}, now); err != nil {
			t.Fatal(err)
		}
	}
	submit("create", build.CodeType+".create", create)
	submit("compile", build.SchemaCodeCompile, []byte(`{}`))
	runs := tn.operationDispatches(now)
	if len(runs) != 1 {
		t.Fatal("compiler task was not admitted")
	}
	runs[0]()
	saved, ok := platform.Get[build.Code](tn.automation(build.ID, false), "C")
	if !ok || saved.State != "compiled" || saved.Module == "" || saved.BuiltSource != code.Source {
		t.Fatalf("build output was not accepted: %+v", saved)
	}
	if !tn.files().Exists(context.Background(), artifactKey(tn.ID, saved.Module)) {
		t.Fatal("module was not stored as an artifact")
	}
	preview, err := tn.PreviewRelease(member, platform.AssetCompute, "C")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.SaveReleaseCandidate(member, platform.AssetCompute, "C", preview.CandidateID, "save", now); err != nil {
		t.Fatal(err)
	}
	submit("edit", build.CodeType+".edit", []byte(`{"source":"package main\n// next draft"}`))
	if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate", now); err != nil {
		t.Fatal(err)
	}
	published, _ := platform.Get[build.Code](tn.automation(build.ID, false), "C")
	if published.Version != 1 || published.Source == code.Source {
		t.Fatal("activation overwrote the current source draft")
	}
	op, version, ok := operationDefinition(tn.app(build.ID), code.Name, 1)
	if !ok || version != 1 || op.Binding.Module != saved.Module {
		t.Fatal("activated compute did not use the frozen module")
	}
	if tn.uploads == nil {
		tn.uploads = map[string]time.Time{}
	}
	tn.uploads[saved.Module] = now.Add(-48 * time.Hour)
	tn.SweepUploads(now)
	if !tn.files().Exists(context.Background(), artifactKey(tn.ID, saved.Module)) {
		t.Fatal("ordinary upload sweeping removed the compute artifact")
	}
	// A later standalone activation cannot invalidate an existing exact binding.
	submit("edit2", build.CodeType+".edit", []byte(`{"source":"package main\nfunc Run(input Input)(Output,error){return Output{Value:input.Value*2},nil}"}`))
	submit("compile2", build.SchemaCodeCompile, []byte(`{}`))
	for _, run := range tn.operationDispatches(now) {
		run()
	}
	next, err := tn.PreviewRelease(member, platform.AssetCompute, "C")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.SaveReleaseCandidate(member, platform.AssetCompute, "C", next.CandidateID, "save2", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, next.CandidateID, "activate2", now); err != nil {
		t.Fatal(err)
	}
	old, refusal := tn.InvokeOperation(member, platform.OperationRequest{App: build.ID, Name: code.Name, Version: 1, Key: "old-page-binding", Inputs: json.RawMessage(`{"value":5}`)}, now)
	if refusal != nil || old.Version != 1 || old.Release != preview.CandidateID {
		t.Fatalf("retained binding chose current activation: %+v %v", old, refusal)
	}
}

func (s *computeStock) Manifest() platform.Manifest {
	m := s.stock.Manifest()
	m.Operations = []platform.Operation{{Name: "double", Title: "Double", Description: "Double an integer", Roles: []string{"clerk"}, Input: platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "integer"}}, Required: []string{"value"}}, Output: platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"doubled": {Type: "integer"}}, Required: []string{"doubled"}}, Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 512, MaxInputBytes: 4096, MaxOutputBytes: 4096}}}
	return m
}
func (s *computeStock) Compute(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	s.calls++
	var p struct{ Value int }
	_ = json.Unmarshal(input, &p)
	return json.Marshal(map[string]int{"doubled": p.Value * 2})
}

func TestOperationAcceptedGenerationReplay(t *testing.T) {
	compose := func() (*Tenant, *computeStock) {
		app := &computeStock{stock: newStock("operation-accepted")}
		tn, err := NewTenant("operation-accepted", NewConsole("operation-accepted", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}), app)
		if err != nil {
			t.Fatal(err)
		}
		return tn, app
	}
	tn, app := compose()
	member, _ := tn.Member("ana")
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	call, refusal := tn.InvokeOperation(member, platform.OperationRequest{App: "stock", Name: "double", Key: "double-1", Inputs: json.RawMessage(`{"value":4}`)}, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	retry, refusal := tn.InvokeOperation(member, platform.OperationRequest{App: "stock", Name: "double", Key: "double-1", Inputs: json.RawMessage(`{"value":4}`)}, now)
	if refusal != nil || retry.ID != call.ID || len(entries) != 1 {
		t.Fatalf("stable request did not return accepted intent: %v", refusal)
	}
	if app.calls != 0 {
		t.Fatal("compute ran under the accepting decision")
	}
	runs := tn.operationDispatches(now)
	if len(runs) != 1 {
		t.Fatalf("no owned computation: %d", len(runs))
	}
	runs[0]()
	result, err := tn.ReadOperation(member, call.ID)
	if err != nil || result.State != "completed" || string(result.Output) != `{"doubled":8}` || result.Generation != 1 || app.calls != 1 {
		t.Fatalf("operation did not commit a typed answer: %+v %v", result, err)
	}
	recovered, recoveredApp := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	saved, err := recovered.ReadOperation(member, call.ID)
	if err != nil || saved.State != "completed" || string(saved.Output) != string(result.Output) || recoveredApp.calls != 0 || len(recovered.operationDispatches(now)) != 0 {
		t.Fatalf("recovery reran compute or lost output: %+v %v", saved, err)
	}
	if _, err := tn.InvokeOperation(member, platform.OperationRequest{App: "stock", Name: "double", Key: "invalid-json", Inputs: json.RawMessage(`{"value":1,"value":2}`)}, now); err == nil {
		t.Fatal("duplicate input keys were accepted")
	}
}

func TestWasmCommandCompilerProfiles(t *testing.T) {
	compiler := ContainerCompiler{GoImage: os.Getenv("PLATFORM_GO_WASM_IMAGE"), TinyGoImage: os.Getenv("PLATFORM_TINYGO_WASM_IMAGE")}
	if compiler.GoImage == "" || compiler.TinyGoImage == "" {
		t.Skip("pinned Go/TinyGo compiler images are not configured")
	}
	input := platform.ValueSchema{Type: "variant", Nullable: true, Discriminator: "kind", Variants: map[string]platform.ValueSchema{"calculate": {Type: "object", Properties: map[string]platform.ValueSchema{"kind": {Type: "string", Enum: []string{"calculate"}}, "value": {Type: "integer"}, "note": {Type: "string", Nullable: true}}, Required: []string{"kind", "value", "note"}}}}
	output := platform.ValueSchema{Type: "variant", Nullable: true, Discriminator: "kind", Variants: map[string]platform.ValueSchema{"calculated": {Type: "object", Properties: map[string]platform.ValueSchema{"kind": {Type: "string", Enum: []string{"calculated"}}, "doubled": {Type: "integer"}, "note": {Type: "string", Nullable: true}}, Required: []string{"kind", "doubled", "note"}}}}
	engine := NewWasmEngine(2)
	defer engine.Close(context.Background())
	for _, language := range []string{"go", "tinygo"} {
		t.Run(language, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()
			build, module, err := compiler.Compile(ctx, platform.CodeBuildRequest{Language: language, Input: input, Output: output, Source: "package main\nfunc Run(input Input)(Output,error){if input==nil{return nil,nil};in:=input.AsCalculate;return &OutputValue{Variant:\"calculated\",AsCalculated:&OutputValueAsCalculated{Doubled:in.Value*2,Note:in.Note}},nil}"})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateWasm(ctx, module); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(module)
			hash := hex.EncodeToString(digest[:])
			limits := platform.OperationLimits{TimeoutMillis: 2000, MemoryPages: 1024, MaxInputBytes: 4096, MaxOutputBytes: 4096}
			result, err := engine.Execute(ctx, WasmRequest{Module: module, Digest: hash, Input: json.RawMessage(`{"kind":"calculate","value":7,"note":"slash\/\u0001\uD83D\uDE00"}`), Limits: limits})
			var saved struct {
				Kind    string
				Doubled int
				Note    *string
			}
			decodeErr := json.Unmarshal(result.Output, &saved)
			if err != nil || decodeErr != nil || saved.Kind != "calculated" || saved.Doubled != 14 || saved.Note == nil || *saved.Note != "slash/\x01😀" || build.Toolchain == "" {
				t.Fatalf("%s command failed: %+v %v", language, result, err)
			}
			if err := output.Validate(result.Output, 4096); err != nil {
				t.Fatal(err)
			}
			null, err := engine.Execute(ctx, WasmRequest{Module: module, Digest: hash, Input: json.RawMessage(`null`), Limits: limits})
			if err != nil || string(null.Output) != "null" {
				t.Fatalf("%s nullable variant: %+v %v", language, null, err)
			}
			t.Logf("%s module=%d bytes instantiate=%dµs execute=%dµs", language, len(module), result.InstantiateMicros, result.ExecuteMicros)
		})
	}
}
