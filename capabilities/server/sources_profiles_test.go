package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestPostgresTableProfilePullAndReplay(t *testing.T) {
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
	table := pgx.Identifier{fmt.Sprintf("source_profile_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := db.Exec(ctx, "create table "+table+" (seq bigint primary key, sku text); insert into "+table+" values (2, 'A'), (9, 'B'), (10, 'C')"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, "drop table "+table)
	files := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, "table-profile", []Seat{seatOf("builder", "build:builder", "flow:admin"), seatOf("admin", "platform:admin")}, build.New("table-profile"), work.New("table-profile"), flow.New("table-profile"))
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
	tn.Secrets = func(name string) ([]byte, bool) { return []byte(config.Password), name == "profile-password" }
	secret := ""
	if config.Password != "" {
		secret = "profile-password"
	}
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member, _ := tn.Member("builder")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		decide(t, tn, "builder", build.ID, typ+"."+verb, typ, id, payload, at)
	}
	submit(build.DatasetType, "rows", "create", map[string]any{"name": "tablerows", "title": "Table rows"})
	submit(build.ConnectionType, "db", "create", map[string]any{"name": "db", "title": "Database", "kind": "postgres", "address": address.String(), "secret": secret})
	submit(build.ConnectionType, "db", "check", map[string]any{})
	tn.CheckConnections(at)
	connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "db")
	if connection.State != "ready" {
		t.Fatalf("database check: %+v", connection.Last)
	}
	reader, err := tn.openPostgres(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	var readOnly string
	if err := reader.QueryRow(ctx, "show transaction_read_only").Scan(&readOnly); err != nil {
		t.Fatal(err)
	}
	reader.Close(ctx)
	if readOnly != "on" {
		t.Fatal("source database session is writable")
	}
	submit(build.SourceType, "source", "create", map[string]any{"name": "tableprobe", "title": "Table probe", "connection": "db", "profile": "table", "entity": strings.Trim(table, "\""), "since": "seq", "dataset": "rows"})
	submit(build.SourceType, "source", "publish", map[string]any{})
	tn.PullSources(at)
	source, _ := platform.Get[build.Source](tn.automation(build.ID, false), "source")
	if source.Cursor != "10" || source.Last == nil || source.Last.Applied != 3 {
		t.Fatalf("first pull: %+v", source)
	}
	if _, err := db.Exec(ctx, "insert into "+table+" values (11, 'D'), (12, 'E')"); err != nil {
		t.Fatal(err)
	}
	submit(build.SourceType, "source", "pull", map[string]any{})
	tn.PullSources(at.Add(time.Second))
	source, _ = platform.Get[build.Source](tn.automation(build.ID, false), "source")
	if source.Cursor != "12" || source.Last.Applied != 2 {
		t.Fatalf("incremental pull: %+v", source)
	}
	version, _ := platform.Get[build.DatasetVersion](tn.automation(build.ID, false), "rows@2")
	var rows []map[string]any
	if err := json.Unmarshal(version.Data, &rows); err != nil || len(rows) != 2 {
		t.Fatalf("loaded rows: %s (%v)", version.Data, err)
	}
	CheckReplay(t, tn, entries, compose)

	// The same controlled PostgreSQL connection feeds an actual continuous
	// instance. Machine-rate rows never become dataset versions or objects.
	if _, err := db.Exec(ctx, "truncate "+table+"; alter table "+table+" add plant text default 'P', add device text default 'D', add at timestamptz default '2026-10-06T09:00:00Z', add reading double precision default 12; insert into "+table+"(seq,sku) select n,'event-'||n from generate_series(1,1025) n"); err != nil {
		t.Fatal(err)
	}
	submit(build.SourceType, "stream", "create", map[string]any{"name": "telemetry", "title": "Telemetry", "connection": "db", "profile": "table", "entity": strings.Trim(table, "\""), "since": "seq", "stream": true})
	submit(build.SourceType, "stream", "publish", map[string]any{})
	continuous := &platform.Continuous{Source: "telemetry", Batch: 512, State: 2 << 20, FrameBytes: 4 << 20, DeadLetter: true,
		Intake: &platform.StreamIntake{SourceRecord: "stream", Key: "sku", Partition: []string{"plant", "device"}, EventTime: "at", Value: "reading"},
		Window: &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 50000, LateEvents: "sideOutput"}}
	submit(build.ProcessType, "consumer", "create", map[string]any{"name": "telemetry", "title": "Telemetry consumer", "manual": true, "continuous": continuous,
		"steps": []build.ProcessStep{{Name: "intake", Kind: "wait", Condition: &platform.Predicate{Op: "eq", Left: &platform.Binding{Source: "literal", Value: platform.Raw(false)}, Right: &platform.Binding{Source: "literal", Value: platform.Raw(true)}}}}})
	submit(build.ProcessType, "consumer", "publish", map[string]any{})
	submit(build.ProcessType, "consumer", "run", map[string]any{"key": "pg"})
	const instance = "build.telemetry:pg"
	tn.PullContinuousSources(at)
	frame, failure := tn.ReadFlowFrame(member, instance, at)
	if failure != nil || frame.Consumed != 1025 || frame.Source == nil || frame.Source.Cursor != "1025" || frame.Source.Record != "stream" {
		t.Fatalf("real source checkpoint unavailable or counters incorrect: %v", failure)
	}
	for tick := 1; tick <= 4; tick++ {
		tn.Work(at.Add(time.Duration(tick) * time.Second))
	}
	running, _ := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), instance)
	if running.State != "waiting" || len(running.Tokens) != 1 || running.Tokens[0].Waits != "wait" || !running.Tokens[0].Due.IsZero() {
		t.Fatal("the original timer mistook the initial yield deadline for a wait timeout")
	}
	stream, _ := platform.Get[build.Source](tn.automation(build.ID, false), "stream")
	if stream.Cursor != "" || stream.Last != nil || stream.Requested || stream.Due(at) {
		t.Fatal("the stream also scheduled an import or kept a second cursor")
	}
	count := len(entries)
	tn.PullContinuousSources(at)
	if len(entries) != count {
		t.Fatal("empty source pull wrote another accepted batch")
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, snapErr := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	restored.Secrets = tn.Secrets
	restored.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	if _, err := db.Exec(ctx, "insert into "+table+"(seq,sku,at) values (1026,'after-recovery','2026-10-06T09:00:01Z'), (1027,'late','2026-10-06T08:59:20Z')"); err != nil {
		t.Fatal(err)
	}
	restored.PullContinuousSources(at.Add(time.Second))
	recovered, failure := restored.ReadFlowFrame(memberOf(t, restored, "builder"), instance, at)
	if failure != nil || recovered.Source.Cursor != "1027" || recovered.Consumed != 1026 || recovered.Rejected != 1 || len(recovered.DeadLetters) != 1 {
		t.Fatalf("restored source did not continue the accepted cursor/window: %v", failure)
	}
	// Pause/republication, changed definitions, and stopped instances cannot
	// read past the committed predecessor or silently rebind a running flow.
	decide(t, restored, "builder", build.ID, "build.source.pause", build.SourceType, "stream", map[string]any{}, at)
	if _, err := restored.ConsumeFlowSource(memberOf(t, restored, "builder"), instance, at); err == nil {
		t.Fatal("a paused source continued consuming")
	}
	decide(t, restored, "builder", build.ID, "build.source.publish", build.SourceType, "stream", map[string]any{}, at)
	if _, err := restored.ConsumeFlowSource(memberOf(t, restored, "builder"), instance, at); err != nil {
		t.Fatalf("an unchanged source did not resume: %v", err)
	}
	decide(t, restored, "builder", build.ID, "build.source.edit", build.SourceType, "stream", map[string]any{"filter": "seq > 10"}, at)
	if _, err := restored.ConsumeFlowSource(memberOf(t, restored, "builder"), instance, at); err == nil {
		t.Fatal("a changed source silently rebound its consumer")
	}
	decide(t, restored, "builder", build.ID, "build.source.edit", build.SourceType, "stream", map[string]any{"filter": ""}, at)
	decide(t, restored, "builder", build.ID, build.SchemaProcessRun, build.ProcessType, "consumer", map[string]any{"key": "revoked"}, at)
	// Revoke the source role after the outside read captured its member,
	// before its prepared frame is accepted; the old grant cannot carry it.
	secrets, revoked := restored.Secrets, false
	restored.Secrets = func(name string) ([]byte, bool) {
		if !revoked {
			revoked = true
			decide(t, restored, "admin", PlatformApp, "platform.member.revoke", "platform.member", "builder", map[string]any{"app": build.ID}, at)
		}
		return secrets(name)
	}
	files.mu.Lock()
	beforeFiles := len(files.m)
	files.mu.Unlock()
	if _, err := restored.ConsumeFlowSource(memberOf(t, restored, "builder"), "build.telemetry:revoked", at); err == nil || !revoked {
		t.Fatalf("a captured source grant survived its revocation during I/O (revoked=%v, refusal=%v, role=%s)", revoked, err, memberOf(t, restored, "builder").Roles[build.ID])
	}
	files.mu.Lock()
	afterFiles := len(files.m)
	files.mu.Unlock()
	if afterFiles != beforeFiles {
		t.Fatal("permission refusal kept an unused prepared source frame")
	}
	restored.Secrets = secrets
	decide(t, restored, "admin", PlatformApp, "platform.member.grant", "platform.member", "builder", map[string]any{"app": build.ID, "role": build.Builder}, at)
	decide(t, restored, "builder", flow.ID, flow.SchemaFlowStop, flow.InstanceType, instance, map[string]any{}, at)
	if _, err := restored.ConsumeFlowSource(memberOf(t, restored, "builder"), instance, at); err == nil {
		t.Fatal("a canceled flow continued consuming")
	}
}

func TestODataWalksPagesPastCursor(t *testing.T) {
	var seen []string
	tn := &Tenant{Outbound: func(req *http.Request, allow bool) (*http.Response, error) {
		seen = append(seen, req.URL.String())
		body := `{"value":[{"Matnr":"A","Changed":"2026-01-02T00:00:00Z"}],"@odata.nextLink":"Products?$skiptoken=2"}`
		if strings.Contains(req.URL.RawQuery, "skiptoken") {
			body = `{"value":[{"Matnr":"B","Changed":"2026-01-03T00:00:00Z"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}
	c := build.Connection{Kind: "odata", Address: "https://sap.example/sap/opu/odata/sap/API_PRODUCT/"}
	s := build.Source{Profile: "odata", Entity: "Products", Since: "Changed", Cursor: "2026-01-01T00:00:00Z", Filter: "Plant eq '1000'"}
	rows, err := tn.readOData(s, c)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if !strings.Contains(seen[0], "%24filter=%28Plant+eq+%271000%27%29+and+Changed+gt+2026-01-01T00%3A00%3A00Z") || !strings.Contains(seen[0], "%24orderby=Changed") {
		t.Fatalf("first request %s", seen[0])
	}
	if seen[1] != "https://sap.example/sap/opu/odata/sap/API_PRODUCT/Products?$skiptoken=2" {
		t.Fatalf("next %s", seen[1])
	}
	if got := s.Advance(rows); got != "2026-01-03T00:00:00Z" {
		t.Fatalf("cursor %s", got)
	}
}

func TestConnectionChecksPersistAndReplay(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("connections", NewConsole("connections", Seat{Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("connections"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	for _, durable := range []bool{false, true} {
		tn := compose()
		var entries []Entry
		tn.Record = func(e Entry) { entries = append(entries, e) }
		if durable {
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
		}
		member, _ := tn.Member("builder")
		at := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
		for i, address := range []string{"postgres://reader:must-not-persist@db.example/data", "postgres://reader@db.example/data?password=must-not-persist"} {
			id := fmt.Sprintf("credential%d", i)
			if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: id,
				Target: &pb.EntityRef{Type: build.ConnectionType, Id: id}, Schema: &pb.SchemaRef{Name: build.ConnectionType + ".create", Version: 1},
				Payload: platform.Raw(map[string]any{"name": id, "title": id, "kind": "postgres", "address": address})}, at); err == nil {
				t.Fatal("persisted an address containing a password")
			}
			if _, ok := platform.Get[build.Connection](tn.automation(build.ID, false), id); ok {
				t.Fatal("refused credential address entered the record store")
			}
		}
		submit := func(id, verb string, payload any) *pb.ChangeRecord {
			r, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: id + verb,
				Target: &pb.EntityRef{Type: build.ConnectionType, Id: id}, Schema: &pb.SchemaRef{Name: build.ConnectionType + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		for _, id := range []string{"good", "failed"} {
			submit(id, "create", map[string]any{"name": id, "title": id, "kind": "http", "address": "https://" + id + ".example"})
			submit(id, "check", map[string]any{})
			pending, _ := platform.Get[build.Connection](tn.automation(build.ID, false), id)
			if pending.State != "draft" || !pending.Requested {
				t.Fatalf("pending check: %+v", pending)
			}
			if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: id + "forged",
				Target: &pb.EntityRef{Type: build.ConnectionType, Id: id}, Schema: &pb.SchemaRef{Name: build.SchemaConnectionChecked, Version: 1},
				Payload: platform.Raw(map[string]any{"revision": pending.Revision, "check": build.ConnectionCheck{At: at, OK: true}})}, at); err == nil {
				t.Fatal("builder forged a host check result")
			}
		}
		tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
			status := 200
			if req.URL.Host == "failed.example" {
				status = 503
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		tn.CheckConnections(at.Add(time.Second))
		for _, id := range []string{"good", "failed"} {
			conn, _ := platform.Get[build.Connection](tn.automation(build.ID, false), id)
			want := "ready"
			if id == "failed" {
				want = "draft"
			}
			if conn.State != want || conn.Requested || conn.Last == nil || conn.Last.OK != (id == "good") {
				t.Fatalf("completed check: %+v", conn)
			}
		}
		CheckReplay(t, tn, entries, compose)
	}
}

func TestODataRejectsCrossOriginAndIncompletePagination(t *testing.T) {
	for _, next := range []string{"https://elsewhere.example/Products", "Products?$skiptoken=more"} {
		tn := &Tenant{Outbound: func(*http.Request, bool) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":[{"Id":"1"}],"@odata.nextLink":"` + next + `"}`))}, nil
		}}
		if _, err := tn.readOData(build.Source{Entity: "Products"}, build.Connection{Address: "https://service.example/odata/"}); err == nil {
			t.Fatalf("accepted unsafe/incomplete pagination %s", next)
		}
	}
}

func TestODataV2Shape(t *testing.T) {
	tn := &Tenant{Outbound: func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"d":{"results":[{"Id":"1"}]}}`))}, nil
	}}
	rows, err := tn.readOData(build.Source{Profile: "odata", Entity: "Set"}, build.Connection{Kind: "odata", Address: "https://x.example/odata"})
	if err != nil || len(rows) != 1 || rows[0]["Id"] != "1" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}
