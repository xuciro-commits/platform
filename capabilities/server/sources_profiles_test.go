package platformserver

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

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
