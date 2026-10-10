package build

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/internal/host"
	"platformserver/platform"
)

type processStartRuntime struct {
	platform.Runtime
	current Process
}

func (r *processStartRuntime) Get(_ platform.Caller, typ reflect.Type, id string) (any, bool) {
	if typ == reflect.TypeFor[Process]() && id == r.current.ID {
		return r.current, true
	}
	return nil, false
}

type processStartHost struct {
	host.Host
	caller platform.Caller
}

func (h processStartHost) Automation(platform.Caller, string) platform.Caller { return h.caller }

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

func TestContinuousProcessUsesPublishedSourceStartInsteadOfManualEntry(t *testing.T) {
	p := Process{
		Record: platform.Record{ID: "continuous"}, Name: "telemetry", Title: "Telemetry", State: "published", Version: 1, Scheduler: "builder",
		Continuous: &platform.Continuous{
			Source: "planttelemetry", Batch: 512, State: 16 << 20, FrameBytes: 64 << 20, DeadLetter: true,
			Window: &platform.StreamWindow{Node: "windowstate", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 10000, LateEvents: "sideOutput"},
			Intake: &platform.StreamIntake{SourceRecord: "source-1", Key: "eventId", Partition: []string{"plantId", "deviceId"}, EventTime: "eventTime", Value: "reading", Offset: "latest"},
		},
		Steps: []ProcessStep{{Name: "finish", Kind: "end"}},
	}
	runtime := &processStartRuntime{current: p}
	caller := platform.NewCaller(runtime, platform.Member{ID: "builder"}, ID, false, true)
	b := &Build{host: processStartHost{caller: caller}}
	if err := b.checkFlowOn(p, platform.Entity{}); err != nil {
		t.Fatalf("an automatically started continuous process is valid: %v", err)
	}
	compiled := b.flowOf(p)
	if compiled.Start.Manual || !compiled.Start.Continuous || compiled.Start.OnBehalf != "builder" || compiled.Start.Enabled == nil || !compiled.Start.Enabled(platform.Caller{}) {
		t.Fatalf("the compiled Process lost its published-source lifecycle or active-owner guard: %+v", compiled.Start)
	}
	runtime.current.State = "draft"
	if compiled.Start.Enabled(platform.Caller{}) {
		t.Fatal("a draft Process kept its continuous start enabled")
	}
	runtime.current.State, runtime.current.Version = "published", 2
	if compiled.Start.Enabled(platform.Caller{}) {
		t.Fatal("a newer Process version kept an old continuous Flow start enabled")
	}
	runtime.current.Version, runtime.current.Name = 1, "replacement"
	if compiled.Start.Enabled(platform.Caller{}) {
		t.Fatal("a renamed Process kept an obsolete continuous Flow start enabled")
	}
	manual := p
	manual.Manual = true
	if err := b.checkFlowOn(manual, platform.Entity{}); err == nil {
		t.Fatal("a controlled continuous Process must not be downgraded to manual start")
	}
}
