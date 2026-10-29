package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"platformserver/engine/resources"
	acceptedformat "platformserver/kernel/acceptedformat"

	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"

	"platformserver/platform"
)

// The source fixture enters through one ordinary accepted decision. Flow
// remains the only instance owner; the source keeps no second runtime state.
type frameSource struct {
	ledger          *platform.Ledger
	host            hostView
	stateBytes      int
	checkpointEvery int
	compute         bool
	retryCompute    bool
	computeFailures int
	largeInput      bool
	autoStart       bool
	autoEnabled     bool
	module          []byte
	moduleDigest    string
	computeInputs   []json.RawMessage
	windowOutputs   []map[string]json.RawMessage
}

type frameStream struct{ platform.Record }

func newFrameSource(tenant string, stateBytes int) *frameSource {
	return &frameSource{stateBytes: stateBytes, ledger: platform.NewLedger(tenant, "frames", platform.NewCatalog(
		platform.Action{Schema: "frames.source.start", Target: "frames.source", Title: "Start", Description: "Start the source's original Flow instance", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.source.feed", Target: "frames.source", Title: "Feed", Description: "Accept one source batch through its original Flow instance", Roles: []string{"operator"}, Payload: []platform.Field{{Name: "batch", Type: "json", Required: true}}}), "frames.source")}
}
func newFrameComputeSource(tenant string, stateBytes int) *frameSource {
	s := newFrameSource(tenant, stateBytes)
	s.compute = true
	return s
}
func newFrameRetryComputeSource(tenant string, stateBytes int) *frameSource {
	s := newFrameComputeSource(tenant, stateBytes)
	s.retryCompute, s.computeFailures = true, 1
	return s
}
func newFrameDataComputeSource(tenant string, stateBytes int) *frameSource {
	s := newFrameComputeSource(tenant, stateBytes)
	s.largeInput = true
	s.module = []byte("fixture-wasm-module")
	s.moduleDigest = fmt.Sprintf("%x", sha256.Sum256(s.module))
	return s
}

type frameDataWorker struct {
	calls int
	data  []byte
}

func (w *frameDataWorker) Execute(_ context.Context, request platform.WasmRequest) (platform.WasmResponse, error) {
	if request.Data == nil || len(request.Input) != 0 || request.Limits.DataInputBytes <= request.Limits.MaxInputBytes {
		return platform.WasmResponse{}, fmt.Errorf("the large Compute input did not use the declared data channel")
	}
	w.calls++
	w.data = slices.Clone(request.Data.Bytes)
	return platform.WasmResponse{Output: platform.Raw(map[string]int{"bytes": len(request.Data.Bytes)})}, nil
}

func (s *frameSource) AttachBuild(h platform.BuildHost) { s.host = h.(hostView) }
func (s *frameSource) Manifest() platform.Manifest {
	continuous := &platform.Continuous{
		Source: "frames.source", Batch: 512, State: s.stateBytes, FrameBytes: 2 * s.stateBytes, CheckpointEvery: s.checkpointEvery, DeadLetter: true,
		Window: &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"},
	}
	start := platform.Start{Manual: true}
	if s.autoStart {
		continuous.Intake = &platform.StreamIntake{SourceRecord: "stream", Key: "eventId", Partition: []string{"plantId"}, EventTime: "eventAt", Value: "reading", Offset: "latest"}
		start = platform.Start{Continuous: true, OnBehalf: "member", Enabled: func(platform.Caller) bool { return s.autoEnabled }}
	}
	steps := []platform.Step{{Name: "intake", Wait: &platform.Wait{Until: func(platform.Caller, *platform.Run) bool { return false }}}}
	var operations []platform.Operation
	if s.compute {
		continuous.Entry = "aggregate"
		var retry *platform.RetryPolicy
		if s.retryCompute {
			retry = &platform.RetryPolicy{Attempts: 2, Backoff: time.Millisecond}
		}
		steps = []platform.Step{
			{Name: "aggregate", Next: "finish", Retry: retry, Operation: &platform.OperationStep{Request: func(_ platform.Caller, run *platform.Run) (platform.OperationRequest, *kernel.Error) {
				var input automation.ContinuousWindowInput
				if json.Unmarshal(run.Data, &input) != nil {
					return platform.OperationRequest{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "window input is malformed")
				}
				previous := make(map[string]json.RawMessage, len(input.Outputs))
				for name, raw := range input.Outputs {
					previous[name] = slices.Clone(raw)
				}
				s.windowOutputs = append(s.windowOutputs, previous)
				if s.largeInput {
					if len(input.Signals) == 0 {
						return platform.OperationRequest{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "the window has no signal")
					}
					var payload string
					if json.Unmarshal(input.Signals[0].Value, &payload) != nil {
						return platform.OperationRequest{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "the signal payload is not text")
					}
					return platform.OperationRequest{App: "frames", Name: "aggregate", Inputs: platform.Raw(map[string]string{"payload": payload})}, nil
				}
				return platform.OperationRequest{App: "frames", Name: "aggregate", Inputs: platform.Raw(map[string]int{"signalCount": len(input.Signals)})}, nil
			}}},
			{Name: "finish", End: true},
		}
		operation := platform.Operation{
			Name: "aggregate", Title: "Aggregate window", Description: "Count events in a ready window", Roles: []string{"operator"},
			Input:   platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"signalCount": {Type: "integer"}}, Required: []string{"signalCount"}},
			Output:  platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"count": {Type: "integer"}}, Required: []string{"count"}},
			Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 64, MaxInputBytes: 4096, MaxOutputBytes: 4096},
		}
		if s.largeInput {
			operation.Input = platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"payload": {Type: "string", MaxLength: 200000}}, Required: []string{"payload"}}
			operation.Output = platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"bytes": {Type: "integer"}}, Required: []string{"bytes"}}
			operation.Binding = platform.OperationBinding{Kind: "wasm", Module: s.moduleDigest, ABI: platform.WasmDataABI}
			operation.Limits = platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 64, MaxInputBytes: 4096, MaxOutputBytes: 4096, StagedOutputBytes: 8192, DataInputBytes: 256 << 10}
		}
		operations = []platform.Operation{operation}
	}
	fl := platform.Flow{Name: "window", Title: "Accepted window", Version: 1, Start: start, Continuous: continuous, Steps: steps}
	return platform.Manifest{
		ID: "frames", Version: "1", Actions: s.ledger.Catalog,
		Entities:   []platform.Entity{{Type: "frames.source", Title: "Source", Model: frameStream{}}},
		Flows:      []platform.Flow{fl},
		Operations: operations,
	}
}
func (s *frameSource) Compute(_ context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	if !s.compute || name != "aggregate" {
		return nil, fmt.Errorf("unknown computation %s", name)
	}
	s.computeInputs = append(s.computeInputs, slices.Clone(raw))
	if s.computeFailures > 0 {
		s.computeFailures--
		return nil, fmt.Errorf("transient Compute failure")
	}
	var input struct {
		SignalCount int `json:"signalCount"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]int{"count": input.SignalCount})
}
func (s *frameSource) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *frameSource) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *frameSource) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *frameSource) AcceptedLedger() *platform.Ledger         { return s.ledger }
func (*frameSource) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "unknown read")
}
func (*frameSource) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "unknown input")
}
func (s *frameSource) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return s.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var err *kernel.Error
		if sub.GetSchema().GetName() == "frames.source.start" {
			start := s.host.Processes().(interface {
				StartManual(platform.Caller, string, string, int, string, json.RawMessage, time.Time) *kernel.Error
			})
			err = start.StartManual(c, "frames", "window", 1, "source", json.RawMessage(`{}`), now)
		} else {
			var input struct {
				Batch automation.Batch `json:"batch"`
			}
			if json.Unmarshal(sub.GetPayload(), &input) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "invalid batch")
			}
			consume := s.host.Processes().(interface {
				ConsumeBatch(platform.Caller, string, automation.Batch, time.Time) (automation.BatchOutcome, *kernel.Error)
			})
			_, err = consume.ConsumeBatch(s.host.Automation(c, automation.ID), "frames.window:source", input.Batch, now)
		}
		if err != nil {
			return nil, err
		}
		return func(*pb.ChangeRecord) {}, nil
	})
}

func TestContinuousFlowAutoStartsOnceAndStopIsDurable(t *testing.T) {
	const tenant, id = "continuous-auto-start", "frames.window:continuous-v1"
	source := newFrameSource(tenant, 128<<10)
	source.autoStart = true
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	at := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	runJob(t, tn, automation.ID, "timers", at)
	if _, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id); ok {
		t.Fatal("a continuous flow started while its published Process owner was inactive")
	}
	source.autoEnabled = true
	runJob(t, tn, automation.ID, "timers", at.Add(time.Second))
	instance, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !ok || instance.State != "waiting" || instance.OnBehalf != "member" || len(instance.Tokens) != 1 || instance.Tokens[0].Waits != "stream" {
		t.Fatalf("published stream did not start one retained waiting instance: %+v", instance)
	}
	runJob(t, tn, automation.ID, "timers", at.Add(2*time.Second))
	if _, duplicate := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id+"#2"); duplicate {
		t.Fatal("the continuous start tick created a duplicate instance")
	}
	decide(t, tn, "member", automation.ID, automation.SchemaFlowStop, automation.InstanceType, id, map[string]any{}, at.Add(3*time.Second))
	runJob(t, tn, automation.ID, "timers", at.Add(4*time.Second))
	stopped, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !ok || stopped.State != "canceled" {
		t.Fatalf("the explicit stop did not remain terminal: %+v", stopped)
	}
	if _, restarted := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id+"#2"); restarted {
		t.Fatal("a durable stop was silently restarted by the timer")
	}
}

func TestNativeFlowApplicationArtifactUsesOriginalDescriptor(t *testing.T) {
	const tenant = "native-flow-application"
	source := newFrameSource(tenant, 128<<10)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	root := platform.AssetRef{App: "frames", Kind: platform.AssetApp, Name: "frames"}
	assets, err := tn.releaseAssetsLocked(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := tn.candidateWithBindings([]platform.AssetRef{root}, assets, []platform.AssetRef{root})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := platform.ArtifactOfApplication(root, closed.Assets)
	if err != nil || len(artifact.Flows) != 1 || len(artifact.Model) != 1 || len(artifact.Rules) != 2 {
		t.Fatalf("native application is not a closed artifact: %+v %v", artifact, err)
	}
	var flowAsset platform.ReleaseAsset
	for _, asset := range closed.Assets {
		if asset.Ref.Kind == platform.AssetFlow {
			flowAsset = asset
		}
	}
	var descriptor platform.FlowReleaseDescriptor
	var definition struct {
		platform.FlowDefinition
		Continuous *platform.Continuous `json:"continuous"`
		Mode       string               `json:"mode"`
	}
	if json.Unmarshal(flowAsset.Body, &descriptor) != nil || !descriptor.Native || json.Unmarshal(descriptor.Definition, &definition) != nil || definition.Version != 1 || definition.Mode != "manual" || definition.Continuous.FrameBytes != 256<<10 {
		t.Fatalf("flow owner declaration was lost: %s", flowAsset.Body)
	}
	registry, err := tn.assetRegistryLocked()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := registry.Owner(flowAsset.Ref)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := owner.ResolvePinned(flowAsset.Ref, flowAsset.SourceVersion)
	if err != nil || !evalEqualRaw(retained.Body, flowAsset.Body) {
		t.Fatal("native version did not resolve to its original owner")
	}
	descriptor.Definition = json.RawMessage(`{"id":"frames.window","version":1,"mode":"manual"}`)
	flowAsset.Body = platform.Raw(descriptor)
	if _, err := owner.PrepareReleaseAssets([]platform.ReleaseAsset{flowAsset}, platform.AssetReleaseContext{}); err == nil {
		t.Fatal("changed native declaration bypassed its original compiled owner")
	}
}

func TestContinuousWindowRunsRetainedComputeWithBackpressure(t *testing.T) {
	const tenant, id = "continuous-window-compute", "frames.window:source"
	source := newFrameComputeSource(tenant, 128<<10)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	store := &resources.MemoryFiles{}
	tn.Files = store
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	read := func() automation.FlowInstance {
		t.Helper()
		x, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
		if !ok {
			t.Fatal("continuous Compute instance was not created")
		}
		return x
	}
	first := automation.Batch{ID: "window-1", Signals: []automation.Signal{{Key: "A", Partition: "plant", At: at.Add(-time.Second), Value: platform.Raw(4)}}}
	acceptedAt := at.Add(3 * time.Second)
	if _, err := tn.ConsumeFlowBatch(member, id, first, acceptedAt); err != nil {
		t.Fatalf("first window batch: %v", err)
	}
	accepted := read()
	if accepted.State != "waiting" || accepted.Data != "{}" || accepted.Batch == nil || accepted.Batch.Cursor != first.ID {
		t.Fatalf("window input changed shared Flow data or failed to accept its cursor: %+v", accepted)
	}
	var continuation json.RawMessage
	for _, token := range accepted.Tokens {
		if token.Waits == "operation" {
			continuation = slices.Clone(token.Input)
		}
	}
	var priorState struct {
		Outputs map[string]json.RawMessage `json:"outputs"`
		Signals []automation.Signal        `json:"signals"`
	}
	if len(accepted.Tokens) != 2 || !slices.ContainsFunc(accepted.Tokens, func(token automation.Token) bool { return token.Waits == "stream" }) || len(continuation) == 0 || json.Unmarshal(continuation, &priorState) != nil || priorState.Outputs == nil || len(priorState.Signals) != 0 {
		t.Fatalf("Compute token was not retained with bounded continuation state beside the idle source token: %+v", accepted.Tokens)
	}
	second := automation.Batch{ID: "window-2", Predecessor: first.ID, Signals: []automation.Signal{{Key: "B", Partition: "plant", At: at.Add(time.Second), Value: platform.Raw(5)}}}
	if _, err := tn.ConsumeFlowBatch(member, id, second, at.Add(time.Second)); err == nil {
		t.Fatal("source advanced while its previous Compute operation was running")
	}
	if current := read(); current.Batch.Cursor != first.ID || current.Revision != accepted.Revision {
		t.Fatal("backpressure refusal advanced the accepted window")
	}
	runs := tn.operationDispatches(acceptedAt)
	if len(runs) != 1 {
		t.Fatalf("ready window did not enqueue exactly one Compute operation: %d", len(runs))
	}
	runs[0]()
	computed := read()
	if computed.State != "waiting" || len(computed.Tokens) != 1 || computed.Tokens[0].Waits != "stream" || computed.Data != "{}" {
		t.Fatalf("OperationEnded did not resume the same long-lived flow: %+v", computed)
	}
	var aggregate map[string]int
	if json.Unmarshal(computed.Outputs["aggregate"], &aggregate) != nil || aggregate["count"] != 1 || len(source.computeInputs) != 1 {
		t.Fatalf("Compute result was not retained as the latest step output: %s", computed.Outputs["aggregate"])
	}
	var used struct {
		SignalCount int `json:"signalCount"`
	}
	if json.Unmarshal(source.computeInputs[0], &used) != nil || used.SignalCount != 1 {
		t.Fatalf("Compute received the wrong window projection: %s", source.computeInputs[0])
	}
	if len(source.windowOutputs) != 1 || len(source.windowOutputs[0]) != 0 {
		t.Fatalf("the first window should receive an empty prior-output state: %+v", source.windowOutputs)
	}
	if _, err := tn.ConsumeFlowBatch(member, id, second, at.Add(8*time.Second)); err != nil {
		t.Fatalf("second window batch after completion: %v", err)
	}
	nextRuns := tn.operationDispatches(time.Now().UTC().Add(time.Second))
	if len(nextRuns) != 1 {
		current := read()
		t.Fatalf("next ready window did not schedule a fresh retained Compute operation: tokens=%+v", current.Tokens)
	}
	var previousOutput map[string]int
	if len(source.windowOutputs) != 2 || json.Unmarshal(source.windowOutputs[1]["aggregate"], &previousOutput) != nil || previousOutput["count"] != 1 {
		t.Fatalf("the next window did not receive the previous accepted Flow output: %+v", source.windowOutputs)
	}
	nextRuns[0]()
	computed = read()
	if json.Unmarshal(computed.Outputs["aggregate"], &aggregate) != nil || aggregate["count"] != 2 || len(source.computeInputs) != 2 {
		t.Fatalf("second ready window did not replace the latest Compute output: %s", computed.Outputs["aggregate"])
	}
	CheckReplay(t, tn, entries, func() *Tenant {
		return composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), newFrameComputeSource(tenant, 128<<10))
	})
}

func TestContinuousWindowRetriesComputeFromItsAcceptedSealedFrame(t *testing.T) {
	const tenant, id = "continuous-window-compute-retry", "frames.window:source"
	source := newFrameRetryComputeSource(tenant, 128<<10)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	tn.Files = &resources.MemoryFiles{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	batch := automation.Batch{ID: "retry-window", Signals: []automation.Signal{{Key: "A", Partition: "plant", At: at.Add(-time.Second), Value: platform.Raw(7)}}}
	acceptedAt := at.Add(3 * time.Second)
	if _, err := tn.ConsumeFlowBatch(memberOf(t, tn, "member"), id, batch, acceptedAt); err != nil {
		t.Fatalf("retry window batch: %v", err)
	}
	runs := tn.operationDispatches(acceptedAt)
	if len(runs) != 1 {
		t.Fatalf("ready window did not enqueue its initial Compute: %d", len(runs))
	}
	runs[0]()
	instance, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !ok || !slices.ContainsFunc(instance.Tokens, func(token automation.Token) bool {
		return token.Step == "aggregate" && token.Waits == "retry" && token.Attempts == 1
	}) {
		t.Fatalf("the transient Compute failure did not retain its step retry: %+v", instance.Tokens)
	}
	retryAt := time.Now().UTC().Add(time.Second)
	tn.PullContinuousSources(retryAt)
	retries := tn.operationDispatches(retryAt.Add(time.Second))
	if len(retries) != 1 {
		t.Fatalf("the accepted sealed window did not enqueue one retry: %d", len(retries))
	}
	retries[0]()
	instance, _ = platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	var output map[string]int
	if instance.Batch.Cursor != batch.ID || instance.Data != "{}" || instance.State != "waiting" || len(instance.Tokens) != 1 || instance.Tokens[0].Waits != "stream" || len(source.computeInputs) != 2 || !slices.Equal(source.computeInputs[0], source.computeInputs[1]) || json.Unmarshal(instance.Outputs["aggregate"], &output) != nil || output["count"] != 1 {
		t.Fatalf("the retry did not reuse the exact sealed window and settle the original Flow: %+v", instance)
	}
}

func TestContinuousWindowSealsLargeComputeInput(t *testing.T) {
	const tenant, id = "continuous-window-data-input", "frames.window:source"
	source := newFrameDataComputeSource(tenant, 256<<10)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	store := &resources.MemoryFiles{}
	tn.Files = store
	worker := &frameDataWorker{}
	tn.ComputeWorker = worker
	if err := store.Put(context.Background(), artifactKey(tenant, source.moduleDigest), source.module, "application/wasm"); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	text := strings.Repeat("x", 90<<10)
	value, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	batch := automation.Batch{ID: "large-window", Signals: []automation.Signal{{Key: "A", Partition: "plant", At: at.Add(-time.Second), Value: value}}}
	if _, err := tn.ConsumeFlowBatch(member, id, batch, at.Add(3*time.Second)); err != nil {
		t.Fatalf("large window Compute batch: %v", err)
	}
	instance, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !ok || instance.Data != "{}" || instance.Batch == nil || instance.Batch.Sealed == nil || len(platform.Raw(instance.Batch)) > 2048 {
		t.Fatal("the large window or operation input was copied into the durable Flow record")
	}
	if worker.calls != 0 {
		t.Fatal("the worker ran inside the accepted source decision")
	}
	tn.opsMu.Lock()
	var inputRef *platform.OperationInputArtifact
	for _, item := range tn.outbound {
		if item.Endpoint != acceptedformat.OperationEndpoint {
			continue
		}
		var binding operationBinding
		if json.Unmarshal([]byte(item.Body), &binding) != nil || binding.SealedInput == nil || len(binding.Inputs) != 0 {
			tn.opsMu.Unlock()
			t.Fatal("the operation intent did not reference the sealed per-call input")
		}
		copy := *binding.SealedInput
		inputRef = &copy
	}
	tn.opsMu.Unlock()
	if inputRef == nil || inputRef.Tenant != tenant || inputRef.Member != member.ID || inputRef.Size <= 4096 || inputRef.Size > 256<<10 {
		t.Fatalf("the operation input handle has a wrong owner or bound: %+v", inputRef)
	}
	sealed, err := tn.staged.ReadInput(context.Background(), *inputRef)
	if err != nil || len(sealed) != inputRef.Size {
		t.Fatalf("the operation input could not be resolved: %v", err)
	}
	for _, entry := range entries {
		if len(entry.Body) > platform.AcceptedResultMaxBytes {
			t.Fatal("the large window input was copied into the accepted journal result")
		}
	}
	runs := tn.operationDispatches(time.Now().UTC().Add(time.Second))
	if len(runs) != 1 {
		t.Fatalf("the sealed operation was not dispatched once: %d", len(runs))
	}
	runs[0]()
	if worker.calls != 1 || string(worker.data) != string(sealed) {
		t.Fatal("the worker did not receive exactly the accepted call's sealed bytes")
	}
	instance, _ = platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	var output map[string]int
	if json.Unmarshal(instance.Outputs["aggregate"], &output) != nil || output["bytes"] != len(sealed) {
		t.Fatalf("the operation result did not resume into the original Flow output: %s", instance.Outputs["aggregate"])
	}
}

func TestContinuousAcceptedFrameRefusalAndRecovery(t *testing.T) {
	const tenant = "accepted-frames"
	compose := func() *Tenant {
		return composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), newFrameSource(tenant, 4096))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	first := automation.Batch{ID: "one", Signals: []automation.Signal{{Key: "A", Partition: "plant", At: at, Value: platform.Raw(4)}}}
	decide(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": first}, at)
	read := func() automation.FlowInstance {
		x, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), "frames.window:source")
		if !ok {
			t.Fatal("no native instance")
		}
		return x
	}
	before := read()
	tooLarge := automation.Batch{ID: "two", Predecessor: "one", Signals: []automation.Signal{{Key: "B", Partition: "plant", At: at.Add(time.Second), Value: platform.Raw(strings.Repeat("x", 6000))}}}
	if refusal := refuse(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": tooLarge}, at.Add(time.Second)); refusal == "ok" {
		t.Fatal("over-budget accepted work consumed source input")
	}
	after := read()
	if after.Revision != before.Revision || string(platform.Raw(after.Batch)) != string(platform.Raw(before.Batch)) || string(platform.Raw(after.Outputs)) != string(platform.Raw(before.Outputs)) {
		t.Fatal("refused accepted work published a partial frame")
	}
	decide(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": first}, at.Add(time.Second))
	if read().Revision != before.Revision {
		t.Fatal("identical input generated another flow change")
	}
	changed := first
	changed.Signals = []automation.Signal{{Key: "A", Partition: "plant", At: at, Value: platform.Raw(40)}}
	if refuse(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": changed}, at.Add(time.Second)) == "ok" {
		t.Fatal("reused batch identity accepted different bytes")
	}
	second := automation.Batch{ID: "two", Predecessor: "one", Signals: []automation.Signal{{Key: "B", Partition: "plant", At: at.Add(5 * time.Second), Value: platform.Raw(5)}}}
	decide(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": second}, at.Add(5*time.Second))
	if read().Batch.Cursor != "two" || read().Batch.Consumed != 2 {
		t.Fatal("valid successor could not resume after refusal")
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	copy, ok := platform.Get[automation.FlowInstance](restored.automation(automation.ID, false), "frames.window:source")
	if !ok || string(platform.Raw(copy.Batch)) != string(platform.Raw(read().Batch)) {
		t.Fatal("snapshot changed the accepted window")
	}

	t.Run("large window keeps bytes in the original frame", func(t *testing.T) {
		const tenant = "large-accepted-frames"
		compose := func() *Tenant {
			return composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), newFrameSource(tenant, 128<<10))
		}
		live := compose()
		var entries []Entry
		live.Record = func(e Entry) { entries = append(entries, e) }
		live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
		decide(t, live, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
		cursor := ""
		for n := 1; n <= 9; n++ {
			batch := automation.Batch{ID: fmt.Sprintf("%d", n), Predecessor: cursor, Signals: []automation.Signal{{Key: fmt.Sprintf("%d", n), Partition: "plant", At: at, Value: platform.Raw(strings.Repeat("x", 6000))}}}
			decide(t, live, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": batch}, at)
			cursor = batch.ID
		}
		x, ok := platform.Get[automation.FlowInstance](live.automation(automation.ID, false), "frames.window:source")
		if !ok || len(x.Batch.State["window"]) <= 48<<10 {
			t.Fatal("fixture did not retain a window above the inline budget")
		}
		var ref automation.BatchStateReference
		if err := json.Unmarshal(x.Outputs["state:window"], &ref); err != nil || ref.Instance != x.ID || ref.Node != "window" || ref.Size != len(x.Batch.State["window"]) || ref.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(x.Batch.State["window"])) {
			t.Fatalf("output does not identify its original accepted bytes: %+v %v", ref, err)
		}
		if len(platform.Raw(x.Outputs)) > 60<<10 {
			t.Fatal("large state bypassed the existing output budget")
		}
		// Retained history is checked against its predecessor, but not copied
		// into each new accepted result. The original result budget still holds.
		batch := automation.Batch{ID: "ten", Predecessor: cursor, Signals: []automation.Signal{{Key: "ten", Partition: "plant", At: at, Value: platform.Raw(strings.Repeat("x", 6000))}}}
		decide(t, live, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": batch}, at)
		after, ok := platform.Get[automation.FlowInstance](live.automation(automation.ID, false), x.ID)
		if !ok || after.Batch.Cursor != "ten" {
			t.Fatal("history growth blocked a bounded successor")
		}
		CheckReplay(t, live, entries, compose)
	})
}

func TestContinuousLatestBootstrapWithComputeEntry(t *testing.T) {
	const tenant = "latest-compute"
	source := newFrameComputeSource(tenant, 128<<10)
	source.autoStart, source.autoEnabled = true, true
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
	tn.Files = &resources.MemoryFiles{}
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	procs := tn.app(automation.ID).(*automation.Flows)
	runJob(t, tn, automation.ID, "timers", at)
	x, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), "frames.window:continuous-v1")
	if !ok {
		t.Fatal("continuous instance did not start")
	}
	plan, refusal := procs.PlanBatch(x, tn.automation(automation.ID, false))
	if refusal != nil {
		t.Fatal(refusal)
	}
	batch := automation.Batch{ID: "bootstrap", Bootstrap: true, Source: &automation.SourceCheckpoint{Record: "stream", Config: "hash", Cursor: "42", Offset: "latest", Initialized: true}}
	prepared, refusal := plan.Prepare(batch, at, flowFrameStore{tenant: tenant, files: tn.Files})
	if refusal != nil {
		t.Fatalf("latest bootstrap was refused: %v", refusal)
	}
	if _, invoked := prepared.OperationRequest(); invoked {
		t.Fatal("bootstrap invoked Compute without an event-time window")
	}
	raw, err := (flowFrameStore{tenant: tenant, files: tn.Files}).Read(prepared.Artifact())
	if err != nil {
		t.Fatal(err)
	}
	var frame automation.BatchFrame
	if err := json.Unmarshal(raw, &frame); err != nil || frame.Source == nil || frame.Source.Cursor != "42" || frame.Consumed != 0 {
		t.Fatalf("bootstrap lost the source position: %+v %v", frame, err)
	}
}

type transientDeleteFiles struct {
	*resources.MemoryFiles
	failures int
}

func (s *transientDeleteFiles) Delete(ctx context.Context, key string) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("injected object-store delete failure")
	}
	return s.MemoryFiles.Delete(ctx, key)
}

func TestContinuousArtifactRetirementRetriesAndUsesHostAcceptedWork(t *testing.T) {
	const tenant, id = "continuous-artifact-retirement", "frames.window:source"
	files := &transientDeleteFiles{MemoryFiles: &resources.MemoryFiles{}}
	compose := func() *Tenant {
		source := newFrameSource(tenant, 2<<20)
		source.checkpointEvery = 2
		tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
		tn.Files = files
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	feed := func(batch automation.Batch, now time.Time) automation.FlowInstance {
		t.Helper()
		if _, err := tn.ConsumeFlowBatch(member, id, batch, now); err != nil {
			t.Fatalf("accept source batch %s: %v", batch.ID, err)
		}
		x, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
		if !ok || x.Batch == nil || x.Batch.Sealed == nil {
			t.Fatal("accepted batch did not retain its sealed frame")
		}
		return x
	}
	stored := func(ref platform.FlowStateArtifact) bool {
		t.Helper()
		key, err := (flowFrameStore{tenant: tenant, files: files}).key(ref)
		if err != nil {
			t.Fatal(err)
		}
		return files.Exists(context.Background(), key)
	}
	one := feed(automation.Batch{ID: "one", Signals: []automation.Signal{{Key: "one", Partition: "plant", At: at, Value: platform.Raw(1)}}}, at)
	firstFrame := *one.Batch.Sealed
	two := feed(automation.Batch{ID: "two", Predecessor: "one", Signals: []automation.Signal{{Key: "two", Partition: "plant", At: at.Add(time.Second), Value: platform.Raw(2)}}}, at.Add(time.Second))
	firstCheckpoint := *two.Batch.CheckpointArtifact
	if !two.CleanupPending || len(two.ArtifactCleanup) != 1 || two.ArtifactCleanup[0] != firstFrame || !stored(firstFrame) {
		t.Fatalf("the accepted replacement did not durably identify its superseded frame: %+v", two.ArtifactCleanup)
	}
	if !stored(*two.Batch.Sealed) || !stored(firstCheckpoint) {
		t.Fatal("the live frame and checkpoint must remain stored")
	}
	if result := refuse(t, tn, "member", automation.ID, automation.SchemaFlowArtifactCleanup, automation.InstanceType, id, map[string]string{"digest": "forged"}, at.Add(time.Second)); !strings.Contains(result, pb.ErrorCode_ERROR_CODE_POLICY_DENIED.String()) {
		t.Fatalf("a member submitted the host-only cleanup action: %s", result)
	}

	f := tn.procs.(*automation.Flows)
	cleanup, refusal := f.PlanArtifactCleanup(two, at.Add(2*time.Second))
	if refusal != nil {
		t.Fatal(refusal)
	}
	files.failures = 1
	if refusal := tn.retireContinuousArtifacts(cleanup, at.Add(2*time.Second)); refusal == nil {
		t.Fatal("a transient object-store failure must leave cleanup unacknowledged")
	}
	stillPending, _ := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !stillPending.CleanupPending || stillPending.Revision != two.Revision || !stored(firstFrame) {
		t.Fatal("a failed delete cleared the durable retry intent or removed the object")
	}

	three := feed(automation.Batch{ID: "three", Predecessor: "two", Signals: []automation.Signal{{Key: "three", Partition: "plant", At: at.Add(2 * time.Second), Value: platform.Raw(3)}}}, at.Add(3*time.Second))
	thirdFrame := *three.Batch.Sealed
	if !three.CleanupPending || len(three.ArtifactCleanup) != 1 || three.ArtifactCleanup[0] != *two.Batch.Sealed || stored(firstFrame) {
		t.Fatalf("a retried cleanup did not precede the next accepted batch: %+v", three.ArtifactCleanup)
	}
	four := feed(automation.Batch{ID: "four", Predecessor: "three", Signals: []automation.Signal{{Key: "four", Partition: "plant", At: at.Add(3 * time.Second), Value: platform.Raw(4)}}}, at.Add(4*time.Second))
	if !four.CleanupPending || len(four.ArtifactCleanup) != 2 || four.ArtifactCleanup[0] != thirdFrame || four.ArtifactCleanup[1] != firstCheckpoint {
		t.Fatalf("checkpoint rollover did not retire both replaced artifacts: %+v", four.ArtifactCleanup)
	}
	if !stored(thirdFrame) || !stored(firstCheckpoint) {
		t.Fatal("accepted cleanup intent must not delete artifacts before host work")
	}

	// The original shared host source loop discovers and retries cleanup even
	// when this Flow has no further source records to consume.
	tn.PullContinuousSources(at.Add(5 * time.Second))
	cleaned, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
	if !ok || cleaned.CleanupPending || len(cleaned.ArtifactCleanup) != 0 || stored(thirdFrame) || stored(firstCheckpoint) {
		t.Fatalf("host cleanup did not delete and acknowledge the superseded frame/checkpoint: %+v", cleaned.ArtifactCleanup)
	}
	if !stored(*cleaned.Batch.Sealed) || !stored(*cleaned.Batch.CheckpointArtifact) {
		t.Fatal("host cleanup removed a live Flow artifact")
	}
	CheckReplay(t, tn, entries, compose)
}

func TestContinuousPeriodicCheckpointIsRetainedAndVersionBound(t *testing.T) {
	const tenant, id = "periodic-checkpoints", "frames.window:source"
	store := &resources.MemoryFiles{}
	compose := func() *Tenant {
		source := newFrameSource(tenant, 2<<20)
		source.checkpointEvery = 2
		tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), source)
		tn.Files = store
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	feed := func(batch automation.Batch, now time.Time) {
		t.Helper()
		if _, err := tn.ConsumeFlowBatch(member, id, batch, now); err != nil {
			t.Fatalf("accept source batch %s: %v", batch.ID, err)
		}
	}
	read := func() automation.FlowInstance {
		t.Helper()
		x, ok := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
		if !ok {
			t.Fatal("the original Flow instance was not retained")
		}
		return x
	}
	first := automation.Batch{ID: "one", Signals: []automation.Signal{{Key: "one", Partition: "plant", At: at, Value: platform.Raw(1)}}}
	feed(first, at)
	if x := read(); x.Batch.CheckpointArtifact != nil || x.Batch.BatchesSinceCheckpoint != 1 || x.Batch.CheckpointEvery != 2 {
		t.Fatalf("checkpoint was not scheduled at the declared interval: %+v", x.Batch)
	}
	second := automation.Batch{ID: "two", Predecessor: "one", Signals: []automation.Signal{{Key: "two", Partition: "plant", At: at.Add(time.Second), Value: platform.Raw(2)}}}
	feed(second, at.Add(time.Second))
	x := read()
	if x.Batch.CheckpointArtifact == nil || x.Batch.Sealed == nil || x.Batch.CheckpointCursor != "two" || x.Batch.BatchesSinceCheckpoint != 0 || x.Batch.CheckpointEvery != 2 || x.Batch.CheckpointArtifact.Ticket == x.Batch.Sealed.Ticket {
		t.Fatalf("the second accepted batch did not create a distinct periodic checkpoint: %+v", x.Batch)
	}
	checkpointRaw, err := (flowFrameStore{tenant: tenant, files: store}).Read(*x.Batch.CheckpointArtifact)
	var checkpoint automation.BatchFrame
	if err != nil || json.Unmarshal(checkpointRaw, &checkpoint) != nil || checkpoint.Cursor != "two" || checkpoint.CheckpointCursor != "two" || checkpoint.CheckpointArtifact != nil || checkpoint.Sealed != nil || len(checkpoint.State) == 0 {
		t.Fatalf("the periodic checkpoint is not a self-contained versioned frame: %+v %v", checkpoint, err)
	}
	third := automation.Batch{ID: "three", Predecessor: "two", Signals: []automation.Signal{{Key: "three", Partition: "plant", At: at.Add(2 * time.Second), Value: platform.Raw(3)}}}
	feed(third, at.Add(2*time.Second))
	frame, refusal := tn.ReadFlowFrame(memberOf(t, tn, "member"), id, at.Add(2*time.Second))
	if refusal != nil || frame.CheckpointCursor != "two" || frame.BatchesSinceCheckpoint != 1 || frame.CheckpointArtifact == nil || frame.Cursor != "three" {
		t.Fatalf("the current frame did not retain the last checkpoint reference: %+v %v", frame, refusal)
	}
	CheckReplay(t, tn, entries, compose)
}

// shop is a test app: orders reserved, paid, packed and shipped by a flow.
// An item named "broken" cannot ship, "none" cannot be reserved, and "stuck"
// cannot be released either.
type ShopOrder struct {
	platform.Record
	Item   string `json:"item" field:"required"`
	Status string `json:"status" field:"readonly" choices:"placed,reserved,paid,labelled,billed,shipped,released"`
	Owner  string `json:"owner" field:"readonly"`
}

type shop struct {
	ledger *platform.Ledger
	flows  []platform.Flow
}

func shopEntities() []platform.Entity {
	return []platform.Entity{{Type: "shop.order", Title: "Order", Model: ShopOrder{}}}
}

func newShop(tenant string, flows ...platform.Flow) *shop {
	var actions []platform.Action
	for _, schema := range []string{"place", "reserve", "release", "pay", "label", "bill", "ship"} {
		actions = append(actions, platform.Action{Schema: "shop.order." + schema, Target: "shop.order", Capability: "orders", Title: schema,
			Description: schema + " an order", Payload: []platform.Field{}, Roles: []string{"clerk"}})
	}
	return &shop{ledger: platform.NewLedger(tenant, "shop", platform.NewCatalog(actions...), "shop.order"), flows: flows}
}

func (s *shop) AcceptedLedger() *platform.Ledger { return s.ledger }

func (s *shop) Manifest() platform.Manifest {
	return platform.Manifest{ID: "shop", Version: "1", Actions: s.ledger.Catalog, Entities: shopEntities(), Flows: s.flows}
}
func (s *shop) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *shop) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *shop) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *shop) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (s *shop) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (s *shop) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return s.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id, schema := sub.GetTarget().GetId(), strings.TrimPrefix(sub.GetSchema().GetName(), "shop.order.")
		o, known := platform.Get[ShopOrder](c, id)
		refuse := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		switch {
		case schema == "place":
			if known {
				return nil, refuse
			}
			var p struct{ Item string }
			json.Unmarshal(sub.GetPayload(), &p)
			o = ShopOrder{Record: platform.Record{ID: id}, Item: p.Item, Status: "placed"}
		case !known:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		case schema == "reserve" && o.Item == "none", schema == "ship" && o.Item == "broken", schema == "release" && o.Item == "stuck":
			return nil, refuse
		default:
			o.Status = map[string]string{"reserve": "reserved", "release": "released", "pay": "paid", "label": "labelled", "bill": "billed", "ship": "shipped"}[schema]
		}
		return func(r *pb.ChangeRecord) { c.Put(r, o) }, nil
	})
}

func order(r *platform.Run) string { return r.Key }

func shopAct(action string) *platform.Act {
	return &platform.Act{Action: "shop.order." + action, Target: func(_ platform.Caller, r *platform.Run) string { return order(r) }}
}

var clerks = func(platform.Caller, *platform.Run) []platform.Recipient {
	return []platform.Recipient{{AppRole: "clerk"}}
}

// fulfil v1: reserve, wait for payment (an hour, then chase), ship.
func fulfil(version int) platform.Flow {
	f := platform.Flow{Name: "fulfil", Title: "Fulfil order", Version: version, Owners: []string{"clerk"},
		Start: platform.Start{On: []string{"shop.order.place"}, Begin: func(_ platform.Caller, e platform.Event) (string, any, bool) {
			return e.Record.GetSubmission().GetTarget().GetId(), map[string]string{"placed": "yes"}, true
		}},
		Steps: []platform.Step{
			{Name: "reserve", Act: shopAct("reserve"), Undo: shopAct("release"), Next: "paid"},
			{Name: "paid", Next: "ship", Timeout: time.Hour, OnTimeout: "chase", Wait: &platform.Wait{On: "shop.order.pay",
				Match: func(_ platform.Caller, r *platform.Run, e platform.Event) bool {
					return e.Record.GetSubmission().GetTarget().GetId() == order(r)
				}}},
			{Name: "chase", Ask: &platform.Ask{Title: func(_ platform.Caller, r *platform.Run) string { return "Chase payment of " + order(r) }, To: clerks,
				Answers: []string{"wait", "cancel"}},
				Choose: func(_ platform.Caller, r *platform.Run) (string, string) {
					if r.Answer == "cancel" {
						return platform.Compensate, "the clerk canceled"
					}
					return "paid", "the clerk waits"
				}},
			{Name: "ship", Act: shopAct("ship")},
		}}
	if version == 2 { // pack in parallel before shipping: a label, and the bill through a sub-flow
		f.Steps[1].Next = "pack"
		f.Steps = append(f.Steps, platform.Step{Name: "pack", All: []string{"label", "invoice"}, Next: "ship"},
			platform.Step{Name: "label", Act: shopAct("label")},
			platform.Step{Name: "invoice", Call: &platform.Call{Flow: "bill"}})
		f.From = map[string]string{"reserve": "reserve", "paid": "paid", "chase": "chase", "ship": "ship"}
	}
	return f
}

// bill is called by fulfil v2: it bills the order in the evening.
var bill = platform.Flow{Name: "bill", Title: "Bill", Version: 1,
	Start: platform.Start{On: []string{"shop.order.bill"}, Begin: func(platform.Caller, platform.Event) (string, any, bool) { return "", nil, false }},
	Steps: []platform.Step{
		{Name: "later", Wait: &platform.Wait{At: func(_ platform.Caller, r *platform.Run) (time.Time, *kernel.Error) {
			return time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC), nil
		}}, Next: "bill"},
		{Name: "bill", Act: &platform.Act{Action: "shop.order.bill", Target: func(c platform.Caller, r *platform.Run) string {
			id, _, _ := strings.Cut(r.Key, "/") // the caller's key, "<flow>:<order>/<n>"
			return strings.TrimPrefix(id, "shop.fulfil:")
		}}},
	}}
