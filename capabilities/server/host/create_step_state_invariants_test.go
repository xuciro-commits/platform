package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK29CreateStepsJudgeStateConditionsByTheInitialState(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k29")
	const ticket, request = "invariantproject.ticket", "invariantproject.request"
	text := func(value string) *platform.Expr {
		return &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw(value)}}
	}
	done := &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("done")}}
	live.accept(t, "build.object.create", build.ObjectType, "TICKET", map[string]any{"name": "ticket", "title": "Ticket",
		"fields": []ontology.Field{{Name: "note", Title: "Note", Type: "text", When: "state=open"}, {Name: "memo", Title: "Memo", Type: "text", When: "state=closed"}},
		"states": []ontology.State{{Name: "open", Title: "Open"}, {Name: "closed", Title: "Closed"}}})
	live.accept(t, "build.object.publish", build.ObjectType, "TICKET", nil)
	live.accept(t, "build.object.create", build.ObjectType, "REQUEST", map[string]any{"name": "request", "title": "Request",
		"fields": []ontology.Field{{Name: "title", Title: "Title", Type: "text"}},
		"states": []ontology.State{{Name: "new", Title: "New"}, {Name: "done", Title: "Done"}}})
	raise := func(name, field string) ontology.Action {
		return ontology.Action{Name: name, Title: "Raise", From: []string{"new"}, Roles: []string{ontology.BuilderRole},
			Transaction: &platform.BusinessTxn{Subject: request, Steps: []platform.TxnStep{
				{Name: "ticket", Kind: "create", Target: platform.TxnRef{Type: ticket}, Changes: []platform.TxnSet{{Field: field, Expr: text("raised")}}},
				{Name: "done", Kind: "update", Target: platform.TxnRef{Type: request, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: done}}},
			}}}
	}
	for id, action := range map[string]ontology.Action{"RAISE": raise("raise", "note"), "MISFILE": raise("misfile", "memo")} {
		live.accept(t, "build.actiontype.create", build.ActionTypeType, id, map[string]any{"name": request + "." + action.Name, "object": request, "definition": action})
		live.accept(t, "build.actiontype.publish", build.ActionTypeType, id, map[string]any{})
	}
	live.accept(t, "build.object.publish", build.ObjectType, "REQUEST", map[string]any{})

	live.accept(t, request+".create", request, "R1", map[string]any{"title": "Printer"})
	raised := live.accept(t, request+".raise", request, "R1", map[string]any{})
	view, refusal := live.RecordOf(t.Context(), live.member(t), ticket, raised.GetChangeId()+"-ticket", invariantClock)
	if refusal != nil {
		t.Fatalf("the created ticket: %s", refusal.Message)
	}
	var created struct {
		Note  string `json:"note"`
		State string `json:"state"`
	}
	_ = json.Unmarshal(platform.Raw(view.Record), &created)
	if created.Note != "raised" || created.State != "open" {
		t.Fatalf("a create step must write a field that applies in the initial state: %+v", created)
	}

	live.accept(t, request+".create", request, "R2", map[string]any{"title": "Scanner"})
	_, refusal = live.Submit(live.member(t), live.submission(request+".misfile", request, "R2", map[string]any{}), invariantClock)
	if refusal == nil || !strings.Contains(refusal.Message, "applies only when") {
		t.Fatalf("a create step writing a field of another state must still be refused, got %#v", refusal)
	}

	before := live.recordImage(t)
	if after := live.restore(t, live.entries).recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must reproduce the created record")
	}
}
