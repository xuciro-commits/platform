package build

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"testing"
	"time"

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

func TestProcessDeadlineUsesAuthorizedTimestampAndPreservesReadRefusal(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	binding := &platform.Binding{Source: "subject", Path: []string{"expires"}}
	process := Process{Name: "deadline", Manual: true, Object: "build.case", Steps: []ProcessStep{{Name: "wait", Kind: "wait", WaitUntil: binding}}}
	reads := 0
	denied := false
	compiled := CompileProcess(process, ProcessExecution{SubjectPath: func(_ platform.Caller, _ *platform.Run, path []string) (json.RawMessage, []string, *kernel.Error) {
		reads++
		if denied {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Current member cannot read cutoff")
		}
		return platform.Raw("2026-10-21T13:00:00+02:00"), []string{"build.case/one#expires"}, nil
	}})
	if reads != 0 {
		t.Fatal("compilation read a live cutoff")
	}
	run := &platform.Run{Now: at}
	deadline, err := compiled.Steps[0].Wait.At(platform.Caller{}, run)
	if err != nil || !deadline.Equal(at.Add(2*time.Hour)) || !slices.Equal(run.Sources, []string{"build.case/one#expires"}) {
		t.Fatalf("declared deadline lost instant or provenance: %s %v %+v", deadline, err, run.Sources)
	}
	denied = true
	if _, err := compiled.Steps[0].Wait.At(platform.Caller{}, run); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("unreadable deadline silently became an immediate wait: %v", err)
	}
	process.Object = ""
	process.Steps[0].WaitUntil = &platform.Binding{Source: "literal", Value: platform.Raw("2026-10-21T10:00:00Z")}
	if err := CheckProcess(process, platform.Entity{}, ProcessLookup{}); err != nil {
		t.Fatal(err)
	}
	process.Steps[0].WaitUntil.Value = platform.Raw("not a timestamp")
	if err := CheckProcess(process, platform.Entity{}, ProcessLookup{}); err == nil {
		t.Fatal("invalid deadline passed authoring checks")
	}
}
