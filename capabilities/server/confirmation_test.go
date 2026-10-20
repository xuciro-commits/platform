package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"strings"
	"sync"
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
		if req.Method != http.MethodPost {
			return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`[{"id":"R1","state":"` + sourceState + `"}]`))}, nil
		}
		posts++
		if strings.HasSuffix(req.URL.Path, "wms") {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"state":"confirmed"}`))}, nil
	}
	spec := businessConfirmationSpec("hold", "hold")
	spec.DeadlineSeconds = 3600
	prepareBusinessConfirmation(t, tn, at, spec)
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	before, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	sub := submission(tn, memberOf(t, tn, "operator"), construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{})
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
		t.Fatalf("start: %s %s; fault=%+v", err.Code, err.Message, tn.fault.Load())
	}
	runs, _, _ := platform.Find[platform.ConfirmationRun](tn.automation(construction.ID, false), platform.Query{})
	if len(runs) != 1 || len(runs[0].Parties) != 2 {
		t.Fatalf("progress: %+v", runs)
	}
	for _, effect := range tn.Effects(at) {
		if effect.Event != construction.ID+"/confirmation" {
			continue
		}
		out := tn.sendWriteback(effect)
		tn.settleWithUsage(effect.ID, out, nil, at)
	}
	run, _ := platform.Get[platform.ConfirmationRun](tn.automation(construction.ID, false), runs[0].ID)
	if run.Parties[0].State != "confirmed" || run.Parties[1].State != "unknown" || posts != 2 {
		t.Fatalf("independent receipts: %+v (%d sends)", run, posts)
	}
	if !strings.Contains(string(platform.Raw(run)), "unknown") {
		t.Fatal("unknown outcome lost")
	}
	if got := refuse(t, tn, "operator", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "outcome": "accepted", "reason": "claim"}, at); got == "ok" {
		t.Fatal("operator impersonated reviewer")
	}
	tn.ReconcileConfirmations(at.Add(time.Second))
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(construction.ID, false), run.ID)
	if len(run.Parties[1].Differences) != 1 || run.Parties[1].Differences[0].Matched {
		t.Fatalf("difference missing: state=%s fault=%+v", run.Parties[1].State, tn.fault.Load())
	}
	sourceState = "confirmed"
	tn.ReconcileConfirmations(at.Add(3 * time.Second))
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(construction.ID, false), run.ID)
	if run.State != "confirmed" || len(run.Parties[1].Differences) != 2 {
		t.Fatalf("source convergence: %+v", run)
	}
	runJob(t, tn, construction.ID, "confirmations", at.Add(4*time.Second))
	if tn.quarantined() {
		t.Fatal(tn.fault.Load())
	}
	run, _ = platform.Get[platform.ConfirmationRun](tn.automation(construction.ID, false), run.ID)
	final, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	if run.Completion == "" || !strings.Contains(string(platform.Raw(final.Record)), `"status":"complete"`) {
		t.Fatalf("original completion action: %+v %s", run, platform.Raw(final.Record))
	}
	if posts != 2 {
		t.Fatal("reconciliation resent an unknown request")
	}
	CheckReplay(t, tn, entries, compose)
}

func businessConfirmationSpec(partial, expiry string) *platform.Confirmation {
	return &platform.Confirmation{
		Parties: []platform.ConfirmationParty{
			{Name: "erp", Title: "ERP", Writeback: "erp", CompensateWriteback: "erp-reverse"},
			{Name: "wms", Title: "WMS", Writeback: "wms", CompensateWriteback: "wms-reverse", Reconcile: &platform.ConfirmationRead{Source: "SOURCE", KeyField: "id", ResultField: "state", Expected: json.RawMessage(`"confirmed"`), EverySeconds: 1}},
		}, DeadlineSeconds: 2, OnPartial: partial, OnExpire: expiry, OnComplete: "complete", OnPartialAction: "partial", ReviewerRole: "reviewer",
	}
}

func confirmationTenant(t *testing.T, id string, extra ...platform.App) *Tenant {
	t.Helper()
	apps := append(slices.Clone(extra), build.New(id), work.New(id))
	return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user", "stock:keeper"), seatOf("reviewer", "build:reviewer")}, apps...)
}

func prepareBusinessConfirmation(t *testing.T, tn *Tenant, at time.Time, spec *platform.Confirmation) {
	t.Helper()
	start := construction.Action{Name: "start", Title: "Start receipt", From: []string{"open"}, Roles: []string{construction.User, construction.Builder}, Inputs: []construction.Input{{Name: "item", Title: "Item", Type: "text"}}, Transaction: &platform.BusinessTxn{Subject: "build.receipt", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.receipt", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
	actions := []construction.Action{start, {Name: "complete", Title: "Complete receipt", From: []string{"open"}, Sets: []construction.Set{{Field: "status", From: "=complete"}}}, {Name: "partial", Title: "Partial receipt", From: []string{"open"}, Sets: []construction.Set{{Field: "status", From: "=partial"}}}}
	publishObject(t, tn, "builder", "O", map[string]any{"name": "receipt", "title": "Receipt", "fields": []construction.Field{{Name: "status", Title: "Status", Type: "text"}}, "states": []construction.State{{Name: "open", Title: "Open"}}, "access": []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all", Edit: true}}, "actions": actions}, at)
	for _, id := range []string{"erp", "wms"} {
		decide(t, tn, "builder", construction.ID, "build.connection.create", construction.ConnectionType, id, map[string]any{"name": id, "title": id, "kind": "http", "address": "https://" + id + ".example/"}, at)
		decide(t, tn, "builder", construction.ID, "build.connection.check", construction.ConnectionType, id, map[string]any{}, at)
	}
	tn.CheckConnections(at)
	for _, id := range []string{"erp", "wms"} {
		for _, reverse := range []bool{false, true} {
			name, path := id, id
			if reverse {
				name, path = id+"-reverse", id+"/reverse"
			}
			decide(t, tn, "builder", construction.ID, "build.writeback.create", construction.WritebackType, name, map[string]any{"name": strings.ReplaceAll(name, "-", ""), "title": name, "connection": id, "object": "build.receipt", "on": "confirmation", "path": path}, at)
			decide(t, tn, "builder", construction.ID, "build.writeback.publish", construction.WritebackType, name, map[string]any{}, at)
		}
	}
	decide(t, tn, "builder", construction.ID, "build.source.create", construction.SourceType, "SOURCE", map[string]any{"name": "receiptsource", "title": "External receipt status", "connection": "wms", "profile": "json", "url": "rows", "object": "build.receipt", "key": "id", "mapping": []construction.SourceField{{From: "state", To: "status"}}}, at)
	decide(t, tn, "builder", construction.ID, "build.source.publish", construction.SourceType, "SOURCE", map[string]any{}, at)
	actions[0].Transaction.Confirmation = spec
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "O", map[string]any{"actions": actions}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "O", map[string]any{}, at)
}

func confirmationResponse(code int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: code, Status: fmt.Sprintf("%d answer", code), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func currentConfirmation(t *testing.T, tn *Tenant) platform.ConfirmationRun {
	t.Helper()
	runs, _, err := platform.Find[platform.ConfirmationRun](tn.automation(construction.ID, false), platform.Query{})
	if err != nil || len(runs) != 1 {
		t.Fatalf("confirmation count=%d: %v", len(runs), err)
	}
	return runs[0]
}

func TestBusinessConfirmationDeadlineCompensatesEveryPartyAndAllowsEvidence(t *testing.T) {
	at := time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return confirmationTenant(t, "compensation") }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	reverseKeys := map[string][]string{}
	var reverseMu sync.Mutex
	refuseReverse := true
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method != http.MethodPost {
			return confirmationResponse(200, `[{"id":"R1","state":"pending"}]`)
		}
		if strings.HasSuffix(req.URL.Path, "/reverse") {
			reverseMu.Lock()
			defer reverseMu.Unlock()
			reverseKeys[req.URL.Host] = append(reverseKeys[req.URL.Host], req.Header.Get("Idempotency-Key"))
			if req.URL.Host == "wms.example" {
				return nil, io.ErrUnexpectedEOF
			}
			if refuseReverse {
				return confirmationResponse(400, `{"reason":"review needed"}`)
			}
			return confirmationResponse(200, `{"state":"canceled"}`)
		}
		if req.URL.Host == "wms.example" {
			return nil, io.ErrUnexpectedEOF
		}
		return confirmationResponse(200, `{"state":"confirmed"}`)
	}
	prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec("hold", "compensate"))
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	start := submission(tn, memberOf(t, tn, "operator"), construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{})
	first, err := tn.Submit(memberOf(t, tn, "operator"), start, at)
	if err != nil {
		t.Fatal(err)
	}
	again, err := tn.Submit(memberOf(t, tn, "operator"), start, at.Add(time.Second))
	if err != nil || !proto.Equal(first, again) {
		t.Fatal("same initiation lost its original answer")
	}
	tn.Dispatch(at)
	runJob(t, tn, construction.ID, "confirmations", at.Add(2*time.Second))
	run := currentConfirmation(t, tn)
	if run.State != "compensating" || slices.ContainsFunc(run.Parties, func(p platform.ConfirmationProgress) bool { return p.Compensation == "" }) {
		t.Fatalf("deadline omitted a party: %+v", run)
	}
	tn.Dispatch(at.Add(3 * time.Second))
	run = currentConfirmation(t, tn)
	if run.Parties[0].CompensationState != "rejected" || run.Parties[1].CompensationState != "unknown" {
		t.Fatalf("reverse outcomes: %+v", run)
	}
	if got := refuse(t, tn, "operator", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "compensation": true, "outcome": "accepted", "reason": "checked"}, at); got == "ok" {
		t.Fatal("operator impersonated a reviewer")
	}
	if got := refuse(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "compensation": true, "outcome": "retry", "reason": "guess"}, at); got == "ok" {
		t.Fatal("unknown compensation was resent")
	}
	decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "compensation": true, "outcome": "accepted", "reason": "The receiver confirmed cancellation"}, at.Add(4*time.Second))
	refuseReverse = false
	decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "erp", "compensation": true, "outcome": "retry", "reason": "The receiver is ready to cancel"}, at.Add(4*time.Second))
	tn.Dispatch(at.Add(5 * time.Second))
	run = currentConfirmation(t, tn)
	if run.State != "compensated" || len(reverseKeys["erp.example"]) != 2 || reverseKeys["erp.example"][0] != reverseKeys["erp.example"][1] || len(reverseKeys["wms.example"]) != 1 {
		t.Fatalf("compensation did not converge with stable keys: %+v %+v", run, reverseKeys)
	}
	for _, e := range tn.Effects(at) {
		if e.ID == run.Parties[1].Intent && e.State != "compensated" {
			t.Fatalf("original unknown still blocks: %+v", e)
		}
	}
	CheckReplay(t, tn, entries, compose)
}

func TestBusinessConfirmationPartialPoliciesAndLateAnswers(t *testing.T) {
	for _, policy := range []string{"hold", "proceed-with-flag", "reject"} {
		t.Run(policy, func(t *testing.T) {
			at := time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)
			tn := confirmationTenant(t, "partial-"+policy)
			tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
				if req.Method == http.MethodPost && req.URL.Host == "wms.example" {
					return confirmationResponse(400, `{"reason":"refused"}`)
				}
				return confirmationResponse(200, `{}`)
			}
			prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec(policy, "hold"))
			decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
			decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
			tn.Dispatch(at)
			runJob(t, tn, construction.ID, "confirmations", at.Add(time.Second))
			run := currentConfirmation(t, tn)
			record, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
			want := "waiting"
			if policy != "hold" {
				want = "partial"
			}
			if !run.Partial || !strings.Contains(string(platform.Raw(record.Record)), `"status":"`+want+`"`) {
				t.Fatalf("partial policy lost its visible result: %+v %s", run, platform.Raw(record.Record))
			}
		})
	}
}

func TestBusinessConfirmationRevalidationRejectsStaleSourceEvidence(t *testing.T) {
	at := time.Date(2026, 10, 14, 19, 0, 0, 0, time.UTC)
	tn := confirmationTenant(t, "source-revalidation")
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return nil, io.ErrUnexpectedEOF
		}
		return confirmationResponse(200, `[{"id":"R1","state":"confirmed"}]`)
	}
	prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec("hold", "hold"))
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
	tn.Dispatch(at)
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		run := currentConfirmation(t, tn)
		decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "outcome": "refused", "reason": "Checked external refusal during source read"}, at)
		return confirmationResponse(200, `[{"id":"R1","state":"confirmed"}]`)
	}
	tn.ReconcileConfirmations(at.Add(time.Second))
	run := currentConfirmation(t, tn)
	if run.Parties[1].State != "rejected" || len(run.Parties[1].Differences) != 0 {
		t.Fatalf("stale source replaced the human decision: %+v", run)
	}
}

func TestBusinessConfirmationReconciliationWritesOnlyChanges(t *testing.T) {
	at := time.Date(2026, 10, 15, 19, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return confirmationTenant(t, "reconciliation-changes") }
	tn := compose()
	var entries []Entry
	persist := func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	tn.AcceptResult = persist
	sourceState := "pending"
	var sourceError error
	var duringRead func()
	reads, posts := 0, 0
	outbound := func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method == http.MethodPost {
			posts++
			if req.URL.Host == "wms.example" {
				return nil, io.ErrUnexpectedEOF
			}
			return confirmationResponse(200, `{}`)
		}
		if req.URL.Path == "/rows" {
			reads++
			if duringRead != nil {
				duringRead()
			}
			if sourceError != nil {
				return nil, sourceError
			}
		}
		return confirmationResponse(200, `[{"id":"R1","state":"`+sourceState+`"}]`)
	}
	tn.Outbound = outbound
	spec := businessConfirmationSpec("hold", "hold")
	spec.DeadlineSeconds = 3600
	spec.Parties[1].Reconcile.EverySeconds = 2
	prepareBusinessConfirmation(t, tn, at, spec)
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
	tn.Dispatch(at)
	written := len(entries)
	tn.ReconcileConfirmations(at.Add(time.Second))
	run := currentConfirmation(t, tn)
	if len(entries) != written+1 || reads != 1 || len(run.Parties[1].Differences) != 1 || run.Parties[1].State != "unknown" {
		t.Fatalf("first difference was not saved: reads=%d differences=%+v", reads, run.Parties[1].Differences)
	}
	first := run.Parties[1].Differences[0]
	revision, written := run.Revision, len(entries)
	for _, seconds := range []int{2, 3, 4} {
		tn.ReconcileConfirmations(at.Add(time.Duration(seconds) * time.Second))
	}
	run = currentConfirmation(t, tn)
	if reads != 2 || len(entries) != written || run.Revision != revision || len(run.Parties[1].Differences) != 1 || !run.Parties[1].Differences[0].At.Equal(first.At) {
		t.Fatalf("identical polls wrote results or ignored the interval: reads=%d revision=%d differences=%+v", reads, run.Revision, run.Parties[1].Differences)
	}
	sourceState = "processing"
	tn.ReconcileConfirmations(at.Add(5 * time.Second))
	run = currentConfirmation(t, tn)
	if len(entries) != written+1 || len(run.Parties[1].Differences) != 2 || string(run.Parties[1].Differences[1].Observed) != `"processing"` {
		t.Fatalf("changed evidence was lost: %+v", run.Parties[1].Differences)
	}
	sourceError = errors.New("source temporarily unavailable")
	tn.ReconcileConfirmations(at.Add(7 * time.Second))
	run = currentConfirmation(t, tn)
	if len(entries) != written+2 || len(run.Parties[1].Differences) != 3 || !strings.Contains(run.Parties[1].Differences[2].Error, sourceError.Error()) {
		t.Fatalf("changed error was lost: %+v", run.Parties[1].Differences)
	}
	written = len(entries)
	tn.ReconcileConfirmations(at.Add(9 * time.Second))
	if len(entries) != written || reads != 5 {
		t.Fatalf("identical source errors were saved again: writes=%d reads=%d", len(entries)-written, reads)
	}

	// Only accepted results survive the restart, never the polling cache.
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	tn = recovered
	tn.AcceptResult, tn.Outbound = persist, outbound
	tn.ReconcileConfirmations(at.Add(11 * time.Second))
	tn.ReconcileConfirmations(at.Add(12 * time.Second))
	if reads != 6 || len(entries) != written || len(currentConfirmation(t, tn).Parties[1].Differences) != 3 {
		t.Fatalf("restart stopped reads or repeated evidence: reads=%d writes=%d", reads, len(entries)-written)
	}
	sourceState, sourceError = "confirmed", nil
	duringRead = func() {
		duringRead = nil
		decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "outcome": "refused", "reason": "Checked external refusal during source read"}, at.Add(13*time.Second))
	}
	tn.ReconcileConfirmations(at.Add(13 * time.Second))
	run = currentConfirmation(t, tn)
	if run.Parties[1].State != "rejected" || len(run.Parties[1].Differences) != 3 || len(entries) != written+1 {
		t.Fatalf("a stale source read replaced manual review: %+v", run.Parties[1])
	}
	decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": "wms", "outcome": "retry", "reason": "Receiver is ready for another checked attempt"}, at.Add(14*time.Second))
	tn.ReconcileConfirmations(at.Add(15 * time.Second))
	run = currentConfirmation(t, tn)
	if run.State != "confirmed" || len(run.Parties[1].Differences) != 4 || !run.Parties[1].Differences[3].Matched || reads != 8 || posts != 2 {
		t.Fatalf("matching evidence did not resolve the original request: state=%s reads=%d posts=%d differences=%+v", run.State, reads, posts, run.Parties[1].Differences)
	}
}

func TestBusinessConfirmationRetainsDefinitionAcrossUpgrade(t *testing.T) {
	at := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	tn := confirmationTenant(t, "confirmation-version")
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
	prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec("hold", "hold"))
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
	object, _ := platform.Get[construction.Object](tn.automation(construction.ID, false), "O")
	object.Actions[1].Sets = []construction.Set{{Field: "status", From: "=newcomplete"}}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "O", map[string]any{"actions": object.Actions}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "O", map[string]any{}, at)
	tn.Dispatch(at.Add(time.Second))
	runJob(t, tn, construction.ID, "confirmations", at.Add(time.Second))
	record, _ := tn.RecordOf(memberOf(t, tn, "operator"), "build.receipt", "R1", at)
	run := currentConfirmation(t, tn)
	if run.Completion == "" || !strings.Contains(string(platform.Raw(record.Record)), `"status":"complete"`) {
		t.Fatalf("old answer acquired new semantics: %+v %s", run, platform.Raw(record.Record))
	}
}

func TestBusinessConfirmationDurableUnknownRecovery(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			id := fmt.Sprintf("confirmation-recovery-%d", time.Now().UnixNano())
			at := time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC)
			journal := backend.open(t, id)
			if _, err := journal.Entries(ctx, id, 0); err != nil {
				t.Fatal(err)
			}
			defer backend.cleanup(t, id)
			compose := func() *Tenant { return confirmationTenant(t, id) }
			live := compose()
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return journal.AppendAccepted(ctx, id, e, key, hash) }
			live.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
				if req.Method != http.MethodPost {
					return confirmationResponse(200, `[{"id":"R1","state":"pending"}]`)
				}
				return nil, io.ErrUnexpectedEOF
			}
			prepareBusinessConfirmation(t, live, at, businessConfirmationSpec("hold", "hold"))
			decide(t, live, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
			decide(t, live, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
			// A durable outcome exists, but the process disappears before application.
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				_, err := journal.AppendAccepted(ctx, id, e, key, hash)
				if err != nil {
					return nil, err
				}
				panic("crash after accepted outcome")
			}
			runBefore := currentConfirmation(t, live)
			effect := platform.Effect{}
			for _, candidate := range live.Effects(at) {
				if candidate.ID == runBefore.Parties[0].Intent {
					effect = candidate
				}
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("crash was not injected")
					}
				}()
				live.settle(effect.ID, platform.Outcome{Effect: effect.ID, Result: "unknown", Detail: "Receiver answer lost"}, at)
			}()
			journal.Close()
			reopened := backend.reopen(t, id)
			defer reopened.Close()
			entries, err := reopened.Entries(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			recovered := compose()
			recovered.Outbound = func(*http.Request, bool) (*http.Response, error) {
				t.Fatal("recovery sent an external request")
				return nil, io.EOF
			}
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			run := currentConfirmation(t, recovered)
			if run.Parties[0].State != "unknown" || !slices.ContainsFunc(recovered.Effects(at), func(e platform.Effect) bool { return e.ID == effect.ID && e.State == "unknown" }) {
				t.Fatalf("durable unknown was lost: state=%s", run.Parties[0].State)
			}
			CheckReplay(t, recovered, entries, compose)
		})
	}
}

func TestBusinessConfirmationProtocolUsesOriginalApplicantAndMapping(t *testing.T) {
	at := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	id := "confirmation-protocol"
	template := newRequestsTenant(t, true).app("stock").(*depot)
	compose := func() *Tenant {
		stock := &resultDepot{depot: &depot{ledger: platform.NewLedger(id, "stock", template.ledger.Catalog, "stock.hold")}}
		return confirmationTenant(t, id, stock)
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
	spec := businessConfirmationSpec("hold", "hold")
	spec.Parties[0] = platform.ConfirmationParty{Name: "erp", Title: "Internal inventory", Protocol: testProtocol().ID(), Action: "hold"}
	prepareBusinessConfirmation(t, tn, at, spec)
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{"item": "pen"}, at)
	run := currentConfirmation(t, tn)
	held, known := platform.Get[Hold](tn.automation("stock", false), "R1")
	if !known || held.Item != "pen" || run.Parties[0].State != "confirmed" || run.Parties[0].Protocol == nil || run.Parties[0].Protocol.Provider != "stock" || len(run.Parties[0].Answer) == 0 {
		t.Fatalf("internal confirmation lost its source or answer: %+v %+v", run, held)
	}
	CheckReplay(t, tn, entries, func() *Tenant {
		restored := compose()
		restored.app("stock").(*resultDepot).forbidDecisions = true
		return restored
	})
}

func TestBusinessConfirmationEscalationAndExplicitLateCompletion(t *testing.T) {
	at := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	tn := confirmationTenant(t, "confirmation-escalation")
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method != http.MethodPost {
			return confirmationResponse(200, `{}`)
		}
		return nil, io.ErrUnexpectedEOF
	}
	prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec("hold", "escalate"))
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
	tn.Dispatch(at)
	runJob(t, tn, construction.ID, "confirmations", at.Add(3*time.Second))
	run := currentConfirmation(t, tn)
	if run.State != "expired" || !run.Escalated {
		t.Fatalf("deadline did not escalate: %+v", run)
	}
	for _, party := range []string{"erp", "wms"} {
		decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"party": party, "outcome": "accepted", "reason": "Checked late receipt"}, at.Add(4*time.Second))
	}
	run = currentConfirmation(t, tn)
	if run.State != "expired" || run.Completion != "" {
		t.Fatalf("late answers silently bypassed the deadline: %+v", run)
	}
	decide(t, tn, "reviewer", construction.ID, "build.confirmation.review", platform.ConfirmationType, run.ID, map[string]any{"outcome": "complete", "reason": "All late receipts checked; resume original action"}, at.Add(5*time.Second))
	runJob(t, tn, construction.ID, "confirmations", at.Add(6*time.Second))
	if currentConfirmation(t, tn).Completion == "" {
		t.Fatal("reviewed late completion did not resume")
	}
}
