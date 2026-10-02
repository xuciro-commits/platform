package platform

import "testing"

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
