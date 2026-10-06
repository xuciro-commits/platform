package platformserver

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"platformserver/apps/build"
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

func TestODataV2Shape(t *testing.T) {
	tn := &Tenant{Outbound: func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"d":{"results":[{"Id":"1"}]}}`))}, nil
	}}
	rows, err := tn.readOData(build.Source{Profile: "odata", Entity: "Set"}, build.Connection{Kind: "odata", Address: "https://x.example/odata"})
	if err != nil || len(rows) != 1 || rows[0]["Id"] != "1" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}
