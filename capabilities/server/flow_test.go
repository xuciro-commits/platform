package platformserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

// The source fixture enters through one ordinary accepted decision. Flow
// remains the only instance owner; the source keeps no second runtime state.
type frameSource struct {
	ledger     *platform.Ledger
	host       host.Host
	stateBytes int
	configure  []func(*platform.Continuous)
}

type frameStream struct{ platform.Record }

// declared builds the test source's continuous declaration; each configure
// step is the test's own declared rule, as a real app's would be.
func declared(s *frameSource) *platform.Continuous {
	c := &platform.Continuous{
		Source: "frames.source", Batch: 512, State: s.stateBytes, FrameBytes: 2 * s.stateBytes, DeadLetter: &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 10000, TTLMS: 86400000},
		Window: &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"},
	}
	for _, configure := range s.configure {
		configure(c)
	}
	return c
}

func newFrameSource(tenant string, stateBytes int, configure ...func(*platform.Continuous)) *frameSource {
	return &frameSource{stateBytes: stateBytes, configure: configure, ledger: platform.NewLedger(tenant, "frames", platform.NewCatalog(
		platform.Action{Schema: "frames.source.start", Target: "frames.source", Title: "Start", Description: "Start the source's original Flow instance", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.source.feed", Target: "frames.source", Title: "Feed", Description: "Accept one source batch through its original Flow instance", Roles: []string{"operator"}, Payload: []platform.Field{{Name: "batch", Type: "json", Required: true}}}), "frames.source")}
}
func (s *frameSource) Attach(h host.Host) { s.host = h }
func (s *frameSource) Manifest() platform.Manifest {
	return platform.Manifest{
		ID: "frames", Version: "1", Actions: s.ledger.Catalog,
		Entities: []platform.Entity{{Type: "frames.source", Title: "Source", Model: frameStream{}}},
		Flows: []platform.Flow{{
			Name: "window", Title: "Accepted window", Version: 1, Start: platform.Start{Manual: true},
			Continuous: declared(s),
			Steps:      []platform.Step{{Name: "intake", Wait: &platform.Wait{Until: func(platform.Caller, *platform.Run) bool { return false }}}},
		}},
	}
}
func (s *frameSource) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *frameSource) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *frameSource) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *frameSource) AcceptedLedger() *platform.Ledger         { return s.ledger }
func (s *frameSource) AcceptedActionSchemas() []string {
	return []string{"frames.source.start", "frames.source.feed"}
}
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
				Batch flow.Batch `json:"batch"`
			}
			if json.Unmarshal(sub.GetPayload(), &input) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "invalid batch")
			}
			consume := s.host.Processes().(interface {
				ConsumeBatch(platform.Caller, string, flow.Batch, time.Time) (flow.BatchOutcome, *kernel.Error)
			})
			_, err = consume.ConsumeBatch(s.host.Automation(c, flow.ID), "frames.window:source", input.Batch, now)
		}
		if err != nil {
			return nil, err
		}
		return func(*pb.ChangeRecord) {}, nil
	})
}

func TestContinuousAcceptedFrameRefusalAndRecovery(t *testing.T) {
	const tenant = "accepted-frames"
	compose := func() *Tenant {
		return composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 4096))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	first := flow.Batch{ID: "one", Signals: []flow.Signal{{Key: "A", Partition: "plant", At: at, Value: platform.Raw(4)}}}
	decide(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": first}, at)
	read := func() flow.FlowInstance {
		x, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "frames.window:source")
		if !ok {
			t.Fatal("no native instance")
		}
		return x
	}
	before := read()
	tooLarge := flow.Batch{ID: "two", Predecessor: "one", Signals: []flow.Signal{{Key: "B", Partition: "plant", At: at.Add(time.Second), Value: platform.Raw(strings.Repeat("x", 6000))}}}
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
	changed.Signals = []flow.Signal{{Key: "A", Partition: "plant", At: at, Value: platform.Raw(40)}}
	if refuse(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": changed}, at.Add(time.Second)) == "ok" {
		t.Fatal("reused batch identity accepted different bytes")
	}
	second := flow.Batch{ID: "two", Predecessor: "one", Signals: []flow.Signal{{Key: "B", Partition: "plant", At: at.Add(5 * time.Second), Value: platform.Raw(5)}}}
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
	copy, ok := platform.Get[flow.FlowInstance](restored.automation(flow.ID, false), "frames.window:source")
	if !ok || string(platform.Raw(copy.Batch)) != string(platform.Raw(read().Batch)) {
		t.Fatal("snapshot changed the accepted window")
	}

	t.Run("large window keeps bytes in the original frame", func(t *testing.T) {
		const tenant = "large-accepted-frames"
		compose := func() *Tenant {
			return composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 128<<10))
		}
		live := compose()
		var entries []Entry
		live.Record = func(e Entry) { entries = append(entries, e) }
		live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
		decide(t, live, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
		cursor := ""
		for n := 1; n <= 9; n++ {
			batch := flow.Batch{ID: fmt.Sprintf("%d", n), Predecessor: cursor, Signals: []flow.Signal{{Key: fmt.Sprintf("%d", n), Partition: "plant", At: at, Value: platform.Raw(strings.Repeat("x", 6000))}}}
			decide(t, live, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": batch}, at)
			cursor = batch.ID
		}
		x, ok := platform.Get[flow.FlowInstance](live.automation(flow.ID, false), "frames.window:source")
		if !ok || len(x.Batch.State["window"]) <= 48<<10 {
			t.Fatal("fixture did not retain a window above the inline budget")
		}
		var ref flow.BatchStateReference
		if err := json.Unmarshal(x.Outputs["state:window"], &ref); err != nil || ref.Instance != x.ID || ref.Node != "window" || ref.Size != len(x.Batch.State["window"]) || ref.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(x.Batch.State["window"])) {
			t.Fatalf("output does not identify its original accepted bytes: %+v %v", ref, err)
		}
		if len(platform.Raw(x.Outputs)) > 60<<10 {
			t.Fatal("large state bypassed the existing output budget")
		}
		// Compact token outputs do not bypass the host's accepted-result
		// budget. Large retained histories still need sealed state artifacts.
		before := platform.Raw(x)
		batch := flow.Batch{ID: "ten", Predecessor: cursor, Signals: []flow.Signal{{Key: "ten", Partition: "plant", At: at, Value: platform.Raw(strings.Repeat("x", 6000))}}}
		if why := refuse(t, live, "member", "frames", "frames.source.feed", "frames.source", "source", map[string]any{"batch": batch}, at); !strings.Contains(why, "bounded accepted result") {
			t.Fatalf("retained history bypassed the original accepted-result bound: %s", why)
		}
		after, ok := platform.Get[flow.FlowInstance](live.automation(flow.ID, false), x.ID)
		if !ok || string(before) != string(platform.Raw(after)) {
			t.Fatal("host budget refusal advanced the large accepted window")
		}
		CheckReplay(t, live, entries, compose)
	})
}

func TestContinuousSealedFrameAndRecovery(t *testing.T) {
	const tenant, id = "sealed-frames", "frames.window:source"
	store := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin"), seatOf("other", "flow:admin")}, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 2<<20))
		tn.Files = store
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	var first flow.Batch
	cursor := ""
	for n := 1; n <= 16; n++ {
		batch := flow.Batch{ID: fmt.Sprint(n), Predecessor: cursor, Signals: []flow.Signal{{Key: fmt.Sprint(n), Partition: "plant", At: at, Value: platform.Raw(strings.Repeat("x", 6000))}}}
		if n == 1 {
			first = batch
		}
		if _, err := tn.ConsumeFlowBatch(member, id, batch, at); err != nil {
			t.Fatalf("native batch %d: %v", n, err)
		}
		cursor = batch.ID
	}
	x, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if !ok || x.Batch.Sealed == nil || len(platform.Raw(x.Batch)) > 2048 || x.Batch.State != nil || x.Batch.DeadLetters != nil {
		t.Fatal("the accepted record still carries large window bytes")
	}
	frame, err := tn.ReadFlowFrame(member, id, at)
	if err != nil || frame.Consumed != 16 || len(frame.State["window"]) < 64<<10 {
		t.Fatalf("large original state could not be resolved: %v", err)
	}
	before := platform.Raw(x)
	if answer, err := tn.ConsumeFlowBatch(member, id, first, at); err != nil || answer.Cursor != first.ID {
		t.Fatalf("historical batch retry was not recognised: %+v %v", answer, err)
	}
	changed := first
	changed.Signals = []flow.Signal{{Key: "1", Partition: "plant", At: at, Value: platform.Raw("changed")}}
	if _, err := tn.ConsumeFlowBatch(member, id, changed, at); err == nil {
		t.Fatal("historical batch identity accepted different content")
	}
	if _, err := tn.ReadFlowFrame(memberOf(t, tn, "other"), id, at); err == nil {
		t.Fatal("another member read the source's private frame")
	}
	after, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if string(before) != string(platform.Raw(after)) {
		t.Fatal("retries or refusal changed the accepted frame")
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, snapshotErr := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.ReadFlowFrame(memberOf(t, restored, "member"), id, at)
	if err != nil || string(platform.Raw(frame)) != string(platform.Raw(recovered)) {
		t.Fatalf("restored artifact changed: %v", err)
	}
	restored.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	if _, err := restored.ConsumeFlowBatch(memberOf(t, restored, "member"), id, flow.Batch{ID: "17", Predecessor: "16", Signals: []flow.Signal{{Key: "17", Partition: "plant", At: at, Value: platform.Raw(17)}}}, at); err != nil {
		t.Fatalf("restored instance could not consume its successor: %v", err)
	}
	files := flowFrameStore{tenant: tenant, files: store}
	ref := *x.Batch.Sealed
	foreign := ref
	foreign.Tenant = "other-tenant"
	if _, err := files.Read(foreign); err == nil {
		t.Fatal("a foreign tenant's handle was readable")
	}
	key, keyErr := files.key(ref)
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	store.mu.Lock()
	store.m[key][0] = '!'
	store.mu.Unlock()
	if _, err := files.Read(ref); err == nil {
		t.Fatal("corrupt bytes matched an accepted artifact")
	}
}

// The sealed lane prepares the host's batches outside the tenant lock; the
// declared window→aggregate→threshold operators must advance in that same
// decision, not only in the direct in-process consumer (ADR-0047 §13.1).
func TestContinuousSealedOperatorsAndRecovery(t *testing.T) {
	const tenant, id = "sealed-operators", "frames.window:source"
	store := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant),
			newFrameSource(tenant, 1<<20, func(c *platform.Continuous) {
				c.Window = &platform.StreamWindow{Node: "window", WindowMS: 5000, SlideMS: 1000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"}
				c.Aggregate = &platform.StreamAggregate{Node: "stats", Signal: "vibration", Group: []string{"deviceId"}, Measures: []string{"count", "mean", "max"}}
				c.Threshold = &platform.StreamThreshold{Node: "high", Field: "mean", High: 11.5, Low: 9.5, Severity: "High"}
				c.CheckpointEvery = 2
			}))
		tn.Files = store
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	measurement := func(device string, vibration float64) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"deviceId":%q,"vibration":%g}`, device, vibration))
	}
	cursor := ""
	feed := func(tn *Tenant, name string, when time.Time, signals ...flow.Signal) flow.BatchOutcome {
		t.Helper()
		out, err := tn.ConsumeFlowBatch(memberOf(t, tn, "member"), id, flow.Batch{ID: name, Predecessor: cursor, Signals: signals}, when)
		if err != nil {
			t.Fatalf("sealed batch %s: %v", name, err)
		}
		cursor = out.Cursor
		return out
	}
	alertsOf := func(tn *Tenant) []flow.Alert {
		t.Helper()
		x, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
		if !ok {
			t.Fatal("no native instance")
		}
		var alerts []flow.Alert
		if raw := x.Outputs["alerts"]; len(raw) > 0 && json.Unmarshal(raw, &alerts) != nil {
			t.Fatalf("alerts output is not JSON: %s", raw)
		}
		return alerts
	}
	// One slide over the high mark raises and alarms; the sealed frame, not
	// the accepted record, owns the window, aggregate and threshold state.
	if out := feed(tn, "b1", at,
		flow.Signal{Key: "m1", Partition: "plant-a", At: at, Value: measurement("d1", 12)},
		flow.Signal{Key: "m2", Partition: "plant-a", At: at.Add(time.Second), Value: measurement("d1", 13)},
		flow.Signal{Key: "m3", Partition: "plant-a", At: at.Add(2 * time.Second), Value: measurement("d1", 12)}); out.Checkpoint != 0 {
		t.Fatalf("a checkpoint appeared off period: %d", out.Checkpoint)
	}
	if alerts := alertsOf(tn); len(alerts) != 1 || alerts[0].State != "triggered" || alerts[0].Group != "[d1]" || alerts[0].Field != "mean" {
		t.Fatalf("sealed lane triggered alerts: %+v", alerts)
	}
	x, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
	if !ok || x.Batch.Sealed == nil || x.Batch.State != nil {
		t.Fatal("the sealed lane kept operator state in the accepted record")
	}
	frame, err := tn.ReadFlowFrame(memberOf(t, tn, "member"), id, at)
	if err != nil || len(frame.State["stats"]) == 0 || len(frame.State["high"]) == 0 || len(frame.State["window"]) == 0 {
		t.Fatalf("the seal lost operator state: %s", err.Message)
	}
	if frame.Checkpoint != nil {
		t.Fatalf("a checkpoint appeared off period: %+v", frame.Checkpoint)
	}
	// Inside the band the episode stays raised and says nothing; below the low
	// mark it clears exactly once.
	if out := feed(tn, "b2", at.Add(5*time.Second), flow.Signal{Key: "m4", Partition: "plant-a", At: at.Add(6 * time.Second), Value: measurement("d1", 10)}); out.Checkpoint != 2 {
		t.Fatalf("the sealed lane wrote no run checkpoint at its period: %d", out.Checkpoint)
	}
	// The checkpoint travels inside the sealed frame and names it after the
	// fold: the read verifies it against the bytes it actually got.
	sealed, failure := tn.ReadFlowFrame(memberOf(t, tn, "member"), id, at.Add(5*time.Second))
	if failure != nil || sealed.Checkpoint == nil || sealed.Checkpoint.Ordinal != 2 || sealed.Checkpoint.Digest == "" || flow.VerifyFrame(*sealed) != nil {
		t.Fatalf("sealed checkpoint: %+v %v", sealed.Checkpoint, failure)
	}
	if alerts := alertsOf(tn); len(alerts) != 0 {
		t.Fatalf("hysteresis band alarmed: %+v", alerts)
	}
	feed(tn, "b3", at.Add(10*time.Second), flow.Signal{Key: "m5", Partition: "plant-a", At: at.Add(11 * time.Second), Value: measurement("d1", 8)})
	if alerts := alertsOf(tn); len(alerts) != 1 || alerts[0].State != "cleared" {
		t.Fatalf("cleared alert: %+v", alerts)
	}
	// Recovery carries the operators' committed state with the instance: the
	// restored flow raises a fresh episode from the state the artifact holds,
	// so a lost or stale episode state would alarm differently.
	raw, _, snapshotErr := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	restored.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	feed(restored, "b4", at.Add(16*time.Second),
		flow.Signal{Key: "m6", Partition: "plant-a", At: at.Add(16 * time.Second), Value: measurement("d1", 12)},
		flow.Signal{Key: "m7", Partition: "plant-a", At: at.Add(17 * time.Second), Value: measurement("d1", 13)})
	if alerts := alertsOf(restored); len(alerts) != 1 || alerts[0].State != "triggered" {
		t.Fatalf("restored operators raised %+v", alerts)
	}
}

// Dead letters are the instance's own numbered asset: an administrator delivers
// named letters' signals again through the instance's action, under the
// original authorization, and a signal is re-adjudicated rather than applied
// twice (ADR-0047 §13.3).
func TestContinuousDeadLetterReplay(t *testing.T) {
	const tenant, id = "dead-letter-replay", "frames.window:source"
	store := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin"), seatOf("clerk", "frames:operator")},
			work.New(tenant), flow.New(tenant), newFrameSource(tenant, 1<<20, func(c *platform.Continuous) {
				c.Window = &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"}
				c.DeadLetter = &platform.StreamDeadLetter{Node: "dlq", MaxRecords: 8}
			}))
		tn.Files = store
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	feed := func(when time.Time, name, predecessor string, signals ...flow.Signal) {
		t.Helper()
		decide(t, tn, "member", "frames", "frames.source.feed", "frames.source", "source",
			map[string]any{"batch": flow.Batch{ID: name, Predecessor: predecessor, Signals: signals}}, when)
	}
	read := func(tn *Tenant) flow.FlowInstance {
		t.Helper()
		x, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
		if !ok {
			t.Fatal("no native instance")
		}
		return x
	}
	window := func(tn *Tenant) flow.WindowState {
		t.Helper()
		frame, err := tn.ReadFlowFrame(memberOf(t, tn, "member"), id, at)
		if err != nil {
			t.Fatalf("read frame: %s", err.Message)
		}
		var state flow.WindowState
		if raw := frame.State["window"]; len(raw) > 0 && json.Unmarshal(raw, &state) != nil {
			t.Fatalf("window state is not JSON: %s", raw)
		}
		return state
	}
	feed(at, "one", "", flow.Signal{Key: "m1", Partition: "plant-a", At: at, Value: platform.Raw(4)},
		flow.Signal{Key: "ghost", Partition: "plant-a", Value: platform.Raw(9)})
	feed(at.Add(time.Second), "two", "one", flow.Signal{Key: "m2", Partition: "plant-a", At: at.Add(3 * time.Minute), Value: platform.Raw(5)})
	feed(at.Add(2*time.Second), "three", "two", flow.Signal{Key: "m3", Partition: "plant-a", At: at.Add(10 * time.Second), Value: platform.Raw(6)})
	x := read(tn)
	if len(x.Batch.DeadLetters) != 2 || x.Batch.DeadLetters[0].Seq != 1 || x.Batch.DeadLetters[0].Key != "ghost" || x.Batch.DeadLetters[1].Seq != 2 || x.Batch.DeadLetters[1].Key != "m3" {
		t.Fatalf("dead letters: %+v", x.Batch.DeadLetters)
	}
	if x.Batch.Consumed != 2 || x.Batch.Rejected != 2 || len(window(tn).Rows) != 1 {
		t.Fatalf("frame counters: consumed %d rejected %d rows %d", x.Batch.Consumed, x.Batch.Rejected, len(window(tn).Rows))
	}
	// Only an administrator delivers letters again, and only numbers the
	// instance still retains; a refusal changes nothing.
	before := platform.Raw(x)
	if refusal := refuse(t, tn, "clerk", flow.ID, flow.SchemaFlowReplay, flow.InstanceType, id, map[string]any{"seqs": []int64{2}}, at); !strings.Contains(refusal, "POLICY_DENIED") {
		t.Fatalf("a clerk replayed dead letters: %s", refusal)
	}
	if refusal := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, flow.InstanceType, id, map[string]any{"seqs": []int64{99}}, at); !strings.Contains(refusal, "ERROR_CODE_INVALID_ARGUMENT") {
		t.Fatalf("an unknown letter was replayed: %s", refusal)
	}
	if refusal := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, flow.InstanceType, id, map[string]any{"seqs": []int64{}}, at); !strings.Contains(refusal, "ERROR_CODE_INVALID_ARGUMENT") {
		t.Fatalf("an empty replay was accepted: %s", refusal)
	}
	if string(before) != string(platform.Raw(read(tn))) {
		t.Fatal("a refused replay changed the accepted frame")
	}
	// Delivering the late letter again re-adjudicates it under the same rule:
	// the signal is late once more, so it becomes a fresh letter — the window
	// gains no row and the original letter is marked as delivered.
	decide(t, tn, "member", flow.ID, flow.SchemaFlowReplay, flow.InstanceType, id, map[string]any{"seqs": []int64{2}}, at.Add(3*time.Second))
	x = read(tn)
	if len(x.Batch.DeadLetters) != 3 || x.Batch.DeadLetters[2].Seq != 3 || x.Batch.DeadLetters[2].Key != "m3" || x.Batch.DeadLetters[2].Reason != x.Batch.DeadLetters[1].Reason || !x.Batch.DeadLetters[1].Replayed || x.Batch.DeadLetters[0].Replayed {
		t.Fatalf("replayed letters: %+v", x.Batch.DeadLetters)
	}
	if x.Batch.Consumed != 2 || x.Batch.Rejected != 3 || x.Batch.LetterSeq != 3 {
		t.Fatalf("replay counters: consumed %d rejected %d issued %d", x.Batch.Consumed, x.Batch.Rejected, x.Batch.LetterSeq)
	}
	if state := window(tn); len(state.Rows) != 1 || state.Rows[0].Key != "m2" || state.Duplicate != 0 {
		t.Fatalf("the replay changed the retained window: %+v", state)
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
		{Name: "later", Wait: &platform.Wait{At: func(_ platform.Caller, r *platform.Run) time.Time {
			return time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
		}}, Next: "bill"},
		{Name: "bill", Act: &platform.Act{Action: "shop.order.bill", Target: func(c platform.Caller, r *platform.Run) string {
			id, _, _ := strings.Cut(r.Key, "/") // the caller's key, "<flow>:<order>/<n>"
			return strings.TrimPrefix(id, "shop.fulfil:")
		}}},
	}}

func TestFlows(t *testing.T) {
	var journal []Entry
	build := func(flows ...platform.Flow) *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{"shop": role, flow.ID: flow.Admin}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", "clerk"), seat("bo", "clerk")), work.New("t-1"), flow.New("t-1"), newShop("t-1", flows...))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build(fulfil(1))
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	place := func(id, item string) {
		do("ana", "shop", "shop.order.place", "shop.order", id, map[string]string{"item": item})
	}
	tick := func(d time.Duration) { // time passes; the host works every second of it that matters
		for end := now.Add(d); !now.After(end); now = now.Add(time.Second) {
			tn.Work(now)
		}
	}
	instance := func(id string) flow.FlowInstance {
		x, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), id)
		return x
	}
	status := func(id string) string {
		o, _ := platform.Get[ShopOrder](tn.automation("shop", false), id)
		return o.Status
	}
	trace := func(id string) string {
		var out []string
		for _, l := range instance(id).Trace {
			out = append(out, strings.TrimSpace(l.Step+" "+l.What))
		}
		return strings.Join(out, ", ")
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	tasks := func(who string) []work.WorkTask {
		out, _ := tn.Read(member(who), "inbox")
		return out.([]work.WorkTask)
	}

	// An order placed starts the flow: reserved, then waiting for payment; paid, it ships.
	place("O1", "apple")
	tick(time.Second)
	expect("O1 waits", instance("shop.fulfil:O1").State+" "+status("O1")+" / "+trace("shop.fulfil:O1"), "waiting reserved / started, reserve acted, paid waiting")
	expect("pay", do("bo", "shop", "shop.order.pay", "shop.order", "O1", struct{}{}), "ok")
	tick(time.Second)
	expect("O1 done", instance("shop.fulfil:O1").State+" "+status("O1"), "done shipped")
	expect("on behalf of", instance("shop.fulfil:O1").OnBehalf, "ana")

	// Unpaid for an hour: a clerk is asked; answering cancel undoes the reservation.
	place("O2", "pear")
	tick(time.Hour + 2*time.Second)
	inbox := tasks("bo")
	expect("chase task", fmt.Sprintf("%d %s %v", len(inbox), inbox[0].Title, inbox[0].Answers), "1 Chase payment of O2 [wait cancel]")
	expect("a wrong answer", do("bo", work.ID, "work.task.complete", work.TaskType, inbox[0].ID, map[string]string{"answer": "maybe"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect("cancel", do("bo", work.ID, "work.task.complete", work.TaskType, inbox[0].ID, map[string]string{"answer": "cancel"}), "ok")
	tick(time.Second)
	expect("O2 compensated", instance("shop.fulfil:O2").State+" "+status("O2")+" / "+trace("shop.fulfil:O2"),
		"compensated released / started, reserve acted, paid waiting, paid timeout, chase asked, chase answered, chase chose, compensating, reserve undone, ended")

	// Shipping fails every time: retried with backoff, then the reservation is undone.
	place("O3", "broken")
	tick(time.Second)
	do("ana", "shop", "shop.order.pay", "shop.order", "O3", struct{}{})
	tick(time.Minute)
	expect("O3 compensated", instance("shop.fulfil:O3").State+" "+status("O3"), "compensated released")
	expect("O3 retried", fmt.Sprint(strings.Count(trace("shop.fulfil:O3"), "ship retry")), "4")

	// Nothing to undo when the first act fails; stuck when an undo keeps failing:
	// the owners are asked; an administrator skips the undo, and the flow ends.
	place("O4", "none")
	tick(time.Minute)
	expect("O4", instance("shop.fulfil:O4").State+" "+status("O4"), "compensated placed")
	expect("O4's failure told to the owners (#118)", fmt.Sprint(slices.ContainsFunc(tasks("bo"), func(w work.WorkTask) bool { return strings.HasPrefix(w.Title, "Flow Fulfil order O4 failed at ") })), "true")
	do("ana", "shop", "shop.order.place", "shop.order", "O5", map[string]string{"item": "stuck"})
	tick(time.Second)
	now = now.Add(2 * time.Hour) // unpaid past the chase; the clerk cancels; the release is refused
	tick(time.Second)
	chase := slices.IndexFunc(tasks("ana"), func(w work.WorkTask) bool { return w.Title == "Chase payment of O5" })
	do("ana", work.ID, "work.task.complete", work.TaskType, tasks("ana")[chase].ID, map[string]string{"answer": "cancel"})
	tick(time.Minute)
	stuck := instance("shop.fulfil:O5")
	expect("O5 stuck", stuck.State+" "+fmt.Sprint(slices.ContainsFunc(tasks("bo"), func(w work.WorkTask) bool { return w.Title == "Flow Fulfil order O5 is stuck" })), "stuck true")
	expect("skip", do("ana", flow.ID, flow.SchemaFlowSkip, flow.InstanceType, "shop.fulfil:O5", map[string]int{"token": stuck.Tokens[0].ID}), "ok")
	expect("O5 ended", instance("shop.fulfil:O5").State+" "+status("O5"), "compensated reserved")
	expect("stuck task closed", fmt.Sprint(slices.ContainsFunc(tasks("bo"), func(w work.WorkTask) bool { return w.Title == "Flow Fulfil order O5 is stuck" })), "false")

	// One running instance per key; a new one once it ended.
	place("O6", "fig")
	tick(time.Second)
	expect("cancel O6", do("ana", flow.ID, flow.SchemaFlowStop, flow.InstanceType, "shop.fulfil:O6", struct{}{}), "ok")
	expect("canceled", instance("shop.fulfil:O6").State+" "+status("O6"), "canceled reserved")

	// Version 2 ships, O7 still waits on version 1: it keeps it; moved, it packs in
	// parallel (a label, and the bill through a sub-flow after its time).
	place("O7", "plum")
	tick(time.Second)
	CheckReplay(t, tn, journal, func() *Tenant { return build(fulfil(1)) })
	v2 := build(fulfil(1), bill, fulfil(2))
	if err := v2.Replay(journal); err != nil {
		t.Fatal(err)
	}
	tn, journal = v2, slices.Clone(journal)
	tn.Record = func(e Entry) { journal = append(journal, e) }
	if err := tn.procs.Check(); err != nil {
		t.Fatal(err)
	}
	expect("pinned", fmt.Sprint(instance("shop.fulfil:O7").Version), "1")
	expect("move", do("ana", flow.ID, flow.SchemaFlowMove, flow.InstanceType, "shop.fulfil:O7", struct{}{}), "ok")
	expect("moved", fmt.Sprint(instance("shop.fulfil:O7").Version), "2")
	do("bo", "shop", "shop.order.pay", "shop.order", "O7", struct{}{})
	tick(time.Second)
	expect("packing", instance("shop.fulfil:O7").State+" "+status("O7"), "waiting labelled")
	now = time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC) // the bill's time, reached by the timer
	tick(2 * time.Second)
	expect("O7 done", instance("shop.fulfil:O7").State+" "+status("O7")+" "+instance("shop.fulfil:O7/3").State, "done shipped done")
	expect("O7 trace", trace("shop.fulfil:O7"), "started, reserve acted, paid waiting, moved, paid event, pack branched, label acted, invoice called, invoice returned, pack joined, ship acted, ended")

	// A journal with an instance on a version the code dropped refuses to start.
	place("O8", "kiwi")
	tick(time.Second)
	CheckReplay(t, tn, journal, func() *Tenant { return build(fulfil(1), bill, fulfil(2)) })
	dropped := build(bill, platform.Flow{Name: "fulfil", Title: "Fulfil order", Version: 3, Start: fulfil(1).Start, Steps: fulfil(1).Steps})
	if err := dropped.Replay(journal); err == nil {
		if err := dropped.procs.Check(); err == nil || !strings.Contains(err.Error(), "version 2") {
			t.Fatalf("a dropped version was not refused: %v", err)
		}
	}

	// Declarations are checked at composition.
	for _, bad := range []platform.Flow{
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: []string{"shop.order.place"}, Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Next: "b", Act: shopAct("ship")}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: []string{"crm.won"}, Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Act: shopAct("ship")}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: []string{"shop.order.place"}, Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Act: shopAct("ship"), Ask: &platform.Ask{}}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: []string{"shop.order.place"}, Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Wait: &platform.Wait{On: "shop.order.pay"}, Timeout: time.Hour}}},
	} {
		if _, err := NewTenant("t", flow.New("t"), newShop("t", bad)); err == nil {
			t.Errorf("flow accepted: %+v", bad.Steps)
		}
	}
	if _, err := NewTenant("t", newShop("t", fulfil(1))); err == nil {
		t.Error("flows without a flow app were accepted")
	}
}
