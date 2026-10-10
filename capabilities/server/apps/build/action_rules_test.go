package build

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
	"time"
)

func actionRulesObject() Object {
	return Object{Name: "asset", States: []State{{Name: "active", Title: "Active"}, {Name: "offline", Title: "Offline"}}, Fields: []Field{{Name: "priority", Type: "choice", Choices: "Low,High"}, {Name: "operator", Type: "reference", Ref: "build.operator"}}, Actions: []Action{{Name: "update", Title: "Update", From: []string{"active", "offline"}, ToInput: "status", Inputs: []Input{{Name: "status", Title: "Status", Type: "choice", Choices: "active,offline", Required: true}, {Name: "priority", Title: "Priority", Type: "choice", Choices: "Low,High"}}, Sets: []Set{{Field: "priority", From: "priority"}}, Conditions: []Condition{{When: &Condition{Field: "input.status", Operator: "=", Value: "offline"}, Field: "input.priority", Operator: "=", Value: "High", Message: "Offline requires High"}}}}}
}
func TestActionRulesChooseOriginalStatesAndGuardConditionsBeforeMutation(t *testing.T) {
	o := actionRulesObject()
	if err := checkProcess(o, nil); err != nil {
		t.Fatal(err)
	}
	type record struct {
		platform.Record
		State    string
		Priority string
		Operator string
	}
	for _, tc := range []struct {
		payload string
		state   string
		ok      bool
	}{{`{"status":"active","priority":"Low"}`, "active", true}, {`{"status":"offline","priority":"Low"}`, "active", false}, {`{"status":"offline","priority":"High"}`, "offline", true}, {`{"status":"unknown","priority":"High"}`, "active", false}} {
		r := &record{State: "active", Priority: "Low"}
		err := take(o, o.Actions[0], platform.Caller{}, r, json.RawMessage(tc.payload), time.Now(), nil, nil)
		if (err == nil) != tc.ok || r.State != tc.state {
			t.Fatalf("%s: %+v %v", tc.payload, r, err)
		}
	}
	for _, edit := range []func(*Object){func(o *Object) { o.Actions[0].To = "offline" }, func(o *Object) { o.Actions[0].ToInput = "priority" }, func(o *Object) { o.Actions[0].Inputs[0].Required = false }, func(o *Object) { o.Actions[0].Conditions[0].When.When = &Condition{} }, func(o *Object) { o.Actions[0].Conditions[0].When.Field = "input.missing" }, func(o *Object) { o.Actions[0].Approval = &ActionApproval{} }} {
		o := actionRulesObject()
		edit(&o)
		if checkProcess(o, nil) == nil {
			t.Fatal("invalid state or guard accepted")
		}
	}
}
func TestOriginalReferenceInputsAndUTF16MinimumHaveBoundedDeclarations(t *testing.T) {
	min := 4
	o := actionRulesObject()
	o.Actions[0].ToInput = ""
	o.Actions[0].Inputs = []Input{{Name: "operator", Title: "Operator", Type: "reference", Ref: "build.operator", Required: true}, {Name: "title", Title: "Title", Type: "text", MinLength: &min}}
	o.Actions[0].Sets = []Set{{Field: "operator", From: "operator"}}
	o.Actions[0].Conditions = nil
	lookup := func(name string) (platform.EntityInfo, bool) {
		return platform.EntityInfo{App: "build", Type: name}, name == "build.operator"
	}
	if err := checkProcess(o, lookup); err != nil {
		t.Fatal(err)
	}
	descriptor := lifecycle(o, []string{User}, nil, lookup)
	if descriptor.Transitions[0].Payload[0].Ref != "build.operator" || descriptor.Transitions[0].Payload[1].Constraints == nil || *descriptor.Transitions[0].Payload[1].Constraints.MinLength != min {
		t.Fatal("reference became an ordinary text parameter")
	}
	for _, input := range []struct {
		title string
		ok    bool
	}{{"abc", false}, {"abcd", true}, {"", true}, {"\U0001f600\U0001f600", true}} {
		r := &struct {
			platform.Record
			State    string
			Priority string
			Operator string
		}{State: "active"}
		raw, _ := json.Marshal(map[string]any{"operator": "OP", "title": input.title})
		err := take(o, o.Actions[0], platform.Caller{}, r, raw, time.Now(), nil, lookup)
		if (err == nil) != input.ok {
			t.Fatalf("title %q: %v", input.title, err)
		}
	}
	for _, edit := range []func(*Object){func(o *Object) { o.Actions[0].Inputs[0].Ref = "build.other" }, func(o *Object) { o.Actions[0].Inputs[0].Type = "text" }, func(o *Object) { o.Actions[0].Inputs[1].Type = "boolean" }, func(o *Object) { bad := 4097; o.Actions[0].Inputs[1].MinLength = &bad }} {
		raw, _ := json.Marshal(o)
		var copy Object
		json.Unmarshal(raw, &copy)
		edit(&copy)
		if checkProcess(copy, lookup) == nil {
			t.Fatal("incompatible reference or length accepted")
		}
	}
}
