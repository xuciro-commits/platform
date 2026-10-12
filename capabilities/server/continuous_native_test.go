package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	late := platform.FlowBatch{ID: "two", Predecessor: "one", Signals: []platform.FlowSignal{{Key: "late", Partition: "late", At: at.Add(-time.Hour), Value: platform.Raw(map[string]any{"probeId": "late", "reading": 13})}}}
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
}

// TestJournalContinuousNativeChainRecoversAcceptedState runs the native
// window/aggregate/threshold/action chain on the real PostgreSQL journal: a
// restart reads back the accepted cursor, window state, read-only output and
// versioned dead letter, a result whose response was lost after its commit
// advances the accepted position exactly once, and no business action runs
// twice (ADR-0047 §13.5; the same route in memory is
// TestContinuousNativeChainAssetsReplayResumeAndRetire).
func TestJournalContinuousNativeChainRecoversAcceptedState(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	tenant, id := fmt.Sprintf("native-flow-journal-%d", time.Now().UnixNano()), "frames.window:source"
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, tenant)
	files := &transientDeleteFiles{memoryFiles: &memoryFiles{}}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), newNativeFrameSource(tenant))
		tn.Files = files
		return tn
	}
	// What a deployed host wires (deploy_recovery.go): ordinary inputs and
	// accepted results are durable before anything is answered.
	attach := func(tn *Tenant) {
		tn.Record = func(e Entry) {
			if err := journal.Append(ctx, tenant, e); err != nil {
				t.Fatalf("journal append: %v", err)
			}
		}
		tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
			return journal.AppendAccepted(ctx, tenant, e, key, hash)
		}
	}
	live := compose()
	// Reading the tenant's entries seeds the journal's next position for it.
	if _, err := journal.Entries(ctx, tenant, 0); err != nil {
		t.Fatal(err)
	}
	attach(live)
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for i := range 9 {
		decide(t, live, "member", "frames", "frames.probe.create", "frames.probe", fmt.Sprint(i), map[string]any{"blocked": i == 7}, at)
	}
	decide(t, live, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, live, "member")
	batch := platform.FlowBatch{ID: "one"}
	for i := range 8 {
		batch.Signals = append(batch.Signals, platform.FlowSignal{Key: fmt.Sprint(i), Partition: fmt.Sprint(i), At: at.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"probeId": fmt.Sprint(i), "reading": 12.5000001})})
	}
	if _, err := live.ConsumeFlowBatch(member, id, batch, at); err != nil {
		t.Fatal(err)
	}
	if journal.Position(tenant) == 0 {
		t.Fatal("the native chain committed no journal entries")
	}

	// Restart: the whole tenant comes back from the journal alone.
	rebuild := func() *Tenant {
		entries, err := journal.Entries(ctx, tenant, 0)
		if err != nil {
			t.Fatal(err)
		}
		again := compose()
		if err := again.Replay(entries); err != nil {
			t.Fatalf("replay: %v", err)
		}
		return again
	}
	restarted := rebuild()
	frame, refusal := restarted.ReadFlowFrame(memberOf(t, restarted, "member"), id, at)
	if refusal != nil || frame.Cursor != "one" || frame.Consumed != 8 {
		t.Fatalf("the accepted native frame did not recover: cursor=%q consumed=%d refusal=%v", frame.Cursor, frame.Consumed, refusal)
	}
	outputs, _, findErr := platform.Find[platform.FlowOutput](restarted.automation(flow.ID, false), platform.Query{})
	if findErr != nil {
		t.Fatalf("reading recovered outputs: %v (%T)", findErr, findErr)
	}
	if len(outputs) != 1 {
		t.Fatalf("recovered %d read-only outputs, want 1", len(outputs))
	}
	if outputs[0].App != "frames" || outputs[0].Instance != id || outputs[0].Version != 1 || outputs[0].Batch != "one" || outputs[0].Name != "stats" {
		t.Fatalf("the recovered read-only output is not the accepted one: %+v", outputs[0])
	}
	var stats []map[string]any
	if json.Unmarshal(outputs[0].Value, &stats) != nil || len(stats) != 8 || stats[0]["mean"] != 12.5 {
		t.Fatal("the recovered output lost its original kernel records")
	}
	letters, _, lettersErr := platform.Find[platform.FlowDeadLetterAsset](restarted.automation(flow.ID, false), platform.Query{})
	if lettersErr != nil || len(letters) != 1 {
		t.Fatalf("recovered %d dead letters (err=%v)", len(letters), lettersErr)
	}
	if letters[0].Target != "7" || letters[0].Action != "frames.probe.accept" || letters[0].Letter.Batch != "one" || letters[0].Version != 1 {
		t.Fatalf("the recovered dead letter is not the rejected action: target=%q action=%q batch=%q version=%d", letters[0].Target, letters[0].Action, letters[0].Letter.Batch, letters[0].Version)
	}
	for i := range 8 {
		probe, _ := platform.Get[nativeProbeRecord](restarted.automation("frames", false), fmt.Sprint(i))
		if want := 1; i == 7 && probe.Applied != 0 || i != 7 && (probe.Applied != want || probe.Reading != 12.5) {
			t.Fatalf("replay re-ran or lost the action of probe %d: %+v", i, probe)
		}
	}

	// The recovered process writes ordinary accepted results again. The
	// versioned dead letter replays on the same durable position first, so its
	// accepted action is already durable when the next process takes over.
	restarted.Record = func(e Entry) {
		if err := journal.Append(ctx, tenant, e); err != nil {
			t.Fatalf("journal append: %v", err)
		}
	}
	restarted.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, tenant, e, key, hash)
	}
	decide(t, restarted, "member", "frames", "frames.probe.unblock", "frames.probe", "7", map[string]any{}, at.Add(2*time.Second))
	decide(t, restarted, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at.Add(2*time.Second))
	if probe, _ := platform.Get[nativeProbeRecord](restarted.automation("frames", false), "7"); probe.Applied != 1 || probe.Reading != 12.5 {
		t.Fatalf("dead-letter replay did not submit the original rejected action: %+v", probe)
	}
	if got := refuse(t, restarted, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at.Add(2*time.Second)); got == "ok" {
		t.Fatal("a new replay command delivered the same dead letter twice")
	}

	// The next batch commits its accepted position and the host then stops
	// before applying it. deploy_recovery.go stops the process outright, so
	// nothing unwinds and the committed bytes stay exactly as written.
	crashed := make(chan struct{})
	restarted.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		raw, err := journal.AppendAccepted(ctx, tenant, e, key, hash)
		if err != nil || e.App != flow.ID || !bytes.Contains(e.Body, []byte(`"cursor":"two"`)) {
			return raw, err
		}
		close(crashed)
		select {} // the dead process never returns, so no deferred cleanup runs
	}
	// The new probe crosses the high mark for the first time, and the watermark
	// slide advanced past the first batch's emission.
	second := platform.FlowBatch{ID: "two", Predecessor: "one", Signals: []platform.FlowSignal{{Key: "eight", Partition: "eight", At: at.Add(-4 * time.Second), Value: platform.Raw(map[string]any{"probeId": "8", "reading": 13.75})}}}
	crashMember := memberOf(t, restarted, "member")
	go func() {
		_, _ = restarted.ConsumeFlowBatch(crashMember, id, second, at.Add(7*time.Second))
	}()
	select {
	case <-crashed:
	case <-time.After(30 * time.Second):
		t.Fatal("the second batch never accepted its position")
	}
	continued := rebuild()
	continued.Record = func(e Entry) {
		if err := journal.Append(ctx, tenant, e); err != nil {
			t.Fatalf("journal append: %v", err)
		}
	}
	continued.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, tenant, e, key, hash)
	}
	frame, refusal = continued.ReadFlowFrame(memberOf(t, continued, "member"), id, at)
	if refusal != nil || frame.Cursor != "two" || frame.Predecessor != "one" || frame.Consumed != 9 || frame.CheckpointArtifact == nil {
		t.Fatalf("the lost response did not leave exactly one accepted position with its checkpoint: frame=%+v refusal=%v", frame, refusal.Message)
	}
	probe, _ := platform.Get[nativeProbeRecord](continued.automation("frames", false), "8")
	if probe.Applied != 1 || probe.Reading != 13.75 {
		t.Fatalf("the committed result was not applied exactly once after the restart: %+v", probe)
	}
	if probe, _ := platform.Get[nativeProbeRecord](continued.automation("frames", false), "7"); probe.Applied != 1 || probe.Reading != 12.5 {
		t.Fatalf("the recovered process delivered the replayed dead letter again: %+v", probe)
	}
	entries, err := journal.Entries(ctx, tenant, 0)
	if err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, continued, entries, compose)
}
