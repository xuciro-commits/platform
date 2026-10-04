package platformserver

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestDefaultActionRulesUseOriginalLedgerAndAtomicRelatedCreation(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	seats := []Seat{{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, {Subjects: []string{"user"}, Member: platform.Member{ID: "user", Roles: map[string]string{build.ID: build.User}}}}
	tn, err := NewTenant("rules", NewConsole("rules", seats...), build.New("rules"))
	if err != nil {
		t.Fatal(err)
	}
	var journal []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
	key := 0
	submit := func(actor, schema, typ, id string, payload any) error {
		key++
		m, _ := tn.Member(actor)
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{TenantId: "rules", PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return fmt.Errorf("%s: %s", schema, err.Message)
		}
		return nil
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	publish := func(id string, payload any) {
		must(submit("builder", build.ObjectType+".create", build.ObjectType, id, payload))
		must(submit("builder", build.SchemaPublish, build.ObjectType, id, map[string]any{}))
	}
	states := []map[string]any{{"name": "active", "title": "Active"}, {"name": "offline", "title": "Offline"}}
	publish("ASSET", map[string]any{"name": "ruleasset", "title": "Asset", "fields": []map[string]any{{"name": "priority", "title": "Priority", "type": "choice", "choices": "Low,High"}}, "states": states})
	publish("OPERATOR", map[string]any{"name": "ruleoperator", "title": "Operator", "fields": []map[string]any{{"name": "name", "title": "Name", "type": "text"}, {"name": "secret", "title": "Secret", "type": "text", "read": []string{build.Builder}}}})
	must(submit("builder", build.ObjectType+".edit", build.ObjectType, "ASSET", map[string]any{"fields": []map[string]any{{"name": "priority", "title": "Priority", "type": "choice", "choices": "Low,High"}, {"name": "operator", "title": "Operator", "type": "reference", "ref": "build.ruleoperator"}}}))
	must(submit("builder", build.SchemaPublish, build.ObjectType, "ASSET", map[string]any{}))
	publish("WORK", map[string]any{"name": "rulework", "title": "Work order", "fields": []map[string]any{{"name": "asset", "title": "Asset", "type": "reference", "ref": "build.ruleasset", "inverse": "work"}, {"name": "operator", "title": "Operator", "type": "reference", "ref": "build.ruleoperator", "required": true}, {"name": "title", "title": "Title", "type": "text", "required": true}}})
	actions := []map[string]any{{"name": "update", "title": "Update status", "from": []string{"active", "offline"}, "toInput": "status", "inputs": []map[string]any{{"name": "status", "title": "Status", "type": "choice", "choices": "active,offline", "required": true}, {"name": "priority", "title": "Priority", "type": "choice", "choices": "Low,High"}}, "sets": []map[string]any{{"field": "priority", "from": "priority"}}, "conditions": []map[string]any{{"when": map[string]any{"field": "input.status", "operator": "=", "value": "offline"}, "field": "input.priority", "operator": "=", "value": "High", "message": "Offline requires High"}}}, {"name": "schedule", "title": "Create work order", "from": []string{"active", "offline"}, "inputs": []map[string]any{{"name": "title", "title": "Title", "type": "text", "required": true, "minLength": 4}, {"name": "operator", "title": "Operator", "type": "reference", "ref": "build.ruleoperator", "required": true}}, "conditions": []map[string]any{{"field": "state", "operator": "!=", "value": "offline", "message": "Cannot schedule offline"}}, "creates": []map[string]any{{"object": "build.rulework", "via": "asset", "sets": []map[string]any{{"field": "title", "from": "title"}, {"field": "operator", "from": "operator"}}}}}}
	actions = append(actions, map[string]any{"name": "guard", "title": "Guard", "from": []string{"active", "offline"}, "inputs": []map[string]any{{"name": "priority", "title": "Priority", "type": "choice", "choices": "Low,High"}}, "conditions": []map[string]any{{"when": map[string]any{"field": "operator.secret", "operator": "=", "value": "allow"}, "field": "input.priority", "operator": "=", "value": "High", "message": "Guard requires High"}}})
	// A real reference input cannot change object identity on assignment into a child.
	badRaw, _ := json.Marshal(actions)
	var badActions []map[string]any
	json.Unmarshal(badRaw, &badActions)
	badActions[1]["inputs"].([]any)[1].(map[string]any)["ref"] = "build.ruleasset"
	must(submit("builder", build.ObjectType+".edit", build.ObjectType, "ASSET", map[string]any{"actions": badActions}))
	if submit("builder", build.SchemaPublish, build.ObjectType, "ASSET", map[string]any{}) == nil {
		t.Fatal("cross-object reference assignment published")
	}
	must(submit("builder", build.ObjectType+".edit", build.ObjectType, "ASSET", map[string]any{"actions": actions}))
	must(submit("builder", build.SchemaPublish, build.ObjectType, "ASSET", map[string]any{}))
	must(submit("builder", "build.ruleoperator.create", "build.ruleoperator", "OP", map[string]any{"name": "Original operator", "secret": "allow"}))
	must(submit("user", "build.ruleasset.create", "build.ruleasset", "A", map[string]any{"priority": "Low", "operator": "OP"}))
	if submit("user", "build.ruleasset.guard", "build.ruleasset", "A", map[string]any{"priority": "Low"}) == nil {
		t.Fatal("unreadable guard became false")
	}

	user, _ := tn.Member("user")
	state := func() string {
		view, err := tn.RecordOf(user, "build.ruleasset", "A", now)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view.Record)
		var record struct{ State string }
		json.Unmarshal(raw, &record)
		return record.State
	}
	count := func() int {
		page, err := tn.Records(user, "build.rulework", platform.Query{}, now)
		if err != nil {
			t.Fatal(err)
		}
		return page.Total
	}
	if submit("user", "build.ruleasset.update", "build.ruleasset", "A", map[string]any{"status": "offline", "priority": "Low"}) == nil || state() != "active" {
		t.Fatal("guard failed to refuse without mutation")
	}
	must(submit("user", "build.ruleasset.update", "build.ruleasset", "A", map[string]any{"status": "offline", "priority": "High"}))
	if state() != "offline" {
		t.Fatal("original lifecycle did not select the input state")
	}
	if submit("user", "build.ruleasset.schedule", "build.ruleasset", "A", map[string]any{"title": "Inspect", "operator": "OP"}) == nil || count() != 0 {
		t.Fatal("offline created a work order")
	}
	must(submit("user", "build.ruleasset.update", "build.ruleasset", "A", map[string]any{"status": "active", "priority": "Low"}))
	for _, payload := range []map[string]any{{"title": "abc", "operator": "OP"}, {"title": "Inspect", "operator": "missing"}} {
		if submit("user", "build.ruleasset.schedule", "build.ruleasset", "A", payload) == nil || count() != 0 {
			t.Fatal("invalid title/reference created a work order")
		}
	}
	must(submit("user", "build.ruleasset.schedule", "build.ruleasset", "A", map[string]any{"title": "Inspect", "operator": "OP"}))
	if count() != 1 {
		t.Fatal("expected one real related work order")
	}
	page, _ := tn.Records(user, "build.rulework", platform.Query{}, now)
	raw, _ := json.Marshal(page.Records[0])
	var work struct{ Asset, Operator, Title string }
	json.Unmarshal(raw, &work)
	if work.Asset != "A" || work.Operator != "OP" || work.Title != "Inspect" {
		t.Fatalf("lost original context: %s", raw)
	}
	restored, err := NewTenant("rules", NewConsole("rules", seats...), build.New("rules"))
	if err != nil {
		t.Fatal(err)
	}
	if replayErr := restored.Replay(journal); replayErr != nil {
		t.Fatal(replayErr)
	}
	CheckReplay(t, tn, journal, func() *Tenant {
		next, err := NewTenant("rules", NewConsole("rules", seats...), build.New("rules"))
		if err != nil {
			t.Fatal(err)
		}
		return next
	})
	replayed, readErr := restored.Records(user, "build.rulework", platform.Query{}, now)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if replayed.Total != 1 {
		t.Fatal("related result was not replayed")
	}
}
