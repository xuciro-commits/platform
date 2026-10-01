package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"slices"
	"testing"
	"time"

	"platformserver/platform"
)

type computeStock struct {
	*stock
	calls int
}

type fixtureCodeCompiler struct{}

func TestApplicationPreviewRetainsPublishedCompute(t *testing.T) {
	store := &memoryFiles{}
	compose := func() *Tenant {
		tn, err := NewTenant("compute-app", NewConsole("compute-app", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"operator-token"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}), work.New("compute-app"), flow.New("compute-app"), build.New("compute-app"))
		if err != nil {
			t.Fatal(err)
		}
		tn.Files = store
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Compiler = fixtureCodeCompiler{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member, _ := tn.Member("builder")
	now := time.Now().UTC()
	submit := func(typ, id, verb string, value any) {
		t.Helper()
		payload, _ := json.Marshal(value)
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: typ + id + verb, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: payload}, now); err != nil {
			t.Fatal(err)
		}
	}
	activate := func(kind platform.AssetKind, id string) ReleasePreview {
		t.Helper()
		preview, err := tn.PreviewRelease(member, kind, id)
		if err != nil || preview.CandidateID == "" {
			t.Fatalf("preview %s: %+v %v", kind, preview, err)
		}
		if _, err := tn.SaveReleaseCandidate(member, kind, id, preview.CandidateID, "save:"+id+":"+preview.CandidateID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate:"+id+":"+preview.CandidateID, now); err != nil {
			t.Fatal(err)
		}
		return preview
	}
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "integer"}}, Required: []string{"value"}}
	submit(build.CodeType, "C", "create", map[string]any{"name": "double", "title": "Double", "language": "go", "source": "package main\nfunc Run(input Input)(Output,error){return Output{Value:input.Value*2},nil}", "input": schema, "output": schema, "roles": []string{build.Builder, build.User}, "limits": platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 512, MaxInputBytes: 4096, MaxOutputBytes: 4096}})
	submit(build.CodeType, "C", "compile", map[string]any{})
	for _, run := range tn.operationDispatches(now) {
		run()
	}
	activate(platform.AssetCompute, "C")
	submit(build.ObjectType, "O", "create", map[string]any{"name": "sample", "title": "Sample", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "value", Title: "Value", Type: "integer"}}, "states": []build.State{{Name: "open", Title: "Open"}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	submit(build.ObjectType, "CHILD", "create", map[string]any{"name": "child", "title": "Child", "fields": []build.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.sample", Inverse: "children"}}})
	submit(build.ObjectType, "CHILD", "publish", map[string]any{})
	submit(build.PageType, "P", "create", map[string]any{"name": "computed", "title": "Computed", "object": "build.sample", "actions": []string{"build.sample.create"}, "sections": []build.Section{{Widget: "compute", Operation: &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: "double"}, SourceVersion: "1.compute-1"}}, {Widget: "table", Object: "build.child", Relation: "children", Fields: []string{"parent"}}, {Widget: "actions", Actions: []string{"build.sample.edit"}}}})
	submit(build.PageType, "P", "publish", map[string]any{})
	submit(build.AppType, "A", "create", map[string]any{"name": "computedapp", "title": "Computed app", "pages": []string{"computed"}})
	submit(build.AppType, "A", "publish", map[string]any{})
	preview := activate(platform.AssetApp, "A")
	found := false
	for _, ref := range preview.Included {
		found = found || ref.Kind == platform.AssetCompute && ref.Name == "double"
	}
	if !found {
		t.Fatal("application candidate omitted its published compute dependency")
	}
	// A child update validates the whole composed page. Its parent's actions
	// must remain owned by the parent, rather than being treated as removals.
	child, err := tn.PreviewRelease(member, platform.AssetObject, "CHILD")
	if err != nil || child.CandidateID == "" {
		t.Fatalf("related object preview: %+v %v", child, err)
	}
	submit(build.ProcessType, "FLOW", "create", map[string]any{"name": "calculate", "title": "Calculate", "object": "build.sample", "when": "open", "steps": []build.ProcessStep{{Name: "calculate", Kind: "compute", Operation: &build.OperationRef{App: build.ID, Name: "double", Version: 1}, Inputs: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"value"}}}, Next: "end"}, {Name: "end", Kind: "end"}}})
	submit(build.ProcessType, "FLOW", "publish", map[string]any{})
	resources := []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.sample"}, {App: build.ID, Kind: platform.AssetFlow, Name: "build.calculate"}, {App: build.ID, Kind: platform.AssetCompute, Name: "double"}}
	submit(build.AppType, "A", "edit", map[string]any{"resources": resources})
	preview = activate(platform.AssetApp, "A")
	if !slices.Contains(preview.Included, resources[1]) || !slices.Contains(preview.Included, resources[2]) {
		t.Fatalf("application omitted its explicit flow/compute: %v", preview.Included)
	}

	operator, _ := tn.Member("operator")
	if _, err := tn.Submit(operator, &pb.Submission{TenantId: tn.ID, PrincipalId: operator.ID, Authority: build.ID, IdempotencyKey: "operator:create", Target: &pb.EntityRef{Type: "build.sample", Id: "SAMPLE"}, Schema: &pb.SchemaRef{Name: "build.sample.create", Version: 1}, Payload: []byte(`{"name":"Sample","value":3}`)}, now); err != nil {
		t.Fatal(err)
	}
	tn.Work(now.Add(time.Second))
	instance, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "build.calculate:SAMPLE")
	if !ok || instance.State != "waiting" || instance.OnBehalf != operator.ID {
		t.Fatalf("operator's compute flow did not start: %+v", instance)
	}
	for _, source := range instance.Sources {
		if source == "build.sample/SAMPLE#id" || source == "build.sample/SAMPLE#revision" || source == "build.sample/SAMPLE#created" || source == "build.sample/SAMPLE#changed" {
			t.Fatalf("record metadata was treated as a declared source field: %s", source)
		}
	}
	CheckReplay(t, tn, entries, compose)
}

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
	// Dispatch completion records wall time; do not dispatch the next build
	// from the fixture's earlier clock.
	if completedAt := time.Now().UTC(); completedAt.After(now) {
		now = completedAt
	}
	secondBuild := tn.operationDispatches(now)
	if len(secondBuild) != 1 {
		t.Fatalf("second compiler task was not admitted: %d", len(secondBuild))
	}
	for _, run := range secondBuild {
		run()
	}
	if current, ok := platform.Get[build.Code](tn.automation(build.ID, false), "C"); !ok || current.State != "compiled" {
		t.Fatalf("second build did not finish: %+v", current)
	}
	next, err := tn.PreviewRelease(member, platform.AssetCompute, "C")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.SaveReleaseCandidate(member, platform.AssetCompute, "C", next.CandidateID, "save2", now); err != nil {
		t.Fatalf("save second candidate: %v; preview: %+v", err, next)
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
