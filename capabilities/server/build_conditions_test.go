package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Cross-record submission rules belong to the original action, including its
// approval retry. A page or a caller cannot supply the compared source value.
func TestCrossRecordActionConditions(t *testing.T) {
	const tenant = "related-rules"
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant(tenant, NewConsole(tenant, seat("builder", build.Builder), seat("user", build.User)), work.New(tenant), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var journal []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
	builder, _ := tn.Member("builder")
	user, _ := tn.Member("user")
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	key := 0
	submit := func(member platform.Member, typ, id, verb string, payload any) string {
		t.Helper()
		key++
		_, err := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: strings.Split(typ, ".")[0], IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			return err.Message
		}
		return "ok"
	}
	must := func(member platform.Member, typ, id, verb string, payload any) {
		t.Helper()
		if got := submit(member, typ, id, verb, payload); got != "ok" {
			t.Fatal(got)
		}
	}
	install := func(id string, payload any) {
		t.Helper()
		must(builder, build.ObjectType, id, "create", payload)
		must(builder, build.ObjectType, id, "publish", map[string]any{})
	}
	install("R", map[string]any{"name": "receipt", "title": "Receipt", "fields": []build.Field{{Name: "number", Title: "Number", Type: "text"}}, "states": []build.State{{Name: "receiving", Title: "Receiving"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"receiving"}, To: "done"}}})
	install("L", map[string]any{"name": "line", "title": "Line", "fields": []build.Field{{Name: "receipt", Title: "Receipt", Type: "reference", Ref: "build.receipt", Required: true}, {Name: "expected", Title: "Expected", Type: "integer", Required: true}}})
	rules := []build.Condition{{Field: "line.receipt.state", Operator: "=", Value: "receiving", Message: "Parent must be receiving"}, {Field: "quantity", Operator: "<=", ValueField: "line.expected", Message: "Too much for this line"}}
	action := build.Action{Name: "start", Title: "Submit", From: []string{"open"}, To: "ready", Conditions: rules, Sets: []build.Set{{Field: "submittedby", From: "$me"}}}
	confirm := build.Action{Name: "confirm", Title: "Confirm", From: []string{"ready"}, To: "done", Conditions: rules, Sets: []build.Set{{Field: "completedby", From: "$me"}}, Approval: &build.ActionApproval{Pending: "pending", Levels: []build.ApproverLevel{{Title: "Supervisor", Role: build.Builder}}}}
	fields := []build.Field{{Name: "line", Title: "Line", Type: "reference", Ref: "build.line", Required: true}, {Name: "quantity", Title: "Quantity", Type: "integer", Required: true}, {Name: "submittedby", Title: "Submitted by", Type: "text"}, {Name: "completedby", Title: "Completed by", Type: "text"}}
	states := []build.State{{Name: "open", Title: "Open"}, {Name: "ready", Title: "Ready"}, {Name: "pending", Title: "Pending"}, {Name: "done", Title: "Done"}}
	install("T", map[string]any{"name": "task", "title": "Task", "fields": fields, "states": states, "actions": []build.Action{action, confirm}})
	// Publication rejects undeclared hops and nonnumeric field comparisons.
	bad := action
	bad.Conditions = []build.Condition{{Field: "quantity", Operator: "<=", ValueField: "line.receipt.number", Message: "Bad types"}}
	must(builder, build.ObjectType, "T", "edit", map[string]any{"actions": []build.Action{bad, confirm}})
	if got := submit(builder, build.ObjectType, "T", "publish", map[string]any{}); !strings.Contains(got, "incompatible") {
		t.Fatalf("type check: %s", got)
	}
	bad.Conditions = []build.Condition{{Field: "line.expected.state", Operator: "=", Value: "done", Message: "Bad path"}}
	must(builder, build.ObjectType, "T", "edit", map[string]any{"actions": []build.Action{bad, confirm}})
	if got := submit(builder, build.ObjectType, "T", "publish", map[string]any{}); !strings.Contains(got, "not a single-record reference") {
		t.Fatalf("path check: %s", got)
	}
	must(builder, build.ObjectType, "T", "edit", map[string]any{"actions": []build.Action{action, confirm}})
	must(builder, build.ObjectType, "T", "publish", map[string]any{})
	for _, id := range []string{"R1", "R2"} {
		must(builder, "build.receipt", id, "create", map[string]any{"number": id})
		must(builder, "build.line", "L"+id, "create", map[string]any{"receipt": id, "expected": 125})
	}
	makeTask := func(id, line string, qty int) {
		t.Helper()
		must(user, "build.task", id, "create", map[string]any{"line": line, "quantity": qty})
	}
	row := func(id string) map[string]any {
		t.Helper()
		view, err := tn.RecordOf(builder, "build.task", id, at)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view.Record)
		var values map[string]any
		_ = json.Unmarshal(raw, &values)
		return values
	}
	businessState := func() string {
		image, _, err := tn.Snapshot(func() int64 { return 0 })
		if err != nil {
			t.Fatal(err)
		}
		var state tenantState
		if err := json.Unmarshal(image, &state); err != nil {
			t.Fatal(err)
		}
		// Refusal audit/idempotency records are expected; business rows,
		// approvals and queued work must remain unchanged.
		raw, _ := json.Marshal([]any{state.Records, state.Works, state.Tasks, state.Queues, state.Jobs, state.Notices, state.Sequences})
		return string(raw)
	}
	refused := func(id, want string) {
		t.Helper()
		before := businessState()
		if got := submit(user, "build.task", id, "start", map[string]any{}); !strings.Contains(got, want) {
			t.Fatalf("refusal: %s, want %s", got, want)
		}
		if businessState() != before {
			t.Fatal("refused action changed business or runtime state")
		}
	}
	makeTask("OVER", "LR1", 126)
	refused("OVER", "Too much")
	makeTask("PARTIAL", "LR1", 75)
	must(user, "build.task", "PARTIAL", "start", map[string]any{})
	must(user, "build.task", "PARTIAL", "confirm", map[string]any{})
	pending := func(target string) work.ApprovalRequest {
		t.Helper()
		out, _ := tn.Read(user, "requests")
		for _, request := range out.([]work.ApprovalRequest) {
			if request.Target == "build.task/"+target && request.State == "pending" {
				return request
			}
		}
		t.Fatal("no approval request")
		return work.ApprovalRequest{}
	}
	request := pending("PARTIAL")
	must(builder, work.ApprovalType, request.ID, "approve", map[string]any{})
	if got := row("PARTIAL"); got["state"] != "done" || got["completedby"] != "user" {
		t.Fatalf("normal approval: %v", got)
	}
	makeTask("WAIT", "LR2", 75)
	must(user, "build.task", "WAIT", "start", map[string]any{})
	must(user, "build.task", "WAIT", "confirm", map[string]any{})
	request = pending("WAIT")
	must(builder, "build.receipt", "R2", "close", map[string]any{})
	must(builder, work.ApprovalType, request.ID, "approve", map[string]any{})
	if got := row("WAIT"); got["state"] != "ready" || got["completedby"] != nil && got["completedby"] != "" {
		t.Fatalf("closed parent passed approval: %v", got)
	}
	makeTask("CLOSED", "LR2", 50)
	refused("CLOSED", "Parent must be receiving")
	CheckReplay(t, tn, journal, compose)
	// A still-readable parent must not allow a private leaf to control a write.
	tn.records.types["build.line"].info.Fields[1].Read = []string{build.Builder}
	makeTask("PRIVATE", "LR1", 50)
	refused("PRIVATE", "unavailable")
	if got := row("PRIVATE"); got["state"] != "open" || got["submittedby"] != nil && got["submittedby"] != "" {
		t.Fatalf("private source left a mutation: %v", got)
	}
}
