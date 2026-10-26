package platformserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The probe writes through an ordinary application action and accepted ledger.
// Its retained Flow is the production window/Wasm/threshold/native-action path.
type nativeProbeRecord struct {
	platform.Record
	Blocked bool    `json:"blocked"`
	Reading float64 `json:"reading"`
	Applied int     `json:"applied"`
}

type nativeFrameSource struct{ *frameSource }

func newNativeFrameSource(tenant string) *nativeFrameSource {
	base := newFrameSource(tenant, 1<<20)
	base.ledger = platform.NewLedger(tenant, "frames", platform.NewCatalog(
		platform.Action{Schema: "frames.source.start", Target: "frames.source", Title: "Start", Description: "Start the retained native stream", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.probe.create", Target: "frames.probe", Title: "Create probe", Description: "Create one source-bound probe", New: true, Roles: []string{"operator"}, Payload: []platform.Field{{Name: "blocked", Type: "boolean"}}},
		platform.Action{Schema: "frames.probe.unblock", Target: "frames.probe", Title: "Unblock probe", Description: "Permit the original rejected reading", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.probe.accept", Target: "frames.probe", Title: "Accept reading", Description: "Accept one original stream reading", Roles: []string{"operator"}, Payload: []platform.Field{{Name: "reading", Type: "number", Required: true}}}), "frames.source", "frames.probe")
	return &nativeFrameSource{frameSource: base}
}

func (s *nativeFrameSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	m.Entities = append(m.Entities, platform.Entity{Type: "frames.probe", Title: "Probe", Model: nativeProbeRecord{}})
	fl := &m.Flows[0]
	fl.Continuous.Entry = "alertsloop"
	fl.Continuous.Concurrency = 8
	fl.Continuous.CheckpointEvery = 2
	fl.Continuous.DeadLetterMaxRecords = 16
	fl.Continuous.ErrorPolicy, fl.Continuous.DeadLetterRetry = "deadLetter", true
	fl.Continuous.Aggregate = &platform.StreamAggregate{Node: "aggregate", Kernel: "wasm:f32-window-stats", Signal: "reading", Group: []string{"probeId"}, Measures: []string{"count", "sum", "mean", "min", "max", "above"}, Above: 11.5}
	fl.Continuous.Threshold = &platform.StreamThreshold{Node: "threshold", Field: "mean", High: 11.5, Low: 9.5}
	fl.Continuous.Output = &platform.StreamOutput{Node: "output", Name: "stats", Mode: "replace"}
	fl.Steps = []platform.Step{
		{Name: "alertsloop", Next: "completed", Loop: &platform.Loop{Body: "accept", MaxIterations: 1000, Concurrency: 1, Items: func(_ platform.Caller, r *platform.Run) ([]json.RawMessage, *kernel.Error) {
			var input struct {
				Triggered []json.RawMessage `json:"triggered"`
			}
			if json.Unmarshal(r.Data, &input) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "invalid alert projection")
			}
			return input.Triggered, nil
		}}},
		{Name: "accept", Fault: "rejected", Retry: &platform.RetryPolicy{Attempts: 1}, Act: &platform.Act{Action: "frames.probe.accept", AsMember: true,
			TargetValue: func(_ platform.Caller, r *platform.Run) (string, *kernel.Error) {
				var item map[string]any
				json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &item)
				id, _ := item["probeId"].(string)
				return id, nil
			},
			Inputs: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				var item map[string]any
				json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &item)
				return platform.Raw(map[string]any{"reading": item["reading"]}), nil
			}}},
		{Name: "rejected", End: true}, {Name: "completed", End: true},
	}
	return m
}

func (s *nativeFrameSource) Submit(c platform.Caller, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if sub.GetSchema().GetName() == "frames.source.start" {
		return s.frameSource.Submit(c, sub, at)
	}
	return s.ledger.Receive(c, sub, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		x, known := platform.Get[nativeProbeRecord](c, sub.GetTarget().GetId())
		switch sub.GetSchema().GetName() {
		case "frames.probe.create":
			if known {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "probe exists")
			}
			var p struct{ Blocked bool }
			json.Unmarshal(sub.GetPayload(), &p)
			x = nativeProbeRecord{Record: platform.Record{ID: sub.GetTarget().GetId()}, Blocked: p.Blocked}
		case "frames.probe.unblock":
			if !known {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "no probe")
			}
			x.Blocked = false
		case "frames.probe.accept":
			if !known || x.Blocked {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "probe rejects reading")
			}
			var p struct{ Reading float64 }
			json.Unmarshal(sub.GetPayload(), &p)
			x.Reading = p.Reading
			x.Applied++
		default:
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "unknown probe action")
		}
		return func(r *pb.ChangeRecord) { c.Put(r, x) }, nil
	})
}

func TestContinuousNativeChainAssetsReplayResumeAndRetire(t *testing.T) {
	const tenant, id = "native-flow-closeout", "frames.window:source"
	files := &transientDeleteFiles{memoryFiles: &memoryFiles{}}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin"), seatOf("other", "flow:admin")}, work.New(tenant), flow.New(tenant), newNativeFrameSource(tenant))
		tn.Files = files
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	for i := range 8 {
		decide(t, tn, "member", "frames", "frames.probe.create", "frames.probe", fmt.Sprint(i), map[string]any{"blocked": i == 7}, at)
	}
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	batch := platform.FlowBatch{ID: "one"}
	for i := range 8 {
		batch.Signals = append(batch.Signals, platform.FlowSignal{Key: fmt.Sprint(i), Partition: fmt.Sprint(i), At: at.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"probeId": fmt.Sprint(i), "reading": 12.5000001})})
	}
	if _, err := tn.ConsumeFlowBatch(member, id, batch, at); err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		record, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), fmt.Sprint(i))
		want := 1
		if i == 7 {
			want = 0
		}
		if record.Applied != want || i != 7 && record.Reading != 12.5 {
			t.Fatalf("real action/f32 output: probe %d applied=%d reading=%v", i, record.Applied, record.Reading)
		}
	}
	outputs, _, err := platform.Find[platform.FlowOutput](tn.automation(flow.ID, false), platform.Query{})
	if err != nil || len(outputs) != 1 || outputs[0].App != "frames" || outputs[0].Instance != id || outputs[0].Version != 1 || outputs[0].Batch != "one" || outputs[0].Name != "stats" {
		t.Fatalf("no application/version-bound read-only output: %+v %v", outputs, err)
	}
	var stats []map[string]any
	if json.Unmarshal(outputs[0].Value, &stats) != nil || len(stats) != 8 || stats[0]["mean"] != 12.5 {
		t.Fatal("read-only output has no original kernel records")
	}
	letters, _, err := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if err != nil || len(letters) != 1 || letters[0].Version != 1 || letters[0].Action != "frames.probe.accept" || letters[0].Target != "7" || letters[0].Key == "" {
		t.Fatalf("rejected real action was not a versioned replayable asset: %+v %v", letters, err)
	}
	if _, err := tn.ReadFlowFrame(memberOf(t, tn, "other"), id, at); err == nil {
		t.Fatal("a different member read the original source projection")
	}
	if _, err := tn.ConsumeFlowBatch(member, id, batch, at); err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		record, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), fmt.Sprint(i))
		if record.Applied != 1 {
			t.Fatal("batch retry resubmitted an accepted business action")
		}
	}
	decide(t, tn, "member", "frames", "frames.probe.unblock", "frames.probe", "7", map[string]any{}, at)
	decide(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at)
	if record, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), "7"); record.Applied != 1 {
		t.Fatal("dead-letter replay did not submit the original rejected action")
	}
	if got := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at); got == "ok" {
		t.Fatal("a new replay command delivered the same dead letter twice")
	}
	// Intake between slide boundaries is accepted, but is not a new output
	// window. It must not replace the last completed statistics with [] or
	// claim that another eight-worker aggregate ran.
	completedOutput := platform.Raw(outputs[0])
	pending := platform.FlowBatch{ID: "pending", Predecessor: "one", Signals: []platform.FlowSignal{{Key: "pending", Partition: "0", At: at.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"probeId": "0", "reading": 12.5})}}}
	if _, err := tn.ConsumeFlowBatch(member, id, pending, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	latest, _ := platform.Get[platform.FlowOutput](tn.automation(flow.ID, false), outputs[0].ID)
	if string(platform.Raw(latest)) != string(completedOutput) {
		t.Fatalf("unready intake replaced the accepted output: %+v", latest)
	}
	waitingFrame, err := tn.ReadFlowFrame(member, id, at)
	if err != nil || waitingFrame.Cursor != "pending" || waitingFrame.Workers != 0 || waitingFrame.CheckpointCursor != "pending" || waitingFrame.CheckpointArtifact == nil {
		t.Fatalf("unready intake lost its checkpoint or reused old worker occupancy: %+v %v", waitingFrame, err)
	}
	tn.PullContinuousSources(at.Add(time.Second)) // retire the superseded frame through its owner
	late := platform.FlowBatch{ID: "two", Predecessor: "pending", Signals: []platform.FlowSignal{{Key: "late", Partition: "late", At: at.Add(-time.Hour), Value: platform.Raw(map[string]any{"probeId": "late", "reading": 13})}}}
	if _, err := tn.ConsumeFlowBatch(member, id, late, at.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	letters, _, err = platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if err != nil || len(letters) != 2 {
		t.Fatalf("late signal not retained as independent read-only asset: %d %v", len(letters), err)
	}
	frame, err := tn.ReadFlowFrame(member, id, at)
	if err != nil || frame.Cursor != "two" || frame.CheckpointArtifact == nil {
		t.Fatalf("periodic checkpoint not bound to accepted native frame: %+v %v", frame, err)
	}
	decide(t, tn, "member", flow.ID, flow.SchemaFlowStop, flow.InstanceType, id, map[string]any{}, at)
	if _, err := tn.ConsumeFlowBatch(member, id, platform.FlowBatch{ID: "stopped", Predecessor: "two"}, at); err == nil {
		t.Fatal("explicit stop kept accepting batches")
	}
	decide(t, tn, "member", flow.ID, flow.SchemaFlowResume, flow.InstanceType, id, map[string]any{}, at)
	resumed, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if resumed.Version != 1 || resumed.Batch.Cursor != "two" || resumed.State != "waiting" {
		t.Fatal("same-version resume lost the original checkpoint")
	}
	tn.PullContinuousSources(at)
	// Explicit signal replay reopens the original late value under the same
	// retained definition; it neither grants another member the source nor
	// redelivers accepted actions from the already-completed window.
	var signalLetter platform.FlowDeadLetterAsset
	for _, letter := range letters {
		if letter.Action == "" && letter.Letter.Key == "late" {
			signalLetter = letter
		}
	}
	if signalLetter.ID == "" {
		t.Fatal("the original late signal has no replayable asset")
	}
	if got := refuse(t, tn, "other", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", signalLetter.ID, map[string]any{}, at.Add(5*time.Second)); got == "ok" {
		t.Fatal("another Flow administrator replayed the member's protected signal")
	}
	decide(t, tn, "member", "frames", "frames.probe.create", "frames.probe", "late", map[string]any{}, at.Add(5*time.Second))
	decide(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", signalLetter.ID, map[string]any{}, at.Add(5*time.Second))
	if probe, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), "late"); probe.Applied != 1 || probe.Reading != 13 {
		t.Fatalf("explicit signal replay did not deliver the original native reading: %+v", probe)
	}
	for i := range 8 {
		if probe, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), fmt.Sprint(i)); probe.Applied != 1 {
			t.Fatalf("signal replay repeated an accepted action: %+v", probe)
		}
	}
	if got := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", signalLetter.ID, map[string]any{}, at.Add(5*time.Second)); got == "ok" {
		t.Fatal("a new replay command delivered the original signal twice")
	}
	frame, err = tn.ReadFlowFrame(member, id, at.Add(5*time.Second))
	if err != nil || frame.Cursor != "replay:"+signalLetter.ID {
		t.Fatalf("signal replay did not retain its exact logical batch: %+v %v", frame, err)
	}
	replayedLetter, _ := platform.Get[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), signalLetter.ID)
	if !replayedLetter.Replayed || replayedLetter.Version != 1 {
		t.Fatal("signal replay did not preserve its versioned accepted outcome")
	}
	tn.PullContinuousSources(at.Add(5 * time.Second))
	// Conversely, a completed slide with no retained rows really replaces the
	// application value. Keeping the previous result forever is not a fix.
	empty := platform.FlowBatch{ID: "empty-window", Predecessor: frame.Cursor}
	if _, err := tn.ConsumeFlowBatch(member, id, empty, at.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	latest, _ = platform.Get[platform.FlowOutput](tn.automation(flow.ID, false), outputs[0].ID)
	if latest.Batch != empty.ID || string(latest.Value) != "[]" {
		t.Fatalf("completed empty window did not replace the application output: %+v", latest)
	}
	tn.PullContinuousSources(at.Add(40 * time.Second))
	decide(t, tn, "member", flow.ID, flow.SchemaFlowStop, flow.InstanceType, id, map[string]any{}, at)
	decide(t, tn, "member", flow.ID, flow.SchemaFlowRetire, flow.InstanceType, id, map[string]any{}, at)
	files.failures = 1
	tn.PullContinuousSources(at)
	retired, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if !retired.CleanupPending || retired.Resumable || retired.Batch != nil {
		t.Fatal("failed terminal delete discarded durable cleanup intent or allowed resume")
	}
	tn.PullContinuousSources(at.Add(time.Second))
	retired, _ = platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if retired.CleanupPending {
		t.Fatal("terminal delete retry was not acknowledged by the original owner")
	}
	if got := refuse(t, tn, "member", flow.ID, flow.SchemaFlowResume, flow.InstanceType, id, map[string]any{}, at); got == "ok" {
		t.Fatal("explicitly retired state was silently recreated")
	}
	CheckReplay(t, tn, entries, compose)

	t.Run("original hundred signals fit unchanged output budgets", func(t *testing.T) {
		full := compose()
		var history []Entry
		full.Record = func(e Entry) { history = append(history, e) }
		full.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { history = append(history, e); return e.Body, nil }
		batch := platform.FlowBatch{ID: "hundred"}
		for i := range 100 {
			probe := fmt.Sprint(i)
			decide(t, full, "member", "frames", "frames.probe.create", "frames.probe", probe, map[string]any{}, at)
			batch.Signals = append(batch.Signals, platform.FlowSignal{Key: probe, Partition: probe, At: at.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"probeId": probe, "reading": 12.5000001})})
		}
		decide(t, full, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
		if _, err := full.ConsumeFlowBatch(memberOf(t, full, "member"), id, batch, at); err != nil {
			t.Fatalf("original workload refused: %s: %s", err.Code, err.Message)
		}
		for i := range 100 {
			probe, _ := platform.Get[nativeProbeRecord](full.automation("frames", false), fmt.Sprint(i))
			if probe.Applied != 1 || probe.Reading != 12.5 {
				t.Fatalf("original workload lost a native action: %+v", probe)
			}
		}
		frame, err := full.ReadFlowFrame(memberOf(t, full, "member"), id, at)
		// Short native folds need not overlap all eight slots. The worker
		// owner's barrier test proves capacity; this route checks the actual
		// occupancy stays within the declaration and preserves every signal.
		if err != nil || frame.Consumed != 100 || frame.Workers < 1 || frame.Workers > 8 {
			t.Fatalf("original workload lost its frame/parallel fold: %+v %v", frame, err)
		}
		output, _ := platform.Get[platform.FlowOutput](full.automation(flow.ID, false), outputs[0].ID)
		var stats []map[string]any
		if json.Unmarshal(output.Value, &stats) != nil || len(stats) != 100 || len(output.Value) > 48<<10 {
			t.Fatalf("original read-only output escaped its budget or lost rows: %d", len(output.Value))
		}
		instance, _ := platform.Get[flow.FlowInstance](full.automation(flow.ID, false), id)
		if len(platform.Raw(instance.Outputs)) > 60<<10 {
			t.Fatal("original Flow output budget was widened")
		}
		var reference platform.FlowBatchStateReference
		original := frame.OperatorOutputs["aggregate"]
		if json.Unmarshal(instance.Outputs["aggregate"], &reference) != nil || !reference.Output || reference.Instance != id || reference.Node != "aggregate" || reference.Size != len(original) || reference.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(original)) {
			t.Fatalf("wide output does not identify the exact authorized frame bytes: %+v", reference)
		}
		if _, err := full.ReadFlowFrame(memberOf(t, full, "other"), id, at); err == nil {
			t.Fatal("wide output reference granted another member frame access")
		}
		if _, err := full.ConsumeFlowBatch(memberOf(t, full, "member"), id, batch, at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		for i := range 100 {
			probe, _ := platform.Get[nativeProbeRecord](full.automation("frames", false), fmt.Sprint(i))
			if probe.Applied != 1 {
				t.Fatal("wide-output batch retry repeated an accepted native action")
			}
		}
		for _, entry := range history {
			if len(entry.Body) > 1<<20 {
				t.Fatal("original accepted-result budget was widened")
			}
		}
		CheckReplay(t, full, history, compose)
	})
}
