package build

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestCompilerRejectsBranchOnlyAndEscapedIterationBindings(t *testing.T) {
	branchOnly := Process{Steps: []ProcessStep{{Name: "branch", Kind: "branch", Cases: map[string]string{"true": "yes", "false": "no"}}, {Name: "yes", Kind: "transform", Next: "merge"}, {Name: "no", Kind: "transform", Next: "merge"}, {Name: "merge", Kind: "end", Value: &platform.Binding{Source: "step", Step: "yes"}}}}
	if err := checkProcessScopes(branchOnly); err == nil || !strings.Contains(err.Error(), "not guaranteed") {
		t.Fatalf("optional branch output became a required downstream value: %v", err)
	}
	escaped := Process{Steps: []ProcessStep{{Name: "loop", Kind: "foreach", Body: "body", Next: "outside"}, {Name: "body", Kind: "transform", Next: "finish"}, {Name: "finish", Kind: "end"}, {Name: "outside", Kind: "end", Value: &platform.Binding{Source: "step", Step: "body"}}}}
	if err := checkProcessScopes(escaped); err == nil {
		t.Fatal("iteration-local output escaped without the loop's ordered result")
	}
}

func TestProcessStepDecodesStrictly(t *testing.T) {
	var process Process
	if err := json.Unmarshal([]byte(`{"name":"review","steps":[{"name":"ask","kind":"ask","ask":"user","answers":["yes"],"cases":{"yes":"done"},"inputs":{"threshold":{"source":"literal","value":10}}},{"name":"done","kind":"action","act":"close"}]}`), &process); err != nil {
		t.Fatal(err)
	}
	if process.Steps[0].Kind != "ask" || process.Steps[0].Cases["yes"] != "done" || process.Steps[0].Inputs["threshold"].Source != "literal" || process.Steps[1].Kind != "action" {
		t.Fatalf("step was not decoded: %+v", process)
	}
	if err := json.Unmarshal([]byte(`{"name":"review","steps":[{"name":"ask","ask":"user","branches":{"yes":"done"}}]}`), &process); err == nil {
		t.Fatal("an older step representation must be refused, not upgraded")
	}
}

func TestAutomationShape(t *testing.T) {
	act := func(name, next string) ProcessStep {
		return ProcessStep{Name: name, Kind: "action", Act: "close", Next: next}
	}
	base := Process{Name: "notify", Kind: "automation", Object: "build.item", When: "open"}
	ok := base
	ok.Steps = []ProcessStep{act("first", "second"), act("second", "")}
	if err := checkAutomation(ok); err != nil {
		t.Fatalf("a linear automation is accepted: %v", err)
	}
	withCondition := base
	withCondition.Steps = []ProcessStep{{Name: "when", Kind: "branch", Cases: map[string]string{"true": "first", "false": "stop"}}, act("first", ""), {Name: "stop", Kind: "end"}}
	if err := checkAutomation(withCondition); err != nil {
		t.Fatalf("a condition first, ending when false, is accepted: %v", err)
	}
	for name, bad := range map[string]func(p *Process){
		"manual":    func(p *Process) { p.Manual = true; p.Steps = ok.Steps },
		"no effect": func(p *Process) { p.Steps = nil },
		"loop": func(p *Process) {
			p.Steps = []ProcessStep{{Name: "each", Kind: "foreach", Body: "first"}, act("first", "")}
		},
		"broken chain": func(p *Process) { p.Steps = []ProcessStep{act("first", ""), act("second", "")} },
		"late branch": func(p *Process) {
			p.Steps = []ProcessStep{act("first", "when"), {Name: "when", Kind: "branch", Cases: map[string]string{"true": "second"}}, act("second", "")}
		},
	} {
		p := base
		bad(&p)
		if checkAutomation(p) == nil {
			t.Fatalf("%s: refused", name)
		}
	}
}
