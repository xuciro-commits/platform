package platform

import (
	"encoding/json"
	"platformkernel/kernel"
	"reflect"
	"testing"
	"time"
)

func TestKanbanUsesOriginalStatusFieldsAndTransitions(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.task", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "state", Type: "choice"}, {Name: "qty", Type: "integer"}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []TransitionInfo{{Schema: "sample.task.close", From: []string{"open"}, To: []string{"done"}}}}}
	s := Section{Widget: "kanban", CollectionVariable: "window", CardLabel: "title", Fields: []string{"qty"}, Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.task.close"}}}
	if err := s.CheckKanban(info); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []func(*Section){func(s *Section) { s.CardLabel = "qty" }, func(s *Section) { s.Fields = []string{"private"} }, func(s *Section) { s.CollectionVariable = "" }, func(s *Section) { s.Actions[0] = AssetRef{App: "sample", Kind: AssetAction, Name: "sample.task.edit"} }} {
		bad := s
		bad.Actions = append([]AssetRef(nil), s.Actions...)
		patch(&bad)
		if bad.CheckKanban(info) == nil {
			t.Fatal("accepted invalid kanban", bad)
		}
	}
	hidden := info
	hidden.Fields = hidden.Fields[:1]
	if s.CheckKanban(hidden) == nil {
		t.Fatal("hidden status produced a board")
	}
	info.Lifecycle.Transitions[0].To = []string{"open", "done"}
	if s.CheckKanban(info) == nil {
		t.Fatal("ambiguous native destination became a move")
	}
}

func TestParameterizedKanbanUsesDeclaredDestinationAndAdmittedProfile(t *testing.T) {
	type task struct {
		Record
		State string `json:"state" field:"readonly"`
	}
	transition := Transition{Name: "update", From: []string{"open", "done"}, To: []string{"open", "done"}, ToInput: "status", Payload: []Field{{Name: "status", Type: "string", Required: true, Choices: []string{"open", "done"}}}, Do: func(Caller, any, json.RawMessage, time.Time) *kernel.Error { return nil }}
	e := Entity{Type: "sample.task", Model: task{}, Lifecycle: &Lifecycle{Field: "state", Initial: "open", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []Transition{transition}}}
	info, err := Describe("sample", e, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if info.Lifecycle.Transitions[0].ToInput != "status" {
		t.Fatal("lost original destination input")
	}
	s := Section{Widget: "kanban", CollectionVariable: "window", CardLabel: "id", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.task.update"}}}
	p := Page{Document: &PageDocument{UIProfile: PageUIProfile()}}
	if err := p.CheckKanban(s, info); err != nil {
		t.Fatal(err)
	}
	p.Document.UIProfile = "platform.page.v2.98"
	if p.CheckKanban(s, info) == nil {
		t.Fatal("old profile admitted dynamic gesture")
	}
	for _, change := range []func(*Transition){func(t *Transition) { t.ToInput = "missing" }, func(t *Transition) { t.Payload[0].Required = false }, func(t *Transition) { t.Payload[0].Choices = []string{"open", "fake"} }, func(t *Transition) { t.Do = nil }} {
		bad := transition
		bad.Payload = append([]Field(nil), transition.Payload...)
		change(&bad)
		e.Lifecycle.Transitions = []Transition{bad}
		if _, err := Describe("sample", e, func(reflect.Type) string { return "" }); err == nil {
			t.Fatal("accepted false destination description")
		}
	}
}

func TestFrozenParameterizedLifecycleRetainsItsOriginalDestination(t *testing.T) {
	info, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"},{"name":"done"}],"actions":[{"name":"update","from":["open","done"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"open,done"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	move := info.Lifecycle.Transitions[0]
	if move.ToInput != "status" || len(move.To) != 2 {
		t.Fatal("frozen descriptor lost state input", move)
	}
	if _, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"}],"actions":[{"name":"update","from":["open"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"fake"}]}]}`)); err == nil {
		t.Fatal("frozen destination escaped original states")
	}
}
