package construction

import (
	"encoding/json"
	"slices"
	"testing"

	"platformkernel/kernel"
	"platformserver/platform"
)

func TestProcessCompileDefersAuthorizedReadsToExecution(t *testing.T) {
	reads := 0
	p := Process{Name: "lookup", Manual: true, Steps: []ProcessStep{
		{Name: "lookup", Kind: "query", App: "catalog", Query: "items", Next: "done"},
		{Name: "done", Kind: "end"},
	}}
	flow := CompileProcess(p, ProcessExecution{Query: func(_ platform.Caller, _ *platform.Run, app, name string, version int, input json.RawMessage) (json.RawMessage, *kernel.Error) {
		reads++
		if app != "catalog" || name != "items" || version != 0 || string(input) != "{}" {
			t.Fatalf("compiled query changed its original read: %s/%s@%d %s", app, name, version, input)
		}
		return json.RawMessage(`{"sources":["catalog.item/one#title"],"items":[]}`), nil
	}})
	if reads != 0 || len(flow.Steps) != 2 || !flow.Steps[1].End {
		t.Fatal("compilation read live data or changed the native control graph")
	}
	run := &platform.Run{}
	answer, err := flow.Steps[0].Evaluate.Run(platform.Caller{}, run)
	if err != nil || len(answer) == 0 || reads != 1 || !slices.Equal(run.Sources, []string{"catalog.item/one#title"}) {
		t.Fatalf("execution bypassed the original read/provenance callback: %s %v", answer, err)
	}
}
