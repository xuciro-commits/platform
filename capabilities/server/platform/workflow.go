package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

type Flow struct {
	Name, Title string
	Version     int
	Start       Start
	Steps       []Step
	Subject     string
	Owners      []string
	From        map[string]string
	Continuous  *Continuous
}

type Continuous struct {
	Source               string
	Entry                string `json:"entry,omitempty"`
	Batch                int
	State                int
	FrameBytes           int
	CheckpointEvery      int `json:"checkpointEvery,omitempty"`
	Window               *StreamWindow
	Intake               *StreamIntake `json:"Intake,omitempty"`
	DeadLetter           bool
	DeadLetterMaxRecords int              `json:"deadLetterMaxRecords,omitempty"`
	DeadLetterTTLMs      int              `json:"deadLetterTtlMs,omitempty"`
	Concurrency          int              `json:"concurrency,omitempty"`
	Aggregate            *StreamAggregate `json:"aggregate,omitempty"`
	Threshold            *StreamThreshold `json:"threshold,omitempty"`
	Output               *StreamOutput    `json:"output,omitempty"`
	ErrorPolicy          string           `json:"errorPolicy,omitempty"`
	DeadLetterRetry      bool             `json:"deadLetterRetry,omitempty"`
}

type StreamAggregate struct {
	Node     string   `json:"node"`
	Kernel   string   `json:"kernel,omitempty"`
	Above    float64  `json:"above,omitempty"`
	Signal   string   `json:"signal,omitempty"`
	Group    []string `json:"group,omitempty"`
	Measures []string `json:"measures"`
}

type StreamThreshold struct {
	Node       string  `json:"node"`
	Field      string  `json:"field"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	DebounceMS int     `json:"debounceMs"`
	Severity   string  `json:"severity,omitempty"`
}

type StreamOutput struct {
	Node string `json:"node"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

type StreamIntake struct {
	SourceRecord string                  `json:"sourceRecord"`
	Key          string                  `json:"key"`
	Partition    []string                `json:"partition"`
	EventTime    string                  `json:"eventTime"`
	Value        string                  `json:"value"`
	Offset       string                  `json:"offset,omitempty" enum:"latest,earliest"`
	Fields       []string                `json:"fields,omitempty"`
	Retry        *StreamPreparationRetry `json:"retry,omitempty"`
}

type StreamWindow struct {
	Node        string                  `json:"node"`
	WindowMS    int                     `json:"windowMs"`
	SlideMS     int                     `json:"slideMs"`
	WatermarkMS int                     `json:"watermarkMs"`
	MaxRecords  int                     `json:"maxRecords"`
	Retry       *StreamPreparationRetry `json:"retry,omitempty"`
	LateEvents  string                  `json:"lateEvents"`
}

type Start struct {
	Manual     bool
	Repeat     bool
	Refused    func(Caller, *Error, time.Time)
	Continuous bool
	Paused     func(Caller) bool
	Enabled    func(Caller) bool
	On         []string
	Begin      func(c Caller, e Event) (key string, data any, ok bool)
	Type       string
	When       func(c Caller, record any) bool
	Every      time.Duration
	OnBehalf   string
}

type RetryPolicy struct {
	Attempts int
	Backoff  time.Duration
}

type Step struct {
	Name        string
	Title       string
	Retry       *RetryPolicy
	Act         *Act
	Wait        *Wait
	Ask         *Ask
	Call        *Call
	All         []string
	Any         []string
	Agent       *AgentStep
	Evaluate    *Evaluate
	Branch      *Branch
	Loop        *Loop
	Operation   *OperationStep
	Invoke      *Invocation
	End         bool
	LoopControl string
	Output      func(c Caller, r *Run) (json.RawMessage, *Error)
	Next        string
	Choose      func(c Caller, r *Run) (next, reason string)
	Timeout     time.Duration
	OnTimeout   string
	WorkingDays int
	NoRetry     bool
	Fault       string
	Undo        *Act
}

const Compensate = "@compensate"

type Act struct {
	Protocol    string
	Action      string
	AsMember    bool
	Inputs      func(Caller, *Run) (json.RawMessage, *Error)
	TargetValue func(Caller, *Run) (string, *Error)
	Target      func(c Caller, r *Run) string
	Payload     func(c Caller, r *Run) any
	Done        func(c Caller, r *Run, target *pb.EntityRef)
}

type Wait struct {
	On    string
	Match func(c Caller, r *Run, e Event) bool
	Until func(c Caller, r *Run) bool
	At    func(c Caller, r *Run) (time.Time, *Error)
}

type Ask struct {
	Title   func(c Caller, r *Run) string
	Body    func(c Caller, r *Run) string
	Ref     func(c Caller, r *Run) string
	To      func(c Caller, r *Run) []Recipient
	Answers []string
	On      string
	Match   func(c Caller, r *Run, e Event) bool
	Reviews string
}

type Call struct {
	Flow    string
	Version int
	Data    func(c Caller, r *Run) any
}

type AgentStep struct {
	Agent string
	Goal  func(c Caller, r *Run) string
	Ref   func(c Caller, r *Run) string
	To    func(c Caller, r *Run) []Recipient
}

type Run struct {
	ID       string
	Now      time.Time
	Flow     string
	Version  int
	Key      string
	Subject  string
	OnBehalf string
	Release  string
	Sequence int
	Data     json.RawMessage
	Answer   string
	Event    *Event
	Sources  []string
	Outputs  map[string]json.RawMessage
	Frames   []Frame
}

type Evaluate struct {
	Run func(Caller, *Run) (json.RawMessage, *Error)
}

type Branch struct {
	Choose func(Caller, *Run) (string, string, *Error)
	Paths  []string
}

type Loop struct {
	Initial       func(Caller, *Run) (json.RawMessage, *Error)
	Body          string
	Items         func(Caller, *Run) ([]json.RawMessage, *Error)
	While         func(Caller, *Run) (bool, *Error)
	MaxIterations int
	Concurrency   int
}

type Frame struct {
	Node  string          `json:"node"`
	Index int             `json:"index"`
	Item  json.RawMessage `json:"item,omitempty"`
}

type OperationStep struct {
	Request func(Caller, *Run) (OperationRequest, *Error)
}

type Invocation struct {
	Act    Act
	Result func(Caller, *Run, string) (json.RawMessage, bool, *Error)
}

func (r *Run) Set(v any) {
	r.Data, _ = json.Marshal(v)
}

type Runs interface {
	Start(c Caller, r *pb.ChangeRecord, run RunStart, now time.Time)
	Signal(c Caller, r *pb.ChangeRecord, run string, s RunSignal, now time.Time)
	Finished(c Caller, flow, step string) []string
}

type RunStart struct {
	ID, Agent, Goal, Ref, Flow, Step string
	Token                            int
}

type RunSignal struct {
	At               time.Time
	Kind, By, Detail string
}

type RunEnd struct {
	ID, Agent, Flow, State, Result, Stopped string
	Token                                   int
}

type ProcessDefinitions interface {
	Install(Service, Flow) error
	Validate(Service, Flow) error
	HasRunningDependency(string) (bool, error)
	HasUnboundRunningDependency(string) (bool, error)
}

type StreamPreparationRetry struct {
	Attempts  int `json:"attempts"`
	BackoffMS int `json:"backoffMs"`
}

type FlowDefinition struct {
	ID      string     `json:"id"`
	App     string     `json:"app"`
	Title   string     `json:"title"`
	Version int        `json:"version"`
	Start   []string   `json:"start"`
	Steps   []FlowStep `json:"steps"`
}

type FlowStep struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Kind    string   `json:"kind" enum:"action,wait,ask,subflow,fork,agent,transform,branch,foreach,compute,ai,end,break,continue"`
	Next    []string `json:"next"`
	Chooses bool     `json:"chooses,omitempty"`
}
