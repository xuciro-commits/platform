package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Holding and its deadline remain ordinary lifecycle + Process/Work behavior.
// ADR-0055 introduces no exception scheduler or separate adjudication engine.
func TestBusinessRefusalHoldDeadlineAndDecisionReturnToSubject(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "refusal-hold", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("manager", "build:manager")}, work.New("refusal-hold"), flow.New("refusal-hold"), build.New("refusal-hold"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	publishObject(t, tn, "builder", "CASE", map[string]any{
		"name": "case", "title": "Case", "plural": "Cases",
		"fields": []construction.Field{{Name: "reason", Title: "Reason", Type: "text"}},
		"states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "hold", Title: "Hold"}, {Name: "review", Title: "Awaiting review"}, {Name: "ready", Title: "Ready"}},
		"access": []construction.Access{{Role: "operator", Read: "all", Create: true}, {Role: "manager", Read: "all"}},
		"actions": []construction.Action{
			{Name: "hold", Title: "Hold", From: []string{"draft"}, To: "hold", Roles: []string{"operator"}},
			{Name: "resolve", Title: "Resolve hold", From: []string{"hold"}, To: "ready", Roles: []string{"operator"}, Approval: &construction.ActionApproval{Pending: "review", Levels: []construction.ApproverLevel{{Title: "Review expired hold", Role: "manager"}}}},
		},
	}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.ProcessType+".create", construction.ProcessType, "HOLD", map[string]any{
		"name": "holdreview", "title": "Review expired hold", "object": "build.case", "when": "hold",
		"steps": []construction.ProcessStep{
			{Name: "deadline", Kind: "wait", UntilSeconds: 60, Next: "review"},
			{Name: "review", Kind: "action", Act: "resolve"},
		},
	}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.SchemaProcess, construction.ProcessType, "HOLD", map[string]any{}, txnAt)
	decide(t, tn, "operator", construction.ID, "build.case.create", "build.case", "C1", map[string]any{}, txnAt)
	decide(t, tn, "operator", construction.ID, "build.case.hold", "build.case", "C1", map[string]any{}, txnAt)
	tn.Work(txnAt)
	tn.Work(txnAt.Add(59 * time.Second))
	if tn.records.types[work.TaskType].rows.Len() != 0 {
		t.Fatal("a hold escalated before its declared deadline")
	}
	tn.Work(txnAt.Add(61 * time.Second))
	requests, _, err := platform.Find[work.ApprovalRequest](tn.automation(work.ID, false), platform.Query{})
	if err != nil || len(requests) != 1 || requests[0].State != "pending" || requests[0].Target != "build.case/C1" {
		t.Fatalf("expired hold did not reach ordinary human work: %+v %v", requests, err)
	}
	view, err := tn.RecordOf(memberOf(t, tn, "manager"), "build.case", "C1", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	var subject map[string]any
	if json.Unmarshal(platform.Raw(view.Record), &subject) != nil || subject["state"] != "review" {
		t.Fatal("the expired source hold did not expose its pending human disposition")
	}
	decide(t, tn, "manager", work.ID, work.ApprovalType+".approve", work.ApprovalType, requests[0].ID, map[string]any{"note": "Evidence checked"}, txnAt.Add(62*time.Second))
	view, err = tn.RecordOf(memberOf(t, tn, "manager"), "build.case", "C1", txnAt.Add(62*time.Second))
	if err != nil || json.Unmarshal(platform.Raw(view.Record), &subject) != nil || subject["state"] != "ready" {
		t.Fatalf("human disposition did not return to the business subject: %+v %v", subject, err)
	}
	CheckReplay(t, tn, entries, compose)
}
