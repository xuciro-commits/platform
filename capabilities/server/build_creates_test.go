package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A tenant action that creates a related record commits both in one accepted
// result, or neither (ADR-0040 21c D2); another app's object is refused.
func TestActionCreatesRelatedRecordAtomically(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
		tn, err := NewTenant("cr", NewConsole("cr", seat), build.New("cr"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		journal = append(journal, e)
		return e.Body, nil
	}
	dana, _ := tn.Member("dana")
	keys := 0
	do := func(schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(dana, &pb.Submission{TenantId: "cr", PrincipalId: "dana", Authority: build.ID,
			IdempotencyKey: fmt.Sprint("k", keys), Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Code.String() + ": " + err.Message
		}
		return "ok"
	}
	must := func(got string) {
		t.Helper()
		if got != "ok" {
			t.Fatal(got)
		}
	}
	visit, followup := build.TypeOf("visit"), build.TypeOf("followup")
	states := []map[string]any{{"name": "open", "title": "Open"}, {"name": "done", "title": "Done"}}
	must(do(build.ObjectType+".create", build.ObjectType, "O-V", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}, "states": states}))
	must(do(build.SchemaPublish, build.ObjectType, "O-V", map[string]any{}))
	must(do(build.ObjectType+".create", build.ObjectType, "O-F", map[string]any{"name": "followup", "title": "Follow-up",
		"fields": []map[string]any{{"name": "visit", "title": "Visit", "type": "reference", "ref": visit, "inverse": "followups"},
			{"name": "note", "title": "Note", "type": "text", "required": true}}}))
	must(do(build.SchemaPublish, build.ObjectType, "O-F", map[string]any{}))

	close := func(sets []map[string]any) map[string]any {
		return map[string]any{"name": "visit", "title": "Visit", "states": states,
			"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}},
			"actions": []map[string]any{{"name": "close", "title": "Close", "from": []string{"open"}, "to": "done",
				"inputs":  []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
				"creates": []map[string]any{{"object": followup, "via": "visit", "sets": sets}}}}}
	}
	// Another app's object, or a Via that is not its reference here, is refused at publish.
	bad := close(nil)
	bad["actions"].([]map[string]any)[0]["creates"] = []map[string]any{{"object": "crm.task", "via": "visit"}}
	must(do(build.ObjectType+".edit", build.ObjectType, "O-V", bad))
	if got := do(build.SchemaPublish, build.ObjectType, "O-V", map[string]any{}); got == "ok" {
		t.Fatal("an action creating another app's record was published")
	}
	must(do(build.ObjectType+".edit", build.ObjectType, "O-V", close([]map[string]any{{"field": "note", "from": "note"}})))
	must(do(build.SchemaPublish, build.ObjectType, "O-V", map[string]any{}))

	must(do(visit+".create", visit, "V-1", map[string]any{"guest": "Ada"}))
	count := func() int {
		page, err := tn.Records(dana, followup, platform.Query{}, now)
		if err != nil {
			t.Fatal(err)
		}
		return page.Total
	}
	stateOf := func(id string) string {
		view, err := tn.RecordOf(dana, visit, id, now)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(view.Record)
		var r struct{ State string }
		json.Unmarshal(raw, &r)
		return r.State
	}
	// The follow-up's required note is missing: the whole action is refused.
	if got := do(visit+".close", visit, "V-1", map[string]any{}); got == "ok" || stateOf("V-1") != "open" || count() != 0 {
		t.Fatalf("a refused related create left a partial result: %s, %s, %d", got, stateOf("V-1"), count())
	}
	// A failed append exposes neither change.
	fail = true
	if got := do(visit+".close", visit, "V-1", map[string]any{"note": "Call back"}); got == "ok" || stateOf("V-1") != "open" || count() != 0 {
		t.Fatalf("failed append exposed a change: %s", got)
	}
	fail = false
	must(do(visit+".close", visit, "V-1", map[string]any{"note": "Call back"}))
	if stateOf("V-1") != "done" || count() != 1 {
		t.Fatalf("action and related create did not commit together: %s, %d", stateOf("V-1"), count())
	}
	view, _ := tn.RecordOf(dana, visit, "V-1", now)
	if len(view.Related) != 1 || view.Related[0].Relation != "followups" || view.Related[0].Total != 1 {
		t.Fatalf("the follow-up is not related to its visit: %+v", view.Related)
	}
	CheckReplay(t, tn, journal, compose)
}
