package construction

import (
	"encoding/json"

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
// Kind says how the builder authored it (ADR-0047): a "flow" is the free
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
	// duration such as 15m, 1h or 24h, at least 1m (ADR-0038 E1). A scheduled
	// process has no source record; its steps act as the member who published it.
	Every string `json:"every,omitempty" title:"Repeat every" help:"A period such as 15m, 1h or 24h; empty: not scheduled"`
	// Scheduler is the member who published the schedule: scheduled runs act as them.
	Scheduler   string                `json:"scheduler,omitempty" field:"readonly" title:"Runs as"`
	Input       json.RawMessage       `json:"input,omitempty" type:"json" title:"Default input"`
	InputSchema *platform.ValueSchema `json:"inputSchema,omitempty" type:"json" title:"Input schema"`
	Continuous  *platform.Continuous  `json:"continuous,omitempty" type:"json" title:"Continuous source intake"`
	Steps       []ProcessStep         `json:"steps" field:"aside"`
	// Lanes are the responsibilities the process is drawn across (ADR-0033): a
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
