package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	ProcessType      = "build.process"
	SchemaProcess    = ProcessType + ".publish"
	SchemaProcessRun = ProcessType + ".run"
)

// Process is the single declarative definition compiled to native Flow/Step.
// Canvas positions are presentation; step IDs and typed references are logic.
//
// Kind says how the builder authored it (ADR-0053 §7): a "flow" is the free
// node graph; an "automation" is the linear sentence "when a record of Object
// reaches When, [if Condition,] run these effects" - the same steps, in a shape
// the host checks (checkAutomation) so the card editor and the map agree.
type Process struct {
	platform.Record
	Name   string `json:"name" field:"required,search"`
	Title  string `json:"title" field:"required,search"`
	Kind   string `json:"kind,omitempty" enum:"flow,automation" title:"Authoring shape"`
	Object string `json:"object,omitempty" title:"Source object"`
	When   string `json:"when,omitempty" title:"Record start state"`
	Manual bool   `json:"manual,omitempty" title:"Manual start"`
	// Every schedules the process instead of a record state or a hand: a Go
	// duration such as 15m, 1h or 24h, at least 1m (ADR-0057 E1). A scheduled
	// process has no source record; its steps act as the member who published it.
	Every string `json:"every,omitempty" title:"Repeat every" help:"A period such as 15m, 1h or 24h; empty: not scheduled"`
	// Scheduler is the member who published the schedule: scheduled runs act as them.
	Scheduler   string                `json:"scheduler,omitempty" field:"readonly" title:"Runs as"`
	Input       json.RawMessage       `json:"input,omitempty" type:"json" title:"Default input"`
	InputSchema *platform.ValueSchema `json:"inputSchema,omitempty" type:"json" title:"Input schema"`
	Continuous  *platform.Continuous  `json:"continuous,omitempty" type:"json" title:"Continuous source intake"`
	Steps       []ProcessStep         `json:"steps" field:"aside"`
	// Lanes are the responsibilities the process is drawn across (ADR-0087 D2): a
	// step names the lane that does it. A lane carries no execution semantics, so
	// the compiler ignores it and the canvas draws it as a band behind its steps.
	Lanes     []ProcessLane           `json:"lanes,omitempty" field:"aside" title:"Lanes"`
	Layout    map[string]NodePosition `json:"layout,omitempty" type:"json" title:"Canvas layout"`
	State     string                  `json:"state" field:"readonly" choices:"draft,published"`
	Version   int                     `json:"version,omitempty" field:"readonly"`
	Published string                  `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions  []string                `json:"versions,omitempty" field:"readonly"`
}

// ProcessLane is one BPMN lane: a named responsibility, titled for the reader.
type ProcessLane struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type OperationRef struct {
	App     string `json:"app,omitempty"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ProcessStep has one explicit kind and one binding/predicate grammar. There
// is no old ask/action branch compiler beside this representation.
type ProcessStep struct {
	Name           string                      `json:"name"`
	Title          string                      `json:"title,omitempty"`
	Kind           string                      `json:"kind" enum:"payload,query,action,transform,branch,switch,foreach,while,fork,join,ask,wait,subflow,ai,compute,end,fail,break,continue"`
	Lane           string                      `json:"lane,omitempty" title:"Lane" help:"The lane responsible for this step"`
	Inputs         map[string]platform.Binding `json:"inputs,omitempty" type:"json"`
	Value          *platform.Binding           `json:"value,omitempty" type:"json"`
	Target         *platform.Binding           `json:"target,omitempty" type:"json"`
	Condition      *platform.Predicate         `json:"condition,omitempty" type:"json"`
	Collection     *platform.Binding           `json:"collection,omitempty" type:"json"`
	Cases          map[string]string           `json:"cases,omitempty" type:"json"`
	Branches       []string                    `json:"branches,omitempty"`
	Next           string                      `json:"next,omitempty"`
	Error          string                      `json:"error,omitempty"`
	Ask            string                      `json:"ask,omitempty"`
	Answers        []string                    `json:"answers,omitempty"`
	Act            string                      `json:"act,omitempty"`
	Protocol       string                      `json:"protocol,omitempty"`
	App            string                      `json:"app,omitempty"`
	Query          string                      `json:"query,omitempty"`
	QueryVersion   int                         `json:"queryVersion,omitempty" title:"Retained query version"`
	Function       *platform.FunctionRef       `json:"function,omitempty" type:"json"`
	Operation      *OperationRef               `json:"operation,omitempty" type:"json"`
	Body           string                      `json:"body,omitempty"`
	MaxIterations  int                         `json:"maxIterations,omitempty"`
	Concurrency    int                         `json:"concurrency,omitempty"`
	Mode           string                      `json:"mode,omitempty" enum:"all,any"`
	TimeoutSeconds int                         `json:"timeoutSeconds,omitempty"`
	UntilSeconds   int                         `json:"untilSeconds,omitempty"`
	RetryAttempts  int                         `json:"retryAttempts,omitempty"`
	RetryBackoffMs int                         `json:"retryBackoffMs,omitempty"`
	Flow           string                      `json:"flow,omitempty"`
	FlowVersion    int                         `json:"flowVersion,omitempty"`
}

func (b *Build) processEntity() platform.Entity {
	return platform.Entity{Type: ProcessType, Title: "Process", Plural: "Processes", Model: Process{}, Display: "title", Description: "Typed capability blocks compiled to the platform's native flow.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "processes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{}, Do: b.publishProcess},
				{Name: "run", Title: "Run", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "processes", Payload: []platform.Field{{Name: "key", Type: "string", Description: "Stable run key"}, {Name: "input", Type: "string", Description: "Typed JSON input"}}, Do: b.runProcess}}}}
}

func (b *Build) publishProcess(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p, ok := record.(*Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	procs := b.host.Processes()
	if procs == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no processes")
	}
	if !c.Replaying {
		if err := b.checkFlow(*p); err != nil {
			return err
		}
		if p.Continuous != nil && p.Continuous.Intake != nil {
			source, ok := platform.Get[Source](c, p.Continuous.Intake.SourceRecord)
			if !ok || !source.Stream || source.State != "published" || source.Name != p.Continuous.Source {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The intake names a readable published continuous source")
			}
			if err := b.checkSource(c, source); err != nil {
				return err
			}
		}
	}
	if p.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A process may retain at most 64 published versions")
	}
	p.Version++
	if p.Every != "" || p.Continuous != nil && p.Continuous.Intake != nil {
		p.Scheduler = c.ID
	} else {
		p.Scheduler = ""
	}
	fl := b.flowOf(*p)
	if c.Staging() {
		if err := procs.Validate(b, fl); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := procs.Install(b, fl); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	p.Published = published(*p)
	p.Versions = append(p.Versions, p.Published)
	return nil
}

func (b *Build) runProcess(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
	p, ok := record.(*Process)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	installed, ok := wasPublished[Process](p.Published)
	if !ok || !installed.Manual {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Publish a manual workflow before running it")
	}
	var request struct {
		Key   string `json:"key"`
		Input string `json:"input"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a stable key and JSON input")
	}
	if request.Key == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A manual run needs a stable key")
	}
	input := installed.Input
	if request.Input != "" {
		input = json.RawMessage(request.Input)
	}
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if installed.InputSchema != nil {
		if err := installed.InputSchema.Validate(input, 64<<10); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	start, ok := b.host.Processes().(interface {
		StartManual(platform.Caller, string, string, int, string, json.RawMessage, time.Time) *kernel.Error
	})
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no manual processes")
	}
	return start.StartManual(c, ID, installed.Name, installed.Version, request.Key, input, now)
}

func (b *Build) checkFlow(p Process) *kernel.Error {
	plans, err := b.processInventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process inventory cannot be checked")
	}
	for _, other := range plans {
		installed, _ := wasPublished[Process](other.Published)
		if other.ID != p.ID && (other.Name == p.Name || installed.Name == p.Name) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process name {name} is already used", p.Name)
		}
	}
	return b.checkFlowOn(p, b.installed[p.Object])
}

// CheckProcess exposes the exact publication compiler to edit/test feedback.
// It returns node-qualified errors instead of trusting frontend edge rules.
func (b *Build) CheckProcess(p Process) *kernel.Error { return b.checkFlow(p) }

func (b *Build) checkFlowOn(p Process, entity platform.Entity) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if !named(p.Name) {
		return refuse("The process name {name} must be lower-case letters and digits", p.Name)
	}
	if old, ok := wasPublished[Process](p.Published); ok && (old.Name != p.Name || old.Object != p.Object) {
		return refuse("A published process keeps its name and object")
	}
	if p.Kind != "" && p.Kind != "flow" && p.Kind != "automation" {
		return refuse("A process is authored as a flow or an automation")
	}
	if p.Kind == "automation" {
		if err := checkAutomation(p); err != nil {
			return err
		}
	}
	if p.Every != "" {
		every, parseErr := time.ParseDuration(p.Every)
		if parseErr != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A schedule repeats every period between 1m and a year, such as 15m, 1h or 24h")
		}
		if p.Manual || p.Object != "" || p.When != "" {
			return refuse("A scheduled process has no source record and is not started by hand")
		}
	}
	continuousStart := p.Continuous != nil && p.Continuous.Intake != nil
	if continuousStart && (p.Manual || p.Every != "" || p.Object != "" || p.When != "") {
		return refuse("A controlled continuous Process starts from its published source, not by hand, on a schedule or from a record state")
	}
	if !p.Manual && p.Every == "" && !continuousStart {
		var states []platform.State
		if entity.Type == p.Object && entity.Lifecycle != nil {
			states = entity.Lifecycle.States
		} else if info, ok := b.lookupEntity(p.Object); ok && info.Lifecycle != nil {
			for _, state := range info.Lifecycle.States {
				states = append(states, platform.State{Name: state.Name})
			}
		}
		if !slices.ContainsFunc(states, func(s platform.State) bool { return s.Name == p.When }) {
			return refuse("The object has no process start state {state}", p.When)
		}
	}
	if p.InputSchema != nil {
		if err := p.InputSchema.Check(); err != nil {
			return refuse("Workflow input: " + err.Error())
		}
	}
	if len(p.Input) > 0 {
		if _, err := platform.DecodeValue(p.Input, 64<<10); err != nil {
			return refuse("Workflow input: " + err.Error())
		}
		if p.InputSchema != nil {
			if err := p.InputSchema.Validate(p.Input, 64<<10); err != nil {
				return refuse("Workflow input: " + err.Error())
			}
		}
	}
	if len(p.Lanes) > 32 {
		return refuse("A process is drawn across at most 32 lanes")
	}
	lanes := map[string]bool{}
	for _, lane := range p.Lanes {
		if !named(lane.Name) || lanes[lane.Name] {
			return refuse("The lane {lane} needs a unique lower-case name", lane.Name)
		}
		lanes[lane.Name] = true
	}
	if len(p.Steps) < 1 || len(p.Steps) > 128 {
		return refuse("A process needs 1–128 typed steps")
	}
	nodes := map[string]ProcessStep{}
	for _, step := range p.Steps {
		if !named(step.Name) || nodes[step.Name].Name != "" {
			return refuse("The process step {step} needs a unique lower-case name", step.Name)
		}
		nodes[step.Name] = step
	}
	parents := map[string]string{}
	for _, step := range p.Steps {
		problem := func(message string) *kernel.Error { return refuse("Node " + step.Name + ": " + message) }
		if !slices.Contains([]string{"payload", "query", "action", "transform", "branch", "switch", "foreach", "while", "fork", "join", "ask", "wait", "subflow", "ai", "compute", "end", "fail", "break", "continue"}, step.Kind) {
			return problem("choose one typed block kind")
		}
		if step.Lane != "" && !lanes[step.Lane] {
			return refuse("Node "+step.Name+": names the lane {lane}, which this process does not declare", step.Lane)
		}
		if len(step.Inputs) > 64 {
			return problem("a block accepts at most 64 input bindings")
		}
		if step.Ask != "" && step.Kind != "ask" || step.Act != "" && step.Kind != "action" || step.Function != nil && step.Kind != "ai" || step.Operation != nil && step.Kind != "compute" || (step.Query != "" || step.QueryVersion != 0) && step.Kind != "query" || len(step.Answers) > 0 && step.Kind != "ask" || len(step.Cases) > 0 && step.Kind != "ask" && step.Kind != "branch" && step.Kind != "switch" || len(step.Branches) > 0 && step.Kind != "fork" || step.Body != "" && step.Kind != "foreach" && step.Kind != "while" {
			return problem("configuration must match this block kind")
		}
		if step.TimeoutSeconds < 0 || step.TimeoutSeconds > 30*86400 || step.UntilSeconds < 0 || step.UntilSeconds > 30*86400 {
			return problem("wait/timeout exceeds its bound")
		}
		if step.RetryAttempts < 0 || step.RetryAttempts > 20 || step.RetryBackoffMs < 0 || step.RetryBackoffMs > int((5*time.Minute)/time.Millisecond) {
			return problem("retry attempts/backoff exceed their bounds")
		}
		if step.TimeoutSeconds > 0 && step.Error == "" {
			return problem("a timeout needs an error path")
		}
		bindings := maps.Clone(step.Inputs)
		if bindings == nil {
			bindings = map[string]platform.Binding{}
		}
		for name, binding := range map[string]*platform.Binding{"value": step.Value, "target": step.Target, "collection": step.Collection} {
			if binding != nil {
				bindings[name] = *binding
			}
		}
		for name, binding := range bindings {
			if err := binding.Check(); err != nil {
				return problem(name + ": " + err.Error())
			}
			if binding.Source == "step" && nodes[binding.Step].Name == "" {
				return problem(name + ": upstream node is missing")
			}
		}
		if step.Condition != nil {
			if err := step.Condition.Check(); err != nil {
				return problem(err.Error())
			}
		}
		switch step.Kind {
		case "ask":
			if step.Ask == "" {
				return problem("choose a recipient role")
			}
			scope := entity.Scope
			if entity.Type != p.Object {
				if info, ok := b.lookupEntity(p.Object); ok {
					scope = info.Scope
				}
			}
			readable := step.Ask == Builder || p.Object == "" || len(scope.Levels) == 0 && step.Ask == User || scope.Levels[step.Ask] == platform.ScopeTenant
			if !readable {
				return problem("recipient role must read all records of its source")
			}
			seen := map[string]bool{}
			for _, answer := range step.Answers {
				if strings.TrimSpace(answer) == "" || seen[answer] {
					return problem("answers must be unique and nonempty")
				}
				seen[answer] = true
			}
			for answer := range step.Cases {
				if !seen[answer] {
					return problem("answer path does not name an allowed answer")
				}
			}
		case "action":
			schema := step.Act
			if !strings.Contains(schema, ".") {
				schema = p.Object + "." + schema
			}
			owner, action, ok := b.host.Action(schema)
			if entity.Type == p.Object && strings.HasPrefix(schema, p.Object+".") {
				ok = false
				for _, candidate := range platform.EntityActions(entity) {
					if candidate.Schema == schema {
						action = candidate
						owner = ID
						ok = true
						break
					}
				}
			}
			if step.Protocol == "" {
				if !ok || action.Automation {
					return problem("action " + schema + " is not declared as an exposed native action")
				}
				_ = owner
				for _, field := range action.Payload {
					if field.Required {
						if _, ok := step.Inputs[field.Name]; !ok {
							return problem("required action input " + field.Name + " is missing")
						}
					}
				}
			}
			if step.Act == "" {
				return problem("choose an action")
			}
			if p.Object == "" && step.Target == nil {
				return problem("choose an explicit target")
			}
		case "ai":
			if step.Function == nil {
				return problem("choose a retained AI function")
			}
			f, _, ok := b.host.Function(cmp.Or(step.Function.App, ID), step.Function.Name, step.Function.Version)
			if !ok || cmp.Or(step.Function.App, ID) == ID && step.Function.Version < 1 || cmp.Or(step.Function.App, ID) != ID && step.Function.Version != 0 || f.Object != p.Object && step.Target == nil {
				return problem("AI function source/version is not installed")
			}
		case "compute":
			if step.Operation == nil || step.Operation.Name == "" || step.Operation.Version < 0 {
				return problem("choose a retained computation")
			}
			op, _, ok := b.host.Operation(cmp.Or(step.Operation.App, ID), step.Operation.Name, step.Operation.Version)
			if !ok {
				return problem("computation owner/version is not installed")
			}
			if step.Value != nil {
				if len(step.Inputs) > 0 {
					return problem("choose a whole input value or named field bindings")
				}
				if step.Value.Source == "literal" {
					if err := op.Input.Validate(step.Value.Value, 64<<10); err != nil {
						return problem("input: " + err.Error())
					}
				}
				break
			}
			for _, field := range op.Input.Required {
				if _, ok := step.Inputs[field]; !ok {
					return problem("required compute input " + field + " is missing")
				}
			}
			for name, binding := range step.Inputs {
				schema, ok := op.Input.Properties[name]
				if !ok {
					return problem("unknown compute input " + name)
				}
				if binding.Source == "literal" {
					if err := schema.Validate(binding.Value, 64<<10); err != nil {
						return problem("input " + name + ": " + err.Error())
					}
				}
			}
		case "query":
			if step.Query == "" || step.App == "" {
				return problem("choose an owner query")
			}
			if step.App == ID {
				q, ordinal, ok := b.QueryDefinition(step.Query, step.QueryVersion)
				if !ok || step.QueryVersion < 1 || step.QueryVersion > 64 || ordinal != step.QueryVersion {
					return problem("choose an exact retained tenant query version")
				}
				if err := b.host.ValidateInstallQuery(q); err != nil {
					return problem(err.Error())
				}
				for name, binding := range step.Inputs {
					if name != "for" || q.By == "" {
						return problem("unknown query input " + name)
					}
					if binding.Source == "literal" {
						var value string
						if json.Unmarshal(binding.Value, &value) != nil || value == "" {
							return problem("query for input needs a record ID")
						}
					}
				}
				if q.By != "" {
					if _, ok := step.Inputs["for"]; !ok {
						return problem("query requires its for input")
					}
				}
			} else if step.QueryVersion != 0 {
				return problem("code queries use their owner version")
			}
		case "branch":
			if step.Condition == nil || step.Cases["true"] == "" || step.Cases["false"] == "" {
				return problem("choose a predicate and true/false paths")
			}
		case "switch":
			if step.Value == nil || len(step.Cases) == 0 || step.Next == "" {
				return problem("switch needs a value, cases and default path")
			}
		case "foreach", "while":
			if step.Body == "" || step.MaxIterations < 1 || step.MaxIterations > 10000 || step.Concurrency < 0 || step.Concurrency > 32 {
				return problem("loop needs a body, bounded iterations/concurrency")
			}
			if step.Kind == "foreach" && step.Collection == nil || step.Kind == "while" && (step.Condition == nil || step.Concurrency > 1) {
				return problem("choose loop collection/condition")
			}
			if parents[step.Body] != "" {
				return problem("a body belongs to one loop scope")
			}
			parents[step.Body] = step.Name
		case "fork":
			if len(step.Branches) < 2 || len(step.Branches) > 32 || step.Mode != "" && step.Mode != "all" && step.Mode != "any" {
				return problem("fork needs 2–32 branches and all/any mode")
			}
		case "subflow":
			if step.Flow == "" || step.FlowVersion < 1 {
				return problem("choose a retained subflow")
			}
		case "wait":
			if step.UntilSeconds == 0 && step.Condition == nil {
				return problem("choose a duration or wait predicate")
			}
		case "end", "join", "break", "continue":
			if step.Next != "" || len(step.Cases) > 0 {
				return problem("scope end has no next control edge")
			}
		}
		for _, to := range processPaths(step) {
			if to != "" && nodes[to].Name == "" {
				return problem("control path " + to + " is missing")
			}
		}
	}
	// Cycles are rejected even if they would wait: iteration is an explicit
	// loop scope with its cursor, not a Choose callback jumping backwards.
	seen, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if name == "" {
			return nil
		}
		if active[name] {
			return fmt.Errorf("Node %s: arbitrary control cycle; use a loop scope", name)
		}
		if seen[name] {
			return nil
		}
		active[name] = true
		for _, next := range processPaths(nodes[name]) {
			if err := visit(next); err != nil {
				return err
			}
		}
		active[name] = false
		seen[name] = true
		return nil
	}
	if err := visit(p.Steps[0].Name); err != nil {
		return refuse(err.Error())
	}
	for _, step := range p.Steps {
		if !seen[step.Name] {
			return refuse("Node " + step.Name + ": unreachable from the entry")
		}
	}
	if err := checkProcessScopes(p); err != nil {
		return refuse(err.Error())
	}
	return nil
}
func processPaths(s ProcessStep) []string {
	paths := []string{s.Next, s.Error, s.Body}
	paths = append(paths, s.Branches...)
	paths = append(paths, slices.Collect(maps.Values(s.Cases))...)
	return paths
}

// processImage checks the already saved version family, independently of
// draft rules. Old versions are flat images, not copies of their own history.
func processImage(image []byte) (Process, error) {
	var record Process
	if err := json.Unmarshal(image, &record); err != nil {
		return Process{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Process{}, fmt.Errorf("accepted process has an invalid version family")
	}
	var latest Process
	for index, raw := range record.Versions {
		version, ok := wasPublished[Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

// flowOf compiles every declarative block into the existing native owner.
func (b *Build) flowOf(p Process) platform.Flow {
	fl := platform.Flow{Name: p.Name, Title: p.Title, Version: p.Version, Subject: p.Object, Owners: []string{Builder}, Start: platform.Start{Manual: p.Manual}, Continuous: p.Continuous}
	if p.Every != "" {
		fl.Start.Every, _ = time.ParseDuration(p.Every)
		fl.Start.OnBehalf = p.Scheduler
	} else if p.Continuous != nil && p.Continuous.Intake != nil {
		processID, processName, processVersion := p.ID, p.Name, p.Version
		fl.Start.Manual, fl.Start.Continuous, fl.Start.OnBehalf = false, true, p.Scheduler
		fl.Start.Enabled = func(c platform.Caller) bool {
			if b.host == nil || processID == "" || processVersion < 1 {
				return false
			}
			current, ok := platform.Get[Process](b.host.Automation(c, ID), processID)
			return ok && current.State == "published" && current.Version == processVersion && current.Name == processName
		}
	} else if !p.Manual {
		fl.Start.Type = p.Object
		stateField := "state"
		if info, ok := b.lookupEntity(p.Object); ok && info.Lifecycle != nil {
			stateField = info.Lifecycle.Field
		}
		fl.Start.When = func(_ platform.Caller, record any) bool {
			raw, _ := json.Marshal(record)
			var value map[string]json.RawMessage
			json.Unmarshal(raw, &value)
			var state string
			json.Unmarshal(value[stateField], &state)
			return state == p.When
		}
	}
	for _, node := range p.Steps {
		s := node
		step := platform.Step{Name: s.Name, Title: cmp.Or(s.Title, s.Name), Next: s.Next, Fault: s.Error, Timeout: time.Duration(s.TimeoutSeconds) * time.Second, OnTimeout: s.Error}
		if s.RetryAttempts > 0 || s.RetryBackoffMs > 0 {
			step.Retry = &platform.RetryPolicy{Attempts: s.RetryAttempts, Backoff: time.Duration(s.RetryBackoffMs) * time.Millisecond}
		}
		subject := func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			if p.Object == "" {
				return json.RawMessage("{}"), nil
			}
			member, ok := b.host.Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			raw, err := b.host.Caller(c, member, ID).ReadRecord(p.Object, r.Key, r.Now)
			if err == nil {
				var fields map[string]json.RawMessage
				info, known := b.lookupEntity(p.Object)
				if known && json.Unmarshal(raw, &fields) == nil {
					for name := range fields {
						// Record identity/stamps are covered by the record source.
						// Field provenance only names the owner's declared fields.
						if _, declared := info.Field(name); declared {
							r.Sources = append(r.Sources, p.Object+"/"+r.Key+"#"+name)
						}
					}
				}
			}
			return raw, err
		}
		resolve := func(c platform.Caller, r *platform.Run, binding platform.Binding) (json.RawMessage, *kernel.Error) {
			if binding.Source == "subject" && len(binding.Path) > 0 {
				member, known := b.host.Member(r.OnBehalf)
				if !known {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
				}
				value, sources, err := b.host.Caller(c, member, ID).ReadRecordPath(p.Object, r.Key, binding.Path, r.Now)
				r.Sources = append(r.Sources, sources...)
				return value, err
			}
			var raw json.RawMessage
			if binding.Source == "subject" {
				var err *kernel.Error
				raw, err = subject(c, r)
				if err != nil {
					return nil, err
				}
			}
			value, err := binding.Resolve(r, raw)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return value, nil
		}
		inputs := func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			values := map[string]json.RawMessage{}
			for _, name := range slices.Sorted(maps.Keys(s.Inputs)) {
				value, err := resolve(c, r, s.Inputs[name])
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
			return platform.Raw(values), nil
		}
		predicate := func(c platform.Caller, r *platform.Run) (bool, *kernel.Error) {
			raw := json.RawMessage("{}")
			if predicateSubject(*s.Condition) {
				var err *kernel.Error
				raw, err = subject(c, r)
				if err != nil {
					return false, err
				}
			}
			holds, problem := s.Condition.Test(r, raw)
			if problem != nil {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
			}
			return holds, nil
		}
		target := func(c platform.Caller, r *platform.Run) string {
			if s.Target == nil {
				return r.Key
			}
			raw, err := resolve(c, r, *s.Target)
			if err != nil {
				return ""
			}
			var value string
			json.Unmarshal(raw, &value)
			return value
		}
		switch s.Kind {
		case "payload", "transform":
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				if s.Value != nil {
					return resolve(c, r, *s.Value)
				}
				if s.Kind == "payload" && len(s.Inputs) == 0 {
					return r.Data, nil
				}
				return inputs(c, r)
			}}
		case "query":
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				raw, err := inputs(c, r)
				if err != nil {
					return nil, err
				}
				member, ok := b.host.Member(r.OnBehalf)
				if !ok {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
				}
				answer, err := b.host.Caller(c, member, s.App).ReadQueryVersion(s.App, s.Query, s.QueryVersion, raw, r.Now)
				if err == nil {
					var provenance struct {
						Sources []string `json:"sources"`
					}
					json.Unmarshal(answer, &provenance)
					r.Sources = append(r.Sources, provenance.Sources...)
				}
				return answer, err
			}}
		case "branch":
			step.Branch = &platform.Branch{Paths: []string{s.Cases["true"], s.Cases["false"]}, Choose: func(c platform.Caller, r *platform.Run) (string, string, *kernel.Error) {
				holds, err := predicate(c, r)
				name := fmt.Sprint(holds)
				return s.Cases[name], name, err
			}}
		case "switch":
			step.Branch = &platform.Branch{Paths: append([]string{s.Next}, slices.Collect(maps.Values(s.Cases))...), Choose: func(c platform.Caller, r *platform.Run) (string, string, *kernel.Error) {
				raw, err := resolve(c, r, *s.Value)
				if err != nil {
					return "", "", err
				}
				var value string
				if json.Unmarshal(raw, &value) != nil {
					value = string(raw)
				}
				return cmp.Or(s.Cases[value], s.Next), value, nil
			}}
		case "foreach", "while":
			step.Loop = &platform.Loop{Body: s.Body, MaxIterations: s.MaxIterations, Concurrency: max(1, s.Concurrency)}
			if s.Kind == "while" {
				step.Loop.While = predicate
				step.Loop.Initial = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					if s.Value != nil {
						return resolve(c, r, *s.Value)
					}
					return inputs(c, r)
				}
			} else {
				step.Loop.Items = func(c platform.Caller, r *platform.Run) ([]json.RawMessage, *kernel.Error) {
					raw, err := resolve(c, r, *s.Collection)
					if err != nil {
						return nil, err
					}
					var items []json.RawMessage
					if json.Unmarshal(raw, &items) != nil {
						return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "ForEach needs an array")
					}
					return items, nil
				}
			}
		case "fork":
			if s.Mode == "any" {
				step.Any = s.Branches
			} else {
				step.All = s.Branches
			}
		case "break", "continue":
			step.LoopControl = s.Kind
			if s.Value != nil {
				step.Output = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					return resolve(c, r, *s.Value)
				}
			}
		case "join", "end":
			step.End = true
			if s.Value != nil {
				step.Output = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					return resolve(c, r, *s.Value)
				}
			}
		case "fail":
			step.NoRetry = true
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, cmp.Or(s.Title, "Flow failed"))
			}}
		case "action":
			schema := s.Act
			if !strings.Contains(schema, ".") {
				schema = p.Object + "." + schema
			}
			step.Act = &platform.Act{Action: schema, Protocol: s.Protocol, AsMember: s.Protocol == "", Target: target, TargetValue: func(c platform.Caller, r *platform.Run) (string, *kernel.Error) {
				if s.Target == nil {
					return r.Key, nil
				}
				raw, err := resolve(c, r, *s.Target)
				if err != nil {
					return "", err
				}
				var value string
				if json.Unmarshal(raw, &value) != nil || value == "" {
					return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action target must be a record ID")
				}
				return value, nil
			}, Inputs: inputs, Payload: func(c platform.Caller, r *platform.Run) any {
				raw, err := inputs(c, r)
				if err != nil {
					return json.RawMessage("null")
				}
				return raw
			}}
		case "ask":
			step.Ask = &platform.Ask{Answers: s.Answers, Title: func(_ platform.Caller, r *platform.Run) string { return cmp.Or(s.Title, s.Name) + ": " + r.Key }, Ref: func(_ platform.Caller, r *platform.Run) string {
				return cmp.Or(p.Object, "flow.instance") + "/" + r.Key
			}, To: func(platform.Caller, *platform.Run) []platform.Recipient {
				return []platform.Recipient{{AppRole: s.Ask}}
			}}
			step.Output = func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				return platform.Raw(map[string]string{"answer": r.Answer}), nil
			}
			if len(s.Cases) > 0 {
				step.Choose = func(_ platform.Caller, r *platform.Run) (string, string) {
					return cmp.Or(s.Cases[r.Answer], s.Next), r.Answer
				}
			}
		case "wait":
			step.Wait = &platform.Wait{}
			if s.Condition != nil {
				step.Wait.Until = func(c platform.Caller, r *platform.Run) bool {
					holds, err := predicate(c, r)
					return err == nil && holds
				}
			} else {
				step.Wait.At = func(_ platform.Caller, r *platform.Run) time.Time {
					return r.Now.Add(time.Duration(s.UntilSeconds) * time.Second)
				}
			}
		case "subflow":
			step.Call = &platform.Call{Flow: s.Flow, Version: s.FlowVersion, Data: func(c platform.Caller, r *platform.Run) any {
				raw, err := inputs(c, r)
				if err != nil {
					return json.RawMessage("null")
				}
				return raw
			}}
		case "compute":
			step.Operation = &platform.OperationStep{Request: func(c platform.Caller, r *platform.Run) (platform.OperationRequest, *kernel.Error) {
				raw, err := inputs(c, r)
				if s.Value != nil {
					raw, err = resolve(c, r, *s.Value)
				}
				if err != nil {
					return platform.OperationRequest{}, err
				}
				return platform.OperationRequest{App: cmp.Or(s.Operation.App, ID), Name: s.Operation.Name, Version: s.Operation.Version, Inputs: raw}, nil
			}}
		case "ai":
			step.Invoke = &platform.Invocation{Act: platform.Act{Action: SchemaFunctionCall, Target: func(_ platform.Caller, r *platform.Run) string {
				return fmt.Sprintf("%s:%s:%d", r.ID, s.Name, r.Sequence)
			}, Payload: func(c platform.Caller, r *platform.Run) any {
				return platform.FunctionRequest{App: s.Function.App, Name: s.Function.Name, Version: s.Function.Version, Source: target(c, r), OnBehalf: r.OnBehalf, Release: &r.Release}
			}}, Result: func(c platform.Caller, r *platform.Run, id string) (json.RawMessage, bool, *kernel.Error) {
				call, ok := platform.Get[FunctionRun](c, id)
				if !ok || call.State == "pending" {
					return nil, false, nil
				}
				if call.State != "ready" {
					return nil, true, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, cmp.Or(call.Reason, "AI function rejected its answer"))
				}
				r.Sources = append(r.Sources, call.Sources...)
				return json.RawMessage(call.Output), true, nil
			}}
		}
		fl.Steps = append(fl.Steps, step)
	}
	return fl
}

// installProcesses installs every published version of every process, oldest
// first, after the objects they run on (a restore, ADR-0034 D4).
func (b *Build) installProcesses() error {
	procs := b.host.Processes()
	list, err := b.processInventory()
	if err != nil {
		return err
	}
	for _, p := range list {
		if len(p.Versions) == 0 {
			continue
		}
		if procs == nil {
			return fmt.Errorf("published process %s needs the flow app", p.ID)
		}
		image, _ := json.Marshal(p)
		if _, err := processImage(image); err != nil {
			return err
		}
		for _, raw := range p.Versions {
			was, ok := wasPublished[Process](raw)
			if !ok {
				return fmt.Errorf("process %s: unreadable version", p.Name)
			}
			if err := procs.Install(b, b.flowOf(was)); err != nil {
				return fmt.Errorf("process %s: %v", p.Name, err)
			}
		}
	}
	return nil
}

func (b *Build) processInventory() ([]Process, error) {
	return readDefinitionInventory[Process](b.host.Automation(platform.Caller{}, ID))
}

func predicateSubject(p platform.Predicate) bool {
	if p.Left != nil && p.Left.Source == "subject" || p.Right != nil && p.Right.Source == "subject" {
		return true
	}
	for _, term := range p.Terms {
		if predicateSubject(term) {
			return true
		}
	}
	return false
}

// checkAutomation holds an automation to its sentence (ADR-0053 §7): it starts
// from a record reaching a state, never by hand; at most one condition, first,
// whose false path ends the run; then effects - actions, AI and compute steps -
// one after another, each leading to the next, the last to nothing. Anything
// richer is a flow and is edited on the map.
func checkAutomation(p Process) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if p.Manual || p.Every == "" && (p.Object == "" || p.When == "") {
		return refuse("An automation starts when a record of an object reaches a state, or on a schedule")
	}
	if len(p.Steps) == 0 {
		return refuse("An automation needs at least one effect")
	}
	ends := map[string]bool{}
	for _, s := range p.Steps {
		if s.Kind == "end" {
			ends[s.Name] = true
		}
	}
	chain := slices.DeleteFunc(slices.Clone(p.Steps), func(s ProcessStep) bool { return s.Kind == "end" })
	for i, s := range chain {
		last := i == len(chain)-1
		next := ""
		if !last {
			next = chain[i+1].Name
		}
		switch s.Kind {
		case "branch":
			if i != 0 {
				return refuse("An automation's condition comes first, before its effects")
			}
			if s.Cases["true"] != next || (s.Cases["false"] != "" && !ends[s.Cases["false"]]) {
				return refuse("An automation's condition leads to its first effect when true and ends the run when false")
			}
		case "action", "ai", "compute":
			if s.Next != next || len(s.Cases) > 0 || len(s.Branches) > 0 || s.Body != "" {
				return refuse("An automation's effects run one after another; step {name} leaves that line", s.Name)
			}
		default:
			return refuse("An automation's effects are actions, AI functions and compute operations; {name} is a {kind} step, which belongs on a flow map", s.Name, s.Kind)
		}
	}
	if len(chain) == 0 || chain[len(chain)-1].Kind == "branch" {
		return refuse("An automation needs at least one effect after its condition")
	}
	return nil
}
