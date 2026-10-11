package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// TestPostgresSevenNodeContinuousGraph runs the seven-node telemetry graph
// (source, window, aggregate, threshold, alert action, variable output and
// dead letters) from a real PostgreSQL stream. The graph is declared as an
// ordinary build Process, the same shape the workshop importer emits. Each
// alert is an ordinary application action on the probe record it names.
func TestPostgresSevenNodeContinuousGraph(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	db, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(ctx)
	table := pgx.Identifier{fmt.Sprintf("seven_node_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := db.Exec(ctx, "create table "+table+" (seq bigint primary key, sku text, plant text, probeid text, at timestamptz, reading double precision)"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, "drop table "+table)
	// Eight probes, three on-target readings each, plus one reading that
	// arrives after the window's watermark (a late event).
	rows := []string{}
	seq := 0
	for p := 0; p < 8; p++ {
		for r := 0; r < 3; r++ {
			seq++
			rows = append(rows, fmt.Sprintf("(%d,'sku-%d','P1','%d','2026-10-06T08:59:54Z',12.5)", seq, seq, p))
		}
	}
	seq++
	rows = append(rows, fmt.Sprintf("(%d,'late','P1','0','2026-10-06T08:59:20Z',12.5)", seq))
	if _, err := db.Exec(ctx, "insert into "+table+" values "+strings.Join(rows, ",")); err != nil {
		t.Fatal(err)
	}

	files := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, "seven-node", []Seat{seatOf("builder", build.ID+":"+build.Builder, flow.ID+":admin", "frames:operator"), seatOf("admin", "platform:admin")},
			build.New("seven-node"), work.New("seven-node"), flow.New("seven-node"), newNativeFrameSource("seven-node"))
		tn.Files = files
		return tn
	}
	tn := compose()
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if address.User != nil {
		address.User = url.User(address.User.Username())
	}
	params := address.Query()
	params.Del("password")
	address.RawQuery = params.Encode()
	password := config.Password
	if password == "" {
		password = "trust-placeholder"
	}
	tn.Secrets = func(name string) ([]byte, bool) { return []byte(password), name == "graph-password" }
	const secret = "graph-password"
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		decide(t, tn, "builder", build.ID, typ+"."+verb, typ, id, payload, at)
	}
	submit(build.ConnectionType, "db", "create", map[string]any{"name": "db", "title": "Database", "kind": "postgres", "address": address.String(), "secret": secret})
	submit(build.ConnectionType, "db", "check", map[string]any{})
	tn.CheckConnections(at)
	connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "db")
	if connection.State != "ready" {
		t.Fatalf("database check: %+v", connection.Last)
	}
	submit(build.SourceType, "stream", "create", map[string]any{"name": "telemetry", "title": "Telemetry", "connection": "db", "profile": "table", "entity": strings.Trim(table, "\""), "since": "seq", "stream": true})
	submit(build.SourceType, "stream", "publish", map[string]any{})

	// Probe records: one per partition. Probe 7 refuses its action once, so
	// its alert becomes a versioned dead letter instead of a silent loss.
	for p := 0; p < 8; p++ {
		id := fmt.Sprint(p)
		decide(t, tn, "builder", "frames", "frames.probe.create", "frames.probe", id, map[string]any{"blocked": id == "7"}, at)
	}

	continuous := &platform.Continuous{
		Source: "telemetry", Batch: 512, State: 2 << 20, FrameBytes: 4 << 20,
		Concurrency: 8, CheckpointEvery: 2, DeadLetter: true, DeadLetterMaxRecords: 16, ErrorPolicy: "deadLetter", DeadLetterRetry: true,
		Intake:    &platform.StreamIntake{SourceRecord: "stream", Key: "sku", Partition: []string{"plant", "probeid"}, Fields: []string{"probeid"}, EventTime: "at", Value: "reading"},
		Window:    &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"},
		Aggregate: &platform.StreamAggregate{Node: "aggregate", Kernel: "wasm:f32-window-stats", Signal: "reading", Group: []string{"probeid"}, Measures: []string{"count", "sum", "mean", "min", "max", "above"}},
		Threshold: &platform.StreamThreshold{Node: "threshold", Field: "mean", High: 11.5, Low: 9.5},
		Output:    &platform.StreamOutput{Node: "output", Name: "stats", Mode: "replace"},
		Entry:     "alertsloop",
	}
	steps := []build.ProcessStep{
		{Name: "alertsloop", Kind: "foreach", Collection: &platform.Binding{Source: "input", Path: []string{"triggered"}}, Body: "accept", Concurrency: 1, MaxIterations: 1000, Next: "completed"},
		{Name: "accept", Kind: "action", Act: "frames.probe.accept", Target: &platform.Binding{Source: "item", Path: []string{"probeid"}},
			Inputs: map[string]platform.Binding{"reading": {Source: "item", Path: []string{"reading"}}}, Error: "rejected"},
		{Name: "rejected", Kind: "end"}, {Name: "completed", Kind: "end"},
	}
	submit(build.ProcessType, "sevennode", "create", map[string]any{"name": "sevennode", "title": "Seven-node telemetry", "continuous": continuous, "steps": steps})
	submit(build.ProcessType, "sevennode", "publish", map[string]any{})
	tn.Work(at)
	const instance = "build.sevennode:continuous-v1"
	tn.PullContinuousSources(at)
	member := memberOf(t, tn, "builder")
	frame, failure := tn.ReadFlowFrame(member, instance, at)
	if failure != nil || frame == nil || frame.Source == nil {
		t.Fatalf("no PostgreSQL checkpoint for the seven-node graph: %v", failure)
	}
	if frame.Source.Cursor != fmt.Sprint(seq) || frame.Source.Record != "stream" {
		t.Fatalf("source cursor %q, want %d; frame %+v", frame.Source.Cursor, seq, frame.Source)
	}
	// Quiet ticks after the grace interval let the window emit its slides.
	// The output mode is replace: a later quiet slide overwrites the stats.
	// The run must emit at least one slide carrying all eight partitions.
	var sawStats bool
	for tick := 1; tick <= 8; tick++ {
		tickAt := at.Add(time.Duration(tick*7) * time.Second)
		tn.PullContinuousSources(tickAt)
		tn.Work(tickAt)
		slides, _, slideErr := platform.Find[platform.FlowOutput](tn.automation(flow.ID, false), platform.Query{})
		if slideErr != nil {
			t.Fatal(slideErr)
		}
		for _, o := range slides {
			var rows []map[string]any
			if json.Unmarshal(o.Value, &rows) == nil && len(rows) == 8 && rows[0]["mean"] == 12.5 {
				sawStats = true
			}
		}
	}
	// A second accepted source batch reaches the checkpoint interval of two.
	if _, err := db.Exec(ctx, "insert into "+table+" values (100,'sku-next','P1','1','2026-10-06T08:59:57Z',12.5)"); err != nil {
		t.Fatal(err)
	}
	secondAt := at.Add(60 * time.Second) // after the quiet ticks, the stream is quiescent
	// The source applies backpressure while the previous window's alert loop
	// is still running; the flow's own work completes it first.
	for range 3 {
		tn.Work(secondAt)
	}
	if _, refusal := tn.ConsumeFlowSource(memberOf(t, tn, "builder"), instance, secondAt); refusal != nil {
		t.Fatalf("the quiescent stream refused its next batch: %s", refusal.Message)
	}
	second, failure := tn.ReadFlowFrame(member, instance, secondAt)
	if failure != nil || second == nil || second.Source == nil || second.Source.Cursor != "100" || second.CheckpointArtifact == nil {
		t.Fatalf("the second batch did not seal a periodic checkpoint: %v second=%+v", failure, second)
	}
	for p := 0; p < 8; p++ {
		record, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), fmt.Sprint(p))
		if p == 7 {
			if record.Applied != 0 {
				t.Fatalf("blocked probe 7 applied %d readings", record.Applied)
			}
			continue
		}
		if record.Applied < 1 || record.Reading != 12.5 {
			t.Fatalf("probe %d: applied=%d reading=%v", p, record.Applied, record.Reading)
		}
	}
	letters, _, findErr := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if findErr != nil {
		t.Fatal(findErr)
	}
	var refusedAlert, lateSignal bool
	for _, l := range letters {
		refusedAlert = refusedAlert || l.Action == "frames.probe.accept" && l.Target == "7" && l.Version == 1
		raw, _ := json.Marshal(l.Letter)
		lateSignal = lateSignal || strings.Contains(string(raw), "late")
	}
	if !refusedAlert || !lateSignal {
		t.Fatalf("dead letters: refused alert=%v late signal=%v (%d letters)", refusedAlert, lateSignal, len(letters))
	}
	if !sawStats {
		t.Fatal("no emitted slide carried the eight aggregated partitions")
	}
	outputs, _, findErr := platform.Find[platform.FlowOutput](tn.automation(flow.ID, false), platform.Query{})
	if findErr != nil || len(outputs) != 1 || outputs[0].Name != "stats" || outputs[0].Version != 1 {
		t.Fatalf("the read-only stats output is not one version-bound asset: %d %v", len(outputs), findErr)
	}
	if got := refuse(t, tn, "builder", flow.ID, flow.SchemaFlowResume, flow.InstanceType, instance, map[string]any{}, at); got == "ok" {
		t.Fatal("a running graph could be resumed")
	}
	decide(t, tn, "builder", flow.ID, flow.SchemaFlowStop, flow.InstanceType, instance, map[string]any{}, at)
	if _, err := tn.ConsumeFlowSource(memberOf(t, tn, "builder"), instance, at); err == nil {
		t.Fatal("explicit stop kept consuming the PostgreSQL stream")
	}
	decide(t, tn, "builder", flow.ID, flow.SchemaFlowResume, flow.InstanceType, instance, map[string]any{}, at)
	resumed, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), instance)
	if resumed.Version != 1 || resumed.State != "waiting" {
		t.Fatalf("same-version resume lost the graph: version=%d state=%s", resumed.Version, resumed.State)
	}
	CheckReplay(t, tn, entries, compose)
}
