package platformserver

import (
	"encoding/json"
	"errors"

	"fmt"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A tenant-authored step uses the work app's approval and inbox, including
// rejection, rather than a second process runtime (ADR-0037 18c).
func TestTenantDefinedApproval(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprint("accepted=", accepted), func(t *testing.T) {
			testTenantDefinedApproval(t, accepted)
		})
	}
}

func testTenantDefinedApproval(t *testing.T, accepted bool) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("dana", build.Builder), seat("eli", build.User), seat("sam", "manager")),
			work.New("t-1"), build.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	fail := false
	if accepted {
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			if fail {
				return nil, errors.New("injected append failure")
			}
			journal = append(journal, e)
			return e.Body, nil
		}
	}
	var retried []*pb.Submission
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	keys := 0
	do := func(who, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		authority := build.ID
		if typ == work.ApprovalType {
			authority = work.ID
		}
		sub := &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("ap", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
		if accepted {
			before := snapshot(tn)
			fail = true
			if _, err := tn.Submit(member(who), sub, now); err == nil {
				t.Fatal("failed append accepted")
			}
			fail = false
			if snapshot(tn) != before {
				t.Fatal("failed append leaked approval, task, publication or notification state")
			}
		}
		receipt, err := tn.Submit(member(who), sub, now)
		if err != nil {
			return fmt.Sprintf("%s: %s", err.Code, err.Message)
		}
		if accepted {
			count, before := len(journal), snapshot(tn)
			again, err := tn.Submit(member(who), sub, now.Add(time.Hour))
			if err != nil || !proto.Equal(receipt, again) || len(journal) != count || snapshot(tn) != before {
				t.Fatalf("retry changed the accepted answer or state: %v", err)
			}
			retried = append(retried, proto.Clone(sub).(*pb.Submission))
		}
		return "ok"
	}
	states := []map[string]any{{"name": "found", "title": "Found"}, {"name": "pending", "title": "Pending"},
		{"name": "returned", "title": "Returned"}, {"name": "rejected", "title": "Rejected"}}
	action := map[string]any{"name": "handback", "title": "Hand it back", "from": []string{"found"}, "to": "returned", "roles": []string{"user"},
		"inputs":   []map[string]any{{"name": "to", "title": "Handed to", "type": "text", "required": true}},
		"sets":     []map[string]any{{"field": "claimant", "from": "to"}},
		"approval": map[string]any{"pending": "pending", "rejected": "rejected", "levels": []map[string]any{{"title": "Manager", "role": "manager"}, {"title": "Builder", "role": "builder"}}}}
	object := map[string]any{"name": "lost", "title": "Lost item", "fields": []map[string]any{{"name": "item", "title": "Item", "type": "text"}, {"name": "claimant", "title": "Claimant", "type": "text"}},
		"states": states, "actions": []any{action}, "access": []map[string]any{{"role": "user", "read": "own", "create": true}, {"role": "manager", "read": "all"}}}
	for _, x := range []struct {
		action map[string]any
		want   string
	}{
		{merge(action, map[string]any{"from": []string{"found", "rejected"}}), "exactly one starting state"},
		{merge(action, map[string]any{"approval": map[string]any{"pending": "missing", "levels": []map[string]any{{"title": "Manager", "role": "manager"}}}}), "must be a separate state"},
		{merge(action, map[string]any{"approval": map[string]any{"pending": "pending", "levels": []any{}}}), "needs an approver level"},
		{merge(action, map[string]any{"approval": map[string]any{"pending": "pending", "rejected": "pending", "levels": []map[string]any{{"title": "Manager", "role": "manager"}}}}), "other than pending"},
		{merge(action, map[string]any{"approval": map[string]any{"pending": "pending", "levels": []map[string]any{{"title": "Clerk", "role": "user"}}}}), "cannot read every record"},
	} {
		id := fmt.Sprintf("bad-%d", keys)
		bad := merge(object, map[string]any{"name": fmt.Sprintf("bad%d", keys), "actions": []any{x.action}})
		if got := do("dana", build.ObjectType+".create", build.ObjectType, id, bad); got != "ok" {
			t.Fatalf("draft: %s", got)
		}
		if got := do("dana", build.SchemaPublish, build.ObjectType, id, map[string]any{}); !strings.Contains(got, x.want) {
			t.Errorf("invalid approval: %s, want %q", got, x.want)
		}
	}
	unknownRole := merge(object, map[string]any{"name": "unknownapprover", "access": []any{},
		"actions": []any{merge(action, map[string]any{"approval": map[string]any{"pending": "pending", "levels": []map[string]any{{"title": "Unknown", "role": "unknown"}}}})}})
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-unknown", unknownRole); got != "ok" {
		t.Fatal(got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-unknown", map[string]any{}); !strings.Contains(got, "cannot read every record") {
		t.Fatalf("unknown approver role: %s", got)
	}
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1", object); got != "ok" {
		t.Fatal(got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatal(got)
	}
	lost := build.TypeOf("lost")
	declared, found := tn.app(build.ID).Manifest().Actions.Action(lost + ".handback")
	if !found || !declared.NeedsApproval {
		t.Fatal("the action is not offered as an approval")
	}
	state := func(id string) (string, string) {
		view, err := tn.RecordOf(member("dana"), lost, id, now)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view.Record)
		var r struct{ State, Claimant string }
		json.Unmarshal(raw, &r)
		return r.State, r.Claimant
	}
	request := func() work.ApprovalRequest {
		out, _ := tn.Read(member("eli"), "requests")
		for _, r := range out.([]work.ApprovalRequest) {
			if r.State == "pending" {
				return r
			}
		}
		t.Fatal("no pending request")
		return work.ApprovalRequest{}
	}
	for _, id := range []string{"L-1", "L-2"} {
		if got := do("eli", lost+".create", lost, id, map[string]any{"item": id}); got != "ok" {
			t.Fatal(got)
		}
	}
	if got := do("eli", lost+".handback", lost, "L-1", map[string]any{"to": "Ada"}); got != "ok" {
		t.Fatal(got)
	}
	if s, claimant := state("L-1"); s != "pending" || claimant != "" {
		t.Fatalf("before approval: %s %s", s, claimant)
	}
	a := request()
	if len(a.Levels) != 2 || fmt.Sprint(a.Levels[0].Approvers) != "[sam]" || fmt.Sprint(a.Levels[1].Approvers) != "[dana]" {
		t.Fatalf("approvers: %+v", a.Levels)
	}
	if out, _ := tn.Read(member("sam"), "inbox"); len(out.([]work.WorkTask)) != 1 {
		t.Fatalf("manager inbox: %+v", out)
	}
	if got := do("sam", work.ApprovalType+".approve", work.ApprovalType, a.ID, map[string]any{}); got != "ok" {
		t.Fatal(got)
	}
	if s, claimant := state("L-1"); s != "pending" || claimant != "" {
		t.Fatalf("after first level: %s %s", s, claimant)
	}
	if got := do("dana", work.ApprovalType+".approve", work.ApprovalType, a.ID, map[string]any{}); got != "ok" {
		t.Fatal(got)
	}
	if s, claimant := state("L-1"); s != "returned" || claimant != "Ada" {
		t.Fatalf("after approval: %s %s", s, claimant)
	}
	if got := do("eli", lost+".handback", lost, "L-2", map[string]any{"to": "Bob"}); got != "ok" {
		t.Fatal(got)
	}
	a = request()
	if got := do("sam", work.ApprovalType+".reject", work.ApprovalType, a.ID, map[string]any{"note": "Not the claimant"}); got != "ok" {
		t.Fatal(got)
	}
	if s, claimant := state("L-2"); s != "rejected" || claimant != "" {
		t.Fatalf("after rejection: %s %s", s, claimant)
	}
	CheckReplay(t, tn, journal, compose)
	if accepted {
		restored := compose()
		if err := restored.Replay(journal); err != nil {
			t.Fatal(err)
		}
		restored.AcceptResult = func(Entry, string, string) ([]byte, error) {
			t.Fatal("recovered retry appended a result")
			return nil, nil
		}
		for _, sub := range retried {
			original, err := tn.Submit(member(sub.GetPrincipalId()), sub, now.Add(2*time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := restored.Submit(member(sub.GetPrincipalId()), sub, now.Add(3*time.Hour))
			if err != nil || !proto.Equal(original, recovered) {
				t.Fatalf("recovery changed answer: %v", err)
			}
		}
	}
}
