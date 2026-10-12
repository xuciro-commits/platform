package platform

import "testing"

// A process flow reads one record's routing, pinned version and current step, and
// the steps of that routing in order (ADR-0098 D4): every binding is checked against
// the objects, not guessed from their field names.
func TestProcessFlowBindsDeclaredFieldsOnly(t *testing.T) {
	sfc := EntityInfo{App: "build", Type: "sfc", Fields: []FieldInfo{
		{Name: "routing", Type: "reference", Ref: "build.routing"},
		{Name: "routingversion", Type: "integer"},
		{Name: "operation", Type: "reference", Ref: "build.operation"},
		{Name: "ncs", Type: "lines"},
	}}
	operation := EntityInfo{App: "build", Type: "operation", Fields: []FieldInfo{
		{Name: "routing", Type: "reference", Ref: "build.routing"},
		{Name: "version", Type: "integer"},
		{Name: "number", Type: "integer"},
		{Name: "name", Type: "text"},
		{Name: "workcenter", Type: "reference", Ref: "build.workcenter"},
	}}
	entities := map[string]EntityInfo{"build.operation": operation}
	entity := func(typ string) (EntityInfo, bool) { e, ok := entities[typ]; return e, ok }
	valid := Section{ID: "flow", Widget: "process-flow", ConfigVersion: 1, ProcessFlow: &PageProcessFlow{
		StepObject: "build.operation", RoutingField: "routing", VersionField: "routingversion", StepVersionField: "version", StepField: "operation",
		OrderField: "number", TitleField: "name", LaneField: "workcenter", NotesField: "ncs"}}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"flow"}}, "flow": {Kind: "widget", Section: "flow"}}}
	if d.Check([]Section{valid}) != nil {
		t.Fatal("a process flow with declared bindings refused")
	}
	if err := valid.CheckProcessFlow(sfc, entity); err != nil {
		t.Fatalf("valid bindings refused: %v", err)
	}
	if err := valid.CheckProcessFlow(sfc, func(string) (EntityInfo, bool) { return EntityInfo{}, false }); err == nil {
		t.Fatal("a flow whose step object is not installed must be refused")
	}
	changes := map[string]func(*Section){
		"version is not an integer":      func(s *Section) { s.ProcessFlow.VersionField = "ncs" },
		"step field is not a step":       func(s *Section) { s.ProcessFlow.StepField = "routing" },
		"order is not a number":          func(s *Section) { s.ProcessFlow.OrderField = "name" },
		"step version is not an integer": func(s *Section) { s.ProcessFlow.StepVersionField = "name" },
		"steps do not hang from the routing": func(s *Section) {
			operation.Fields = []FieldInfo{{Name: "number", Type: "integer"}}
			entities["build.operation"] = operation
			s.ProcessFlow.LaneField = ""
		},
		"lane is not a reference": func(s *Section) { s.ProcessFlow.LaneField = "name" },
		"notes are not lines":     func(s *Section) { s.ProcessFlow.NotesField = "routingversion" },
		"title is unknown":        func(s *Section) { s.ProcessFlow.TitleField = "missing" },
	}
	for name, change := range changes {
		entities["build.operation"] = operation
		copy := valid
		value := *valid.ProcessFlow
		copy.ProcessFlow = &value
		change(&copy)
		if copy.CheckProcessFlow(sfc, entity) == nil {
			t.Fatalf("%s: accepted", name)
		}
		operation = entities["build.operation"]
	}
	// The profile and the document are part of the contract, not a hint.
	if d.Check([]Section{{ID: "flow", Widget: "detail", ConfigVersion: 1, ProcessFlow: valid.ProcessFlow}}) == nil {
		t.Fatal("a process flow configuration without its widget is accepted")
	}
	if (*PageDocument)(nil).Check([]Section{valid}) == nil {
		t.Fatal("a process flow without a document is accepted")
	}
	old := *d
	old.UIProfile = "platform.page.v2.109"
	if old.Check([]Section{valid}) == nil {
		t.Fatal("a process flow is accepted on a profile that predates it")
	}
}
