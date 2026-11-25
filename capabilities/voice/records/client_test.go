package records

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinesReadsTheLinesInUseWithTheirDialingSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/records/phone.trunk" || r.Header.Get("Authorization") != "Bearer secret-token" || r.Header.Get("Platform-Tenant") != "phone" {
			http.Error(w, `{"error":"unexpected"}`, http.StatusBadRequest)
			return
		}
		off := false
		json.NewEncoder(w).Encode(map[string]any{"records": []map[string]any{
			{"account": "phone1", "password": "p1", "numberheader": " X-Dial ", "headers": "X-Sim-Slot: 1\nX-Note:  keep \n\nbroken line"},
			{"account": "phone2", "password": "p2"},
			{"account": "phone3", "password": "p3", "active": off},
			{"account": "phone4"},
		}})
	}))
	defer srv.Close()
	c := New(srv.URL, "phone", func() (string, error) { return "secret-token", nil }, nil)
	trunks, dialing, err := c.Lines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(trunks) != 2 || trunks["phone1"] != "p1" || trunks["phone2"] != "p2" {
		t.Fatalf("trunks = %v", trunks)
	}
	d, ok := dialing["phone1"]
	if !ok || d.NumberHeader != "X-Dial" || d.Headers["X-Sim-Slot"] != "1" || d.Headers["X-Note"] != "keep" || len(d.Headers) != 2 {
		t.Fatalf("dialing = %+v", dialing)
	}
	if _, ok := dialing["phone2"]; ok {
		t.Fatalf("a line without dialing settings got some: %+v", dialing)
	}
}

func TestContactFindsTheOwnerInTheFirstSourceThatHasOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rows []map[string]any
		switch r.URL.Path {
		case "/v1/records/party.contact":
			rows = []map[string]any{{"id": "C1", "organization": "O1"}}
		case "/v1/records/crm.lead":
			rows = []map[string]any{{"id": "L1"}}
		case "/v1/records/crm.contact":
			rows = []map[string]any{{"id": "CON-1", "owner": "member-7"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"records": rows})
	}))
	defer srv.Close()
	c := New(srv.URL, "phone", func() (string, error) { return "t", nil }, nil)
	c.OwnedBy([]Owner{{Type: "crm.lead", Contact: "person", Owner: "owner"}, {Type: "crm.contact", Contact: "base", Owner: "owner"}})
	got := c.Contact(context.Background(), "+8613800138000")
	if got.Contact != "C1" || got.Organization != "O1" || got.Owner != "member-7" {
		t.Fatalf("party = %+v", got)
	}
}
