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
	compose := func() *Tenant {
		tn, err := NewTenant("table-profile", NewConsole("table-profile", Seat{Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("table-profile"))
		if err != nil {
			t.Fatal(err)
		}
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
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprintf("%s:%s:%d", id, verb, len(entries)), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at); err != nil {
			t.Fatal(err)
		}
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
