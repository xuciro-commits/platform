package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestBusinessConfirmationKeepsIndependentReceiptsAndReconciles(t *testing.T) {
	at := time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "confirmation", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer")}, build.New("confirmation"), work.New("confirmation"))
	}
	tn := compose()
	entries := []Entry{}
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	posts := 0
	sourceState := "pending"
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method == "GET" {
			return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`[{"id":"R1","state":"` + sourceState + `"}]`))}, nil
		}
		posts++
		if strings.HasSuffix(req.URL.Path, "wms") {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"state":"confirmed"}`))}, nil
	}
	fields := []build.Field{{Name: "status", Title: "Status", Type: "text"}}
	complete := build.Action{Name: "complete", Title: "Complete receipt", From: []string{"open"}, Sets: []build.Set{{Field: "status", From: "=complete"}}}
	start := build.Action{Name: "start", Title: "Start receipt", From: []string{"open"}, Roles: []string{build.User, build.Builder}, Transaction: &platform.BusinessTxn{Subject: "build.receipt", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.receipt", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
	object := map[string]any{"name": "receipt", "title": "Receipt", "fields": fields, "states": []build.State{{Name: "open", Title: "Open"}}, "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all", Edit: true}}, "actions": []build.Action{start, complete}}
	publishObject(t, tn, "builder", "O", object, at)
	for _, id := range []string{"erp", "wms"} {
		decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, id, map[string]any{"name": id, "title": id, "kind": "http", "address": "https://" + id + ".example/"}, at)
		decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, id, map[string]any{}, at)
	}
	tn.CheckConnections(at)
	for _, id := range []string{"erp", "wms"} {
		decide(t, tn, "builder", build.ID, "build.writeback.create", build.WritebackType, id, map[string]any{"name": id, "title": id, "connection": id, "object": "build.receipt", "on": "confirmation", "path": id}, at)
		decide(t, tn, "builder", build.ID, "build.writeback.publish", build.WritebackType, id, map[string]any{}, at)
	}
	decide(t, tn, "builder", build.ID, "build.source.create", build.SourceType, "SOURCE", map[string]any{"name": "receiptsource", "title": "External receipt status", "connection": "wms", "profile": "json", "url": "rows", "object": "build.receipt", "key": "id", "mapping": []build.SourceField{{From: "state", To: "status"}}}, at)
	decide(t, tn, "builder", build.ID, "build.source.publish", build.SourceType, "SOURCE", map[string]any{}, at)
	start.Transaction.Confirmation = &platform.Confirmation{Parties: []platform.ConfirmationParty{{Name: "erp", Title: "ERP", Writeback: "erp"}, {Name: "wms", Title: "WMS", Writeback: "wms", Reconcile: &platform.ConfirmationRead{Source: "SOURCE", KeyField: "id", ResultField: "state", Expected: json.RawMessage(`"confirmed"`), EverySeconds: 1}}}, DeadlineSeconds: 3600, OnPartial: "hold", OnExpire: "hold", OnComplete: "complete", ReviewerRole: "reviewer"}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "O", map[string]any{"actions": []build.Action{start, complete}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "O", map[string]any{}, at)
	decide(t, tn, "operator", build.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	before, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	sub := submission(tn, memberOf(t, tn, "operator"), build.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{})
	fail = true
	if _, err := tn.Submit(memberOf(t, tn, "operator"), sub, at); err == nil {
		t.Fatal("unpersisted coordination accepted")
	}
	fail = false
	after, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	if !bytes.Equal(platform.Raw(before.Record), platform.Raw(after.Record)) {
		t.Fatal("failed initiation changed business state")
	}
	if _, err := tn.Submit(memberOf(t, tn, "operator"), sub, at); err != nil {
		t.Fatalf("start: %s %s",err.Code,err.Message)
	}
	runs, _, _ := platform.Find[platform.ConfirmationRun](tn.automation(build.ID, false), platform.Query{})
	if len(runs) != 1 || len(runs[0].Parties) != 2 {
		t.Fatalf("progress: %+v", runs)
	}
	for _, effect := range tn.Effects(at) {
		if !strings.HasPrefix(effect.Event, "confirmation/") {
			continue
		}
		out := tn.sendWriteback(effect)
		tn.settleWithUsage(effect.ID, out, nil, at)
	}
	run, _ := platform.Get[platform.ConfirmationRun](tn.automation(build.ID, false), runs[0].ID)
	if run.Parties[0].State != "confirmed" || run.Parties[1].State != "unknown" || posts != 2 {
		t.Fatalf("independent receipts: %+v (%d sends)", run, posts)
	}
	if !strings.Contains(string(platform.Raw(run)), "unknown") {
		t.Fatal("unknown outcome lost")
	}
	if got := refuse(t, tn, "operator", build.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "outcome": "accepted", "reason": "claim"}, at); got == "ok" {
		t.Fatal("operator impersonated reviewer")
	}
	tn.ReconcileConfirmations(at.Add(time.Second))
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(build.ID, false), run.ID)
	if len(run.Parties[1].Differences) != 1 || run.Parties[1].Differences[0].Matched {
		t.Fatalf("difference missing: %+v", run.Parties[1])
	}
	sourceState = "confirmed"
	tn.ReconcileConfirmations(at.Add(3 * time.Second))
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(build.ID, false), run.ID)
	if run.State != "confirmed" || len(run.Parties[1].Differences) != 2 {
		t.Fatalf("source convergence: %+v", run)
	}
	tn.Work(at.Add(4 * time.Second))
	if tn.quarantined() {
		t.Fatal(tn.fault.Load())
	}
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(build.ID, false), run.ID)
	final, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	if run.Completion == "" || !strings.Contains(string(platform.Raw(final.Record)), `"status":"complete"`) {
		t.Fatalf("original completion action: %+v %s", run, platform.Raw(final.Record))
	}
	if posts != 2 {
		t.Fatal("reconciliation resent an unknown request")
	}
	CheckReplay(t, tn, entries, compose)
}
