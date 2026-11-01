package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

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
	procs := b.host.BuildProcesses()
	if procs == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no processes")
	}
	if !c.Replaying {
		if err := b.checkFlow(*p, b.installed[p.Object]); err != nil {
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
	installed, ok := ReadPublication[Process](p.Published)
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
	start, ok := b.host.BuildProcesses().(interface {
		StartManual(platform.Caller, string, string, int, string, json.RawMessage, time.Time) *kernel.Error
	})
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This tenant runs no manual processes")
	}
	return start.StartManual(c, ID, installed.Name, installed.Version, request.Key, input, now)
}

func (b *Build) checkFlow(p Process, entity platform.Entity, objects ...platform.Entity) *kernel.Error {
	plans, err := b.processInventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process inventory cannot be checked")
	}
	for _, other := range plans {
		installed, _ := ReadPublication[Process](other.Published)
		if other.ID != p.ID && (other.Name == p.Name || installed.Name == p.Name) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The process name {name} is already used", p.Name)
		}
	}
	return b.checkFlowOn(p, entity, objects...)
}

func (b *Build) CheckProcess(c platform.Caller, p Process) *kernel.Error {
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The object inventory cannot be checked")
	}
	source := b.installed[p.Object]
	models := []platform.Entity{}
	for _, object := range objects {
		if object.Archived {
			continue
		}
		model := Entity(object)
		models = append(models, model)
		if model.Type == p.Object {
			source = model
		}
	}
	return b.checkFlow(p, source, models...)
}

func (b *Build) checkFlowOn(p Process, entity platform.Entity, objects ...platform.Entity) *kernel.Error {
	lookup := ProcessLookup{Entity: b.lookupEntity, Query: b.QueryDefinition}
	if b.host != nil {
		lookup.Action, lookup.Function = b.host.Action, b.host.Function
		lookup.Operation, lookup.ValidateQuery = b.host.Operation, b.host.ValidateInstallQuery
		if b.jointMode {
			lookup.ValidateQuery = func(query platform.NamedQuery) error {
				if _, selected := b.jointEntities[query.Object]; selected {
					return query.Check()
				}
				return b.host.ValidateInstallQuery(query)
			}
		}
	}
	return CheckProcess(p, entity, lookup, objects...)
}

func (b *Build) installProcesses() error {
	procs := b.host.BuildProcesses()
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
		if _, err := ReadProcessImage(image); err != nil {
			return err
		}
		for _, raw := range p.Versions {
			was, ok := ReadPublication[Process](raw)
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

func (b *Build) flowOf(p Process) platform.Flow {
	execution := ProcessExecution{
		StateField: "state",
		ContinuousEnabled: func(c platform.Caller) bool {
			if b.host == nil || p.ID == "" || p.Version < 1 {
				return false
			}
			current, ok := platform.Get[Process](b.host.Automation(c, ID), p.ID)
			return ok && current.State == "published" && current.Version == p.Version && current.Name == p.Name
		},
		Subject: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
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
						if _, declared := info.Field(name); declared {
							r.Sources = append(r.Sources, p.Object+"/"+r.Key+"#"+name)
						}
					}
				}
			}
			return raw, err
		},
		SubjectPath: func(c platform.Caller, r *platform.Run, path []string) (json.RawMessage, []string, *kernel.Error) {
			member, known := b.host.Member(r.OnBehalf)
			if !known {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return b.host.Caller(c, member, ID).ReadRecordPath(p.Object, r.Key, path, r.Now)
		},
		Query: func(c platform.Caller, r *platform.Run, app, name string, version int, input json.RawMessage) (json.RawMessage, *kernel.Error) {
			member, ok := b.host.Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return b.host.Caller(c, member, app).ReadQueryVersion(app, name, version, input, r.Now)
		},
	}
	if p.Every == "" && !(p.Continuous != nil && p.Continuous.Intake != nil) && !p.Manual {
		if info, ok := b.lookupEntity(p.Object); ok && info.Lifecycle != nil {
			execution.StateField = info.Lifecycle.Field
		}
	}
	return CompileProcess(p, execution)
}

const (
	ProcessType      = "build.process"
	SchemaProcess    = ProcessType + ".publish"
	SchemaProcessRun = ProcessType + ".run"
)

type Process struct {
	platform.Record
	Name        string                  `json:"name" field:"required,search"`
	Title       string                  `json:"title" field:"required,search"`
	Kind        string                  `json:"kind,omitempty" enum:"flow,automation" title:"Authoring shape"`
	Object      string                  `json:"object,omitempty" title:"Source object"`
	When        string                  `json:"when,omitempty" title:"Record start state"`
	Manual      bool                    `json:"manual,omitempty" title:"Manual start"`
	Every       string                  `json:"every,omitempty" title:"Repeat every" help:"A period such as 15m, 1h or 24h; empty: not scheduled"`
	Scheduler   string                  `json:"scheduler,omitempty" field:"readonly" title:"Runs as"`
	Input       json.RawMessage         `json:"input,omitempty" type:"json" title:"Default input"`
	InputSchema *platform.ValueSchema   `json:"inputSchema,omitempty" type:"json" title:"Input schema"`
	Continuous  *platform.Continuous    `json:"continuous,omitempty" type:"json" title:"Continuous source intake"`
	Steps       []ProcessStep           `json:"steps" field:"aside"`
	Lanes       []ProcessLane           `json:"lanes,omitempty" field:"aside" title:"Lanes"`
	Layout      map[string]NodePosition `json:"layout,omitempty" type:"json" title:"Canvas layout"`
	State       string                  `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                     `json:"version,omitempty" field:"readonly"`
	Published   string                  `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string                `json:"versions,omitempty" field:"readonly"`
}

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

type ProcessStep struct {
	Name           string                      `json:"name"`
	Title          string                      `json:"title,omitempty"`
	Kind           string                      `json:"kind" enum:"payload,query,action,transform,branch,switch,foreach,while,fork,join,ask,wait,subflow,ai,compute,end,fail,break,continue"`
	Lane           string                      `json:"lane,omitempty" title:"Lane" help:"The lane responsible for this step"`
	Inputs         map[string]platform.Binding `json:"inputs,omitempty" type:"json"`
	Value          *platform.Binding           `json:"value,omitempty" type:"json"`
	Target         *platform.Binding           `json:"target,omitempty" type:"json"`
	Condition      *platform.Expr              `json:"condition,omitempty" type:"json"`
	Collection     *platform.Binding           `json:"collection,omitempty" type:"json"`
	WaitUntil      *platform.Binding           `json:"waitUntil,omitempty" type:"json" title:"Wait until timestamp"`
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

type ProcessLookup struct {
	Entity        func(string) (platform.EntityInfo, bool)
	Action        func(string) (string, platform.Action, bool)
	Function      func(string, string, int) (platform.AIFunction, int, bool)
	Operation     func(string, string, int) (platform.Operation, int, bool)
	Query         func(string, int) (platform.NamedQuery, int, bool)
	ValidateQuery func(platform.NamedQuery) error
}

func processPaths(s ProcessStep) []string {
	paths := []string{s.Next, s.Error, s.Body}
	paths = append(paths, s.Branches...)
	paths = append(paths, slices.Collect(maps.Values(s.Cases))...)
	return paths
}

func ReadProcessImage(image []byte) (Process, error) {
	var record Process
	if err := json.Unmarshal(image, &record); err != nil {
		return Process{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return Process{}, fmt.Errorf("accepted process has an invalid version family")
	}
	var latest Process
	for index, raw := range record.Versions {
		version, ok := ReadPublication[Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

type ProcessExecution struct {
	StateField        string
	ContinuousEnabled func(platform.Caller) bool
	Subject           func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error)
	SubjectPath       func(platform.Caller, *platform.Run, []string) (json.RawMessage, []string, *kernel.Error)
	Query             func(platform.Caller, *platform.Run, string, string, int, json.RawMessage) (json.RawMessage, *kernel.Error)
}

func (s *ProcessStep) UnmarshalJSON(raw []byte) error {
	type current ProcessStep
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var node struct {
		current
		Condition json.RawMessage `json:"condition,omitempty"`
	}
	if err := decoder.Decode(&node); err != nil {
		return err
	}
	condition, err := platform.UpgradeCondition(node.Condition)
	if err != nil {
		return err
	}
	*s = ProcessStep(node.current)
	s.Condition = condition
	return nil
}
