package authoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	automateowner "platformserver/services/automate"
)

const (
	ProcessType         = "build.process"
	SchemaProcess       = ProcessType + ".publish"
	SchemaProcessRun    = ProcessType + ".run"
	SchemaProcessPause  = ProcessType + ".pause"
	SchemaProcessResume = ProcessType + ".resume"
	Builder             = "builder"
	User                = "user"
)

type Process struct {
	platform.Record
	Name        string                  `json:"name" field:"required,search"`
	Title       string                  `json:"title" field:"required,search"`
	Kind        string                  `json:"kind,omitempty" enum:"flow,automation" title:"Authoring shape"`
	Object      string                  `json:"object,omitempty" title:"Source object"`
	Trigger     *platform.Expr          `json:"trigger,omitempty" type:"json"`
	Repeat      bool                    `json:"repeat,omitempty"`
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
	LegacyAlert string                  `json:"legacyAlert,omitempty" field:"readonly"`
	PauseReason string                  `json:"pauseReason,omitempty" field:"readonly"`
	Paused      bool                    `json:"paused,omitempty" field:"readonly"`
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
	Name           string                            `json:"name"`
	Title          string                            `json:"title,omitempty"`
	Notify         *automateowner.NotificationEffect `json:"notify,omitempty" type:"json"`
	Kind           string                            `json:"kind" enum:"payload,query,notify,action,transform,branch,switch,foreach,while,fork,join,ask,wait,subflow,ai,compute,end,fail,break,continue"`
	Lane           string                            `json:"lane,omitempty" title:"Lane" help:"The lane responsible for this step"`
	Inputs         map[string]platform.Binding       `json:"inputs,omitempty" type:"json"`
	Value          *platform.Binding                 `json:"value,omitempty" type:"json"`
	Target         *platform.Binding                 `json:"target,omitempty" type:"json"`
	Condition      *platform.Expr                    `json:"condition,omitempty" type:"json"`
	Collection     *platform.Binding                 `json:"collection,omitempty" type:"json"`
	WaitUntil      *platform.Binding                 `json:"waitUntil,omitempty" type:"json" title:"Wait until timestamp"`
	Cases          map[string]string                 `json:"cases,omitempty" type:"json"`
	Branches       []string                          `json:"branches,omitempty"`
	Next           string                            `json:"next,omitempty"`
	Error          string                            `json:"error,omitempty"`
	Ask            string                            `json:"ask,omitempty"`
	Answers        []string                          `json:"answers,omitempty"`
	Act            string                            `json:"act,omitempty"`
	Protocol       string                            `json:"protocol,omitempty"`
	App            string                            `json:"app,omitempty"`
	Query          string                            `json:"query,omitempty"`
	QueryVersion   int                               `json:"queryVersion,omitempty" title:"Retained query version"`
	Function       *platform.FunctionRef             `json:"function,omitempty" type:"json"`
	Operation      *OperationRef                     `json:"operation,omitempty" type:"json"`
	Body           string                            `json:"body,omitempty"`
	MaxIterations  int                               `json:"maxIterations,omitempty"`
	Concurrency    int                               `json:"concurrency,omitempty"`
	Mode           string                            `json:"mode,omitempty" enum:"all,any"`
	TimeoutSeconds int                               `json:"timeoutSeconds,omitempty"`
	UntilSeconds   int                               `json:"untilSeconds,omitempty"`
	RetryAttempts  int                               `json:"retryAttempts,omitempty"`
	RetryBackoffMs int                               `json:"retryBackoffMs,omitempty"`
	Flow           string                            `json:"flow,omitempty"`
	FlowVersion    int                               `json:"flowVersion,omitempty"`
}

type ProcessLookup struct {
	App           string
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
		version, ok := readPublished[Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

type ProcessExecution struct {
	App               string
	FunctionAction    string
	FunctionResult    func(platform.Caller, string) (FunctionOutcome, bool)
	StateField        string
	Refused           func(platform.Caller, *kernel.Error, time.Time)
	Notify            func(platform.Caller, *platform.Run, string, automateowner.NotificationEffect, json.RawMessage) (json.RawMessage, *kernel.Error)
	Paused            func(platform.Caller) bool
	ContinuousEnabled func(platform.Caller) bool
	Subject           func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error)
	SubjectPath       func(platform.Caller, *platform.Run, []string) (json.RawMessage, []string, *kernel.Error)
	Query             func(platform.Caller, *platform.Run, string, string, int, json.RawMessage) (json.RawMessage, *kernel.Error)
}

type FunctionOutcome struct {
	State   string
	Output  string
	Reason  string
	Sources []string
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
