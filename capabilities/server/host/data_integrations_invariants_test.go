package host

import (
	"encoding/json"
	"io"
	"net/http"
	runtimeprotocol "platformserver/kernel/protocol"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1DataIntegrationsAcceptSettleCompensateAndReplay(t *testing.T) {
	live := newInvariantTenant(t, "data-integrations")
	object := map[string]any{
		"name": "record", "title": "Record",
		"fields": []ontology.Field{{Name: "note", Title: "Note", Type: "text", Search: true}},
		"states": []ontology.State{{Name: "open", Title: "Open"}},
	}
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", object)
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	owner := live.App(build.ID).(*build.Build)
	ledger, err := owner.Authority().ForkRead()
	if err != nil {
		t.Fatal(err)
	}
	forked, err := owner.Fork(ledger)
	if err != nil {
		t.Fatal(err)
	}

	live.Outbound = func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	for _, connection := range []struct {
		id, name, address string
	}{
		{id: "EXTERNAL", name: "external", address: "https://external.example.test"},
		{id: "ALTERNATE", name: "alternate", address: "https://alternate.example.test"},
	} {
		live.accept(t, "build.connection.create", dataowner.ConnectionType, connection.id, map[string]any{
			"name": connection.name, "title": connection.name, "kind": "http", "address": connection.address,
		})
		live.accept(t, "build.connection.check", dataowner.ConnectionType, connection.id, map[string]any{})
	}
	live.CheckConnections(invariantClock)
	for _, id := range []string{"EXTERNAL", "ALTERNATE"} {
		connection, found := platform.Get[dataowner.Connection](live.automation(build.ID, false), id)
		if !found || connection.State != "ready" || connection.Requested || connection.Last == nil || !connection.Last.OK {
			t.Fatalf("connection %s was not checked: %+v", id, connection)
		}
	}

	writebacks := []struct {
		id, name, on, path string
		result             []dataowner.SourceField
	}{
		{id: "CONFIRM", name: "confirm", on: "confirmation", path: "/confirm"},
		{id: "CONFIRM-UNDO", name: "confirmundo", on: "confirmation", path: "/confirm/undo"},
		{id: "DISPATCH", name: "device", on: "dispatch", path: "/dispatch"},
		{id: "DISPATCH-UNDO", name: "deviceundo", on: "dispatch", path: "/dispatch/undo"},
		{id: "NOTIFY", name: "notify", on: "notify", path: "/notify", result: []dataowner.SourceField{{From: "message", To: "note"}}},
	}
	for _, writeback := range writebacks {
		payload := map[string]any{
			"name": writeback.name, "title": writeback.name, "connection": "EXTERNAL",
			"object": "invariantproject.record", "on": writeback.on, "method": "POST", "path": writeback.path,
		}
		if len(writeback.result) > 0 {
			payload["result"] = writeback.result
		}
		live.accept(t, "build.writeback.create", dataowner.WritebackType, writeback.id, payload)
		live.accept(t, "build.writeback.publish", dataowner.WritebackType, writeback.id, map[string]any{})
	}
	live.accept(t, "build.source.create", dataowner.SourceType, "STATUS", map[string]any{
		"name": "status", "title": "Device status", "connection": "EXTERNAL", "url": "/status",
		"object": "invariantproject.record", "key": "id",
		"mapping": []dataowner.SourceField{{From: "state", To: "note"}},
	})
	live.accept(t, "build.source.publish", dataowner.SourceType, "STATUS", map[string]any{})

	text := func(value string) platform.Expr {
		return platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw(value)}}
	}
	actions := []ontology.Action{
		{
			Name: "reserve", Title: "Reserve", From: []string{"open"},
			Transaction: &platform.BusinessTxn{
				Subject: "invariantproject.record",
				Steps: []platform.TxnStep{{Name: "reserve", Kind: "update", Target: platform.TxnRef{Type: "invariantproject.record", ID: "$subject"},
					Changes: []platform.TxnSet{{Field: "note", Expr: ptrExpr(text("reserved"))}}}},
				Confirmation: &platform.Confirmation{
					Parties:         []platform.ConfirmationParty{{Name: "carrier", Title: "Carrier", Writeback: "CONFIRM", CompensateWriteback: "CONFIRM-UNDO"}},
					DeadlineSeconds: 60, OnPartial: "hold", OnExpire: "compensate", OnComplete: "finish",
					ReviewerRole: build.Builder,
				},
			},
		},
		{
			Name: "finish", Title: "Finish", From: []string{"open"},
			Transaction: &platform.BusinessTxn{
				Subject: "invariantproject.record",
				Steps: []platform.TxnStep{{Name: "finish", Kind: "update", Target: platform.TxnRef{Type: "invariantproject.record", ID: "$subject"},
					Changes: []platform.TxnSet{{Field: "note", Expr: ptrExpr(text("finished"))}}}},
			},
		},
		{
			Name: "send", Title: "Send", From: []string{"open"},
			Transaction: &platform.BusinessTxn{
				Subject: "invariantproject.record",
				Steps: []platform.TxnStep{{Name: "send", Kind: "update", Target: platform.TxnRef{Type: "invariantproject.record", ID: "$subject"},
					Changes: []platform.TxnSet{{Field: "note", Expr: ptrExpr(text("sending"))}}}},
				Dispatch: &platform.Dispatch{
					Command: "send", Target: "EXTERNAL", Key: "$subject", DeadlineSeconds: 60,
					Readback: platform.DispatchRead{Name: "status", Source: "STATUS", KeyField: "id", ResultField: "state",
						States: map[string]string{"working": "running", "done": "completed"}, EverySeconds: 10},
					OnStuck: "manual", OnFail: "compensate", ReviewerRole: build.Builder,
				},
			},
		},
		{
			Name: "notify", Title: "Notify", From: []string{"open"},
			Transaction: &platform.BusinessTxn{
				Subject: "invariantproject.record",
				Steps: []platform.TxnStep{{Name: "notify", Kind: "update", Target: platform.TxnRef{Type: "invariantproject.record", ID: "$subject"},
					Changes: []platform.TxnSet{{Field: "note", Expr: ptrExpr(text("notifying"))}}}},
			},
		},
	}
	for _, action := range actions {
		schema := "invariantproject.record." + action.Name
		live.accept(t, "build.actiontype.create", build.ActionTypeType, strings.ToUpper(action.Name), map[string]any{
			"name": schema, "object": "invariantproject.record", "definition": action,
		})
	}
	object["dispatchCommands"] = []platform.DispatchCommand{
		{Name: "send", Title: "Send command", EndpointKind: "http", Writeback: "DISPATCH", Idempotent: true, ReceiverDeduplicates: true, Compensate: "undo"},
		{Name: "undo", Title: "Undo command", EndpointKind: "http", Writeback: "DISPATCH-UNDO", Idempotent: true, ReceiverDeduplicates: true},
	}
	object["protocols"] = []runtimeprotocol.ProtocolBinding{{
		Name: "recordapi", Version: 1,
		Actions: map[string]string{"reserve": "reserve", "send": "send"},
	}}
	live.accept(t, "build.object.edit", build.ObjectType, "OBJECT", object)
	for _, action := range []string{"finish", "send", "notify", "reserve"} {
		live.accept(t, "build.actiontype.publish", build.ActionTypeType, strings.ToUpper(action), map[string]any{})
	}
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	if _, found := owner.Manifest().Actions.Action("invariantproject.record.send"); !found {
		t.Fatal("the live owner did not publish the dispatch action")
	}
	if !slices.ContainsFunc(owner.Manifest().Provides, func(provide platform.Provision) bool {
		return provide.Protocol.Name == "recordapi" && provide.Protocol.Version == 1 && provide.Actions["send"] == "invariantproject.record.send"
	}) {
		t.Fatalf("the live owner did not publish its protocol binding: %+v", owner.Manifest().Provides)
	}
	if _, found := forked.Manifest().Actions.Action("invariantproject.record.send"); found {
		t.Fatal("a fork captured a dispatch action published after it was created")
	}
	if owner.DataOwner() == forked.(*build.Build).DataOwner() {
		t.Fatal("a fork shares the source data owner")
	}

	for _, id := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"} {
		live.accept(t, "invariantproject.record.create", "invariantproject.record", id, map[string]any{"note": "new"})
	}

	automation := live.automation(build.ID, false)
	settleEffect := func(effect platform.Effect, out platform.Outcome, at time.Time) {
		t.Helper()
		live.settle(effect.ID, out, at)
	}
	answer := func(schema, typ, id, schemaPayload string) {
		t.Helper()
		submission := live.submission(schema, typ, id, map[string]any{"tag": "carrier", "outcome": schemaPayload, "reason": "", "answer": map[string]any{"ok": true}})
		submission.PrincipalId = automation.ID
		if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, submission, invariantClock, acceptOptions{automation: true}); refusal != nil {
			t.Fatalf("%s %s/%s: %v", schema, typ, id, refusal)
		}
	}
	for _, action := range []string{"reserve", "send"} {
		submission := live.submission("invariantproject.record."+action, "invariantproject.record", "A", map[string]any{})
		refusal := platform.ProbeDecision(automation, func(probe platform.Caller) *platform.Error {
			_, refusal := owner.Submit(probe, submission, invariantClock)
			return refusal
		})
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			t.Fatalf("simulated %s did not refuse external execution: %+v", action, refusal)
		}
	}

	live.accept(t, "invariantproject.record.reserve", "invariantproject.record", "A", map[string]any{})
	run, refusal := runtimeprotocol.PendingConfirmations(automation)
	if refusal != nil || len(run) != 1 || len(run[0].Parties) != 1 || run[0].Parties[0].State != "pending" {
		t.Fatalf("confirmation was not opened: %+v %v", run, refusal)
	}
	confirmation := run[0]
	answer(runtimeprotocol.ConfirmationAnswerSchema, runtimeprotocol.ConfirmationType, confirmation.ID, "unknown")
	live.accept(t, runtimeprotocol.ConfirmationType+".review", runtimeprotocol.ConfirmationType, confirmation.ID, map[string]any{
		"party": "carrier", "outcome": "accepted", "reason": "The provider receipt was reconciled",
	})
	confirmation, _ = platform.Get[platform.ConfirmationRun](automation, confirmation.ID)
	if confirmation.State != "confirmed" || confirmation.Parties[0].State != "confirmed" {
		t.Fatalf("unknown confirmation was not reconciled: %+v", confirmation)
	}

	live.accept(t, "invariantproject.record.reserve", "invariantproject.record", "B", map[string]any{})
	run, _ = runtimeprotocol.PendingConfirmations(automation)
	var rejected platform.ConfirmationRun
	for _, candidate := range run {
		if candidate.Subject == "invariantproject.record/B" {
			rejected = candidate
		}
	}
	if rejected.ID == "" {
		t.Fatalf("rejected confirmation was not opened: %+v", run)
	}
	answer(runtimeprotocol.ConfirmationAnswerSchema, runtimeprotocol.ConfirmationType, rejected.ID, "refused")
	rejected, _ = platform.Get[platform.ConfirmationRun](automation, rejected.ID)
	if rejected.State != "partial" || rejected.Parties[0].State != "rejected" {
		t.Fatalf("confirmation refusal was not retained: %+v", rejected)
	}

	live.accept(t, "invariantproject.record.reserve", "invariantproject.record", "C", map[string]any{})
	run, _ = runtimeprotocol.PendingConfirmations(automation)
	var compensated platform.ConfirmationRun
	for _, candidate := range run {
		if candidate.Subject == "invariantproject.record/C" {
			compensated = candidate
		}
	}
	if compensated.ID == "" {
		t.Fatalf("compensated confirmation was not opened: %+v", run)
	}
	answer(runtimeprotocol.ConfirmationAnswerSchema, runtimeprotocol.ConfirmationType, compensated.ID, "accepted")
	live.accept(t, runtimeprotocol.ConfirmationType+".review", runtimeprotocol.ConfirmationType, compensated.ID, map[string]any{
		"party": "carrier", "outcome": "compensate", "reason": "Reverse the confirmed reservation",
	})
	submission := live.submission(runtimeprotocol.ConfirmationAnswerSchema, runtimeprotocol.ConfirmationType, compensated.ID, map[string]any{
		"tag": "carrier:compensate", "outcome": "accepted", "reason": "The provider reversed the reservation",
	})
	submission.PrincipalId = automation.ID
	if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, submission, invariantClock.Add(2*time.Second), acceptOptions{automation: true}); refusal != nil {
		t.Fatalf("confirmation compensation: %v", refusal)
	}
	compensated, _ = platform.Get[platform.ConfirmationRun](automation, compensated.ID)
	if compensated.State != "compensated" || compensated.Parties[0].CompensationState != "confirmed" {
		t.Fatalf("confirmation compensation was not settled: %+v", compensated)
	}

	live.accept(t, "invariantproject.record.send", "invariantproject.record", "D", map[string]any{})
	run, _ = runtimeprotocol.PendingConfirmations(automation)
	dispatches, refusal := runtimeprotocol.PendingDispatches(automation)
	if refusal != nil || len(dispatches) < 1 {
		t.Fatalf("dispatch was not opened: %+v %v", dispatches, refusal)
	}
	var dispatched platform.DispatchRun
	for _, candidate := range dispatches {
		if candidate.Subject == "invariantproject.record/D" {
			dispatched = candidate
		}
	}
	if dispatched.ID == "" || dispatched.State != "pending" || dispatched.Attempt != 1 {
		t.Fatalf("dispatch differs: %+v", dispatched)
	}
	targets, refusal := live.DispatchTargets(live.member(t), dispatched.ID, invariantClock)
	if refusal != nil || len(targets) != 2 || targets[0].ID != "ALTERNATE" || targets[1].ID != "EXTERNAL" {
		t.Fatalf("dispatch targets differ: %+v %v", targets, refusal)
	}
	reading := dataowner.DispatchAnswerSubmission(live.ID, build.ID, automation.Member, dispatched, dispatched,
		platform.DispatchReading{State: "completed", Value: "done", Observed: platform.Raw(map[string]any{"state": "done"})}, invariantClock.Add(3*time.Second))
	if reading == nil {
		t.Fatal("dispatch status submission is missing")
	}
	if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, reading, invariantClock.Add(3*time.Second), acceptOptions{automation: true}); refusal != nil {
		t.Fatalf("dispatch status: %v", refusal)
	}
	dispatched, _ = platform.Get[platform.DispatchRun](automation, dispatched.ID)
	if dispatched.State != "completed" || len(dispatched.Readback) != 1 {
		t.Fatalf("dispatch status was not retained: %+v", dispatched)
	}

	dispatchFor := func(subject string) platform.DispatchRun {
		t.Helper()
		runs, refusal := runtimeprotocol.PendingDispatches(automation)
		if refusal != nil {
			t.Fatal(refusal)
		}
		for _, run := range runs {
			if run.Subject == subject {
				return run
			}
		}
		t.Fatalf("dispatch for %s is unavailable: %+v", subject, runs)
		return platform.DispatchRun{}
	}
	effectFor := func(event, target string) platform.Effect {
		t.Helper()
		for _, queued := range live.outbound.Select(nil) {
			if queued.Event == event && queued.Target == target {
				return queued.Effect
			}
		}
		t.Fatalf("queued %s effect for %s is unavailable", event, target)
		return platform.Effect{}
	}

	live.accept(t, "invariantproject.record.send", "invariantproject.record", "J", map[string]any{})
	retried := dispatchFor("invariantproject.record/J")
	retryFailure := effectFor(build.ID+"/dispatch", retried.Subject)
	settleEffect(retryFailure, platform.Outcome{Effect: retryFailure.ID, Result: "unknown", Detail: "No delivery answer arrived"}, invariantClock.Add(4*time.Second))
	live.accept(t, runtimeprotocol.DispatchType+".review", runtimeprotocol.DispatchType, retried.ID, map[string]any{
		"outcome": "retry", "reason": "Resend the idempotent command",
	})
	retried, _ = platform.Get[platform.DispatchRun](automation, retried.ID)
	if retried.Attempt != 2 || retried.State != "pending" {
		t.Fatalf("dispatch retry was not queued: %+v", retried)
	}
	retryReading := dataowner.DispatchAnswerSubmission(live.ID, build.ID, automation.Member, retried,
		retried, platform.DispatchReading{State: "completed", Value: "done", Observed: platform.Raw(map[string]any{"state": "done"})}, invariantClock.Add(5*time.Second))
	if retryReading == nil {
		t.Fatal("retried dispatch status submission is missing")
	}
	if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, retryReading, invariantClock.Add(5*time.Second), acceptOptions{automation: true}); refusal != nil {
		t.Fatalf("retried dispatch status: %v", refusal)
	}
	retried, _ = platform.Get[platform.DispatchRun](automation, retried.ID)
	if retried.State != "completed" || len(retried.Attempts) != 2 {
		t.Fatalf("retried dispatch did not complete: %+v", retried)
	}

	live.accept(t, "invariantproject.record.send", "invariantproject.record", "E", map[string]any{})
	retargeted := dispatchFor("invariantproject.record/E")
	lost := effectFor(build.ID+"/dispatch", retargeted.Subject)
	settleEffect(lost, platform.Outcome{Effect: lost.ID, Result: "unknown", Detail: "The receiver answer was lost"}, invariantClock.Add(6*time.Second))
	retargeted, _ = platform.Get[platform.DispatchRun](automation, retargeted.ID)
	if retargeted.State != "unknown" {
		t.Fatalf("dispatch unknown outcome was not retained: %+v", retargeted)
	}
	live.accept(t, runtimeprotocol.DispatchType+".review", runtimeprotocol.DispatchType, retargeted.ID, map[string]any{
		"outcome": "retarget", "target": "ALTERNATE", "reason": "The first receiver is unavailable",
	})
	retargeted, _ = platform.Get[platform.DispatchRun](automation, retargeted.ID)
	if retargeted.Target != "ALTERNATE" || retargeted.Attempt != 2 || retargeted.State != "pending" {
		t.Fatalf("dispatch retarget was not retained: %+v", retargeted)
	}
	retargetReading := dataowner.DispatchAnswerSubmission(live.ID, build.ID, automation.Member, retargeted,
		retargeted, platform.DispatchReading{State: "completed", Value: "done", Observed: platform.Raw(map[string]any{"state": "done"})}, invariantClock.Add(7*time.Second))
	if retargetReading == nil {
		t.Fatal("retargeted dispatch status submission is missing")
	}
	if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, retargetReading, invariantClock.Add(7*time.Second), acceptOptions{automation: true}); refusal != nil {
		t.Fatalf("retargeted dispatch status: %v", refusal)
	}
	retargeted, _ = platform.Get[platform.DispatchRun](automation, retargeted.ID)
	if retargeted.State != "completed" || len(retargeted.Attempts) != 2 {
		t.Fatalf("retargeted dispatch did not complete: %+v", retargeted)
	}

	live.accept(t, "invariantproject.record.send", "invariantproject.record", "F", map[string]any{})
	compensating := dispatchFor("invariantproject.record/F")
	failed := effectFor(build.ID+"/dispatch", compensating.Subject)
	settleEffect(failed, platform.Outcome{Effect: failed.ID, Result: "rejected", Detail: "The receiver rejected the command"}, invariantClock.Add(8*time.Second))
	compensating, _ = platform.Get[platform.DispatchRun](automation, compensating.ID)
	if !compensating.Compensating || compensating.State != "compensating" || compensating.Attempt != 2 {
		t.Fatalf("dispatch compensation was not queued: %+v", compensating)
	}
	queuedCompensation, found := live.outbound.Get(compensating.Intent)
	if !found || queuedCompensation.Event != build.ID+"/dispatch" || queuedCompensation.Target != compensating.Subject {
		t.Fatalf("dispatch compensation effect is unavailable: %+v %v", queuedCompensation, found)
	}
	compensation := queuedCompensation.Effect
	settleEffect(compensation, platform.Outcome{Effect: compensation.ID, Result: "delivered", Detail: "The receiver reversed the command"}, invariantClock.Add(9*time.Second))
	compensating, _ = platform.Get[platform.DispatchRun](automation, compensating.ID)
	compensationRead := dataowner.DispatchAnswerSubmission(live.ID, build.ID, automation.Member, compensating,
		compensating, platform.DispatchReading{State: "completed", Value: "done", Observed: platform.Raw(map[string]any{"state": "done"})}, invariantClock.Add(10*time.Second))
	if compensationRead == nil {
		t.Fatal("compensated dispatch status submission is missing")
	}
	if _, refusal := live.Tenant.accept(live.App(build.ID), automation.Member, compensationRead, invariantClock.Add(10*time.Second), acceptOptions{automation: true}); refusal != nil {
		t.Fatalf("compensated dispatch status: %v", refusal)
	}
	compensating, _ = platform.Get[platform.DispatchRun](automation, compensating.ID)
	if compensating.State != "compensated" || !compensating.Compensating {
		t.Fatalf("dispatch compensation was not settled: %+v", compensating)
	}

	for _, result := range []struct {
		id, answer, wantEvent string
	}{
		{id: "G", answer: "delivered one", wantEvent: "writeback/notify"},
		{id: "H", answer: "rejected one", wantEvent: "writeback/notify"},
		{id: "I", answer: "unknown one", wantEvent: "writeback/notify"},
	} {
		live.accept(t, "invariantproject.record.notify", "invariantproject.record", result.id, map[string]any{})
		effect := effectFor(result.wantEvent, "invariantproject.record/"+result.id)
		outcome := map[string]string{"G": "delivered", "H": "rejected", "I": "unknown"}[result.id]
		settleEffect(effect, platform.Outcome{Effect: effect.ID, Result: outcome, Answer: platform.Raw(map[string]any{"message": result.answer})}, invariantClock.Add(8*time.Second))
	}
	writeback, found := platform.Get[dataowner.Writeback](automation, "NOTIFY")
	if !found || writeback.Sent != 1 || writeback.Rejected != 1 || writeback.Unknown != 1 || len(writeback.Answers) != 3 {
		t.Fatalf("writeback answers were not retained: %+v", writeback)
	}
	for id, note := range map[string]string{"G": "delivered one", "H": "notifying", "I": "notifying"} {
		view, refusal := live.RecordOf(t.Context(), live.member(t), "invariantproject.record", id, invariantClock.Add(8*time.Second))
		if refusal != nil {
			t.Fatal(refusal)
		}
		var record struct {
			Note string `json:"note"`
		}
		if err := json.Unmarshal(platform.Raw(view.Record), &record); err != nil || record.Note != note {
			t.Fatalf("writeback answer left %s as %+v; want note %q: %v", id, record, note, err)
		}
	}
	live.accept(t, "invariantproject.record.edit", "invariantproject.record", "G", map[string]any{"note": "edited after delivery"})
	again := effectFor("writeback/notify", "invariantproject.record/G")
	settleEffect(again, platform.Outcome{Effect: again.ID, Result: "delivered", Answer: platform.Raw(map[string]any{"message": "must not replace edited data"})}, invariantClock.Add(9*time.Second))
	view, refusal := live.RecordOf(t.Context(), live.member(t), "invariantproject.record", "G", invariantClock.Add(9*time.Second))
	if refusal != nil {
		t.Fatal(refusal)
	}
	var edited struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(platform.Raw(view.Record), &edited); err != nil || edited.Note != "edited after delivery" {
		t.Fatalf("a repeated writeback answer replaced later edits: %+v: %v", edited, err)
	}

	before := live.recordImage(t)
	replayed := live.restore(t, live.entries)
	if differing := differingTypes(before, replayed.recordImage(t)); len(differing) != 0 {
		t.Fatalf("data integration replay differs in %v", differing)
	}
}

func ptrExpr(expr platform.Expr) *platform.Expr { return &expr }
