package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"platformserver/platform"
	"reflect"
	"slices"
	"testing"
	"time"
)

func actionRulesObject() construction.Object {
	return construction.Object{Name: "asset", States: []construction.State{{Name: "active", Title: "Active"}, {Name: "offline", Title: "Offline"}}, Fields: []construction.Field{{Name: "priority", Type: "choice", Choices: "Low,High"}, {Name: "operator", Type: "reference", Ref: "build.operator"}}, Actions: []construction.Action{{Name: "update", Title: "Update", From: []string{"active", "offline"}, ToInput: "status", Inputs: []construction.Input{{Name: "status", Title: "Status", Type: "choice", Choices: "active,offline", Required: true}, {Name: "priority", Title: "Priority", Type: "choice", Choices: "Low,High"}}, Sets: []construction.Set{{Field: "priority", From: "priority"}}, Conditions: []construction.Condition{{When: &construction.Condition{Field: "input.status", Operator: "=", Value: "offline"}, Field: "input.priority", Operator: "=", Value: "High", Message: "Offline requires High"}}}}}
}

func TestCanonicalActionLifecycleRetainsTransactionDestinations(t *testing.T) {
	for _, tc := range []struct {
		name, to, input string
		want            []string
	}{
		{"input", "", "status", []string{"active", "offline"}},
		{"fixed", "offline", "", []string{"offline"}},
		{"unchanged", "", "", []string{"active", "offline"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := actionRulesObject()
			o.Actions[0].To, o.Actions[0].ToInput = tc.to, tc.input
			b := New("destinations")
			canonical, err := b.CanonicalObject(o)
			if err != nil {
				t.Fatal(err)
			}
			if canonical.Actions[0].To != "" || canonical.Actions[0].ToInput != "" {
				t.Fatal("canonical action retained a second state rule")
			}
			for _, object := range []construction.Object{o, canonical} {
				info, err := platform.Describe(construction.ID, b.entity(object), func(reflect.Type) string { return "" })
				if err != nil {
					t.Fatal(err)
				}
				move := info.Lifecycle.Transitions[0]
				if move.ToInput != tc.input || !slices.Equal(move.To, tc.want) {
					t.Fatalf("destination = %v from %q; want %v from %q", move.To, move.ToInput, tc.want, tc.input)
				}
			}
		})
	}
}

func TestActionRulesChooseOriginalStatesAndGuardConditionsBeforeMutation(t *testing.T) {
	o := actionRulesObject()
	if err := checkProcess(o, nil); err != nil {
		t.Fatal(err)
	}
	b := New("rules")
	canonical, err := b.CanonicalObject(o)
	if err != nil {
		t.Fatal(err)
	}
	action := canonical.Actions[0]
	if action.Transaction == nil || len(action.Sets) > 0 || len(action.Conditions) > 0 || action.ToInput != "" {
		t.Fatal("State action was not replaced by one transaction")
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
		var inputs map[string]any
		if err := json.Unmarshal([]byte(tc.payload), &inputs); err != nil {
			t.Fatal(err)
		}
		fields := []platform.Field{}
		for _, input := range action.Inputs {
			fields = append(fields, input.Field())
		}
		allowed := len(platform.InputIssues(fields, inputs)) == 0
		scope := ruleSpec(canonical, action, action.Transaction.Reads)
		if allowed {
			result, problem := evalRule(*action.Transaction.Guard, scope, platform.Caller{}, r, inputs, time.Now(), ruleLookup(canonical, nil))
			allowed = problem == nil && string(result.Value.Data) == "true"
		}
		if allowed {
			for _, set := range action.Transaction.Steps[0].Changes {
				if set.Field == "state" {
					value, problem := ruleAssignment(*set.Expr, scope, platform.Caller{}, r, inputs, time.Now(), ruleLookup(canonical, nil))
					if problem != nil {
						t.Fatal(problem)
					}
					r.State = value.(string)
				}
			}
		}
		if allowed != tc.ok || r.State != tc.state {
			t.Fatalf("%s: state=%s allowed=%t", tc.payload, r.State, allowed)
		}
	}
	for _, edit := range []func(*construction.Object){func(o *construction.Object) { o.Actions[0].To = "offline" }, func(o *construction.Object) { o.Actions[0].ToInput = "priority" }, func(o *construction.Object) { o.Actions[0].Inputs[0].Required = false }, func(o *construction.Object) { o.Actions[0].Conditions[0].When.When = &construction.Condition{} }, func(o *construction.Object) { o.Actions[0].Conditions[0].When.Field = "input.missing" }, func(o *construction.Object) { o.Actions[0].Approval = &construction.ActionApproval{} }} {
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
	o.Actions[0].Inputs = []construction.Input{{Name: "operator", Title: "Operator", Type: "reference", Ref: "build.operator", Required: true}, {Name: "title", Title: "Title", Type: "text", MinLength: &min}}
	o.Actions[0].Sets = []construction.Set{{Field: "operator", From: "operator"}}
	o.Actions[0].Conditions = nil
	lookup := func(name string) (platform.EntityInfo, bool) {
		return platform.EntityInfo{App: "build", Type: name}, name == "build.operator"
	}
	if err := checkProcess(o, lookup); err != nil {
		t.Fatal(err)
	}
	descriptor := lifecycle(o, []string{construction.User}, nil, lookup)
	if descriptor.Transitions[0].Payload[0].Ref != "build.operator" || descriptor.Transitions[0].Payload[1].Constraints == nil || *descriptor.Transitions[0].Payload[1].Constraints.MinLength != min {
		t.Fatal("reference became an ordinary text parameter")
	}
	for _, input := range []struct {
		title string
		ok    bool
	}{{"abc", false}, {"abcd", true}, {"", true}, {"\U0001f600\U0001f600", true}} {
		issues := platform.InputIssues(descriptor.Transitions[0].Payload, map[string]any{"operator": "OP", "title": input.title})
		if (len(issues) == 0) != input.ok {
			t.Fatalf("title %q: %v", input.title, issues)
		}
	}
	for _, edit := range []func(*construction.Object){func(o *construction.Object) { o.Actions[0].Inputs[0].Ref = "build.other" }, func(o *construction.Object) { o.Actions[0].Inputs[0].Type = "text" }, func(o *construction.Object) { o.Actions[0].Inputs[1].Type = "boolean" }, func(o *construction.Object) { bad := 4097; o.Actions[0].Inputs[1].MinLength = &bad }} {
		raw, _ := json.Marshal(o)
		var copy construction.Object
		json.Unmarshal(raw, &copy)
		edit(&copy)
		if checkProcess(copy, lookup) == nil {
			t.Fatal("incompatible reference or length accepted")
		}
	}
}
