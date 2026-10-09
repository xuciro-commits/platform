package platform

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Flow is a long-running process an app declares (ADR-0020): a trigger, then
// named steps joined by transitions, with Go functions for the logic. The host
// runs each instance as a record of its flow app, acting as the declaring
// app's automation principal on behalf of whoever started it; every step's
// outcome and reason is kept on the instance as its trace. Several versions of
// a flow may be declared: new instances take the highest, running instances
// keep theirs until they end or are moved to the next.
type Flow struct {
	Name    string // unique in the app; the flow is "<app>.<name>"
	Title   string
	Version int // from 1
	Start   Start
	Steps   []Step // an instance starts at the first
	// Subject is the entity type whose record an instance's key names: the
	// record's page shows the processes about it (ADR-0026 D4).
	Subject string
	// Owners are roles of the app asked when an instance is stuck: a failed
	// compensation, or a version the code no longer runs.
	Owners []string
	// From moves running instances of the previous version to this one: their
	// step there → the step here (a decision, taken by an administrator).
	From map[string]string
	// Continuous declares a flow an arriving batch advances, instead of one
	// decision starting one instance (ADR-0047 §13, plan A).
	Continuous *Continuous
}

// Continuous is a flow's declared batch contract (ADR-0047 §13.3): the real
// source it consumes, the batch and state budgets, and whether dead letters are
// kept. The batch identity, predecessor and watermark travel with each batch;
// the instance's frame keeps what was consumed.
type Continuous struct {
	// Source names the connector or protocol the batches arrive from.
	Source string
	// Batch is the most signals one batch may carry; 0 keeps the flow's own
	// budget. A batch over it is refused, never trimmed.
	Batch int
	// State is the most bytes one node's state may hold; 0: no extra bound.
	State int
	// FrameBytes bounds the complete accepted frame, including dead letters
	// and its cursor. Zero retains the historical declaration's budget.
	FrameBytes int
	// Window declares the native event-time profile. Nil preserves the older
	// scalar fold; a published streaming graph explicitly freezes this rule.
	Window *StreamWindow
	// DeadLetter keeps signals that could not be folded, with the reason.
	DeadLetter bool
}

// StreamWindow is a bounded state rule owned by the existing Flow frame.
// Millisecond settings retain the source graph's timing contract.
type StreamWindow struct {
	Node        string `json:"node"`
	WindowMS    int    `json:"windowMs"`
	SlideMS     int    `json:"slideMs"`
	WatermarkMS int    `json:"watermarkMs"`
	MaxRecords  int    `json:"maxRecords"`
	LateEvents  string `json:"lateEvents"` // sideOutput, accept or reject
}

// Start is what starts an instance: events (actions of the app, or protocol
// events "<protocol id>#<event>" the app consumes) and the decision whether one
// starts an instance, with the instance's key (one running instance per key)
// and its first data.
type Start struct {
	// Manual is a typed, explicitly submitted entry point. It shares the
	// same instance, tokens and accepted-result path as state/event starts.
	Manual bool
	On     []string
	Begin  func(c Caller, e Event) (key string, data any, ok bool)
	// Type and When start an instance the first time a decision brings a
	// record of Type into a state When accepts, whichever record the decision
	// named (ADR-0028 D8); its key is the record's ID. Either this or On.
	Type string
	When func(c Caller, record any) bool
	// Every starts an instance once per period (ADR-0057 E1, a tenant's
	// scheduled automation): its key is the period's start time, so a tick
	// never starts the same period twice. At least a minute. OnBehalf is the
	// member its steps act as — the one who published the schedule.
	Every    time.Duration
	OnBehalf string
}

// Step is one step: exactly one of Act, Wait, Ask, Call, All, Any or Agent.
// Next is the step after it; Choose, when set, picks it instead and says why.
// An empty next step ends the flow (or the branch).
type Step struct {
	Name  string
	Title string
	Act   *Act
	Wait  *Wait
	Ask   *Ask
	Call  *Call
	All   []string // branches, by their first step: the flow goes on when all have ended
	Any   []string // branches: the flow goes on when the first has ended; the others are canceled
	Agent *AgentStep
	// Evaluate performs bounded native data mapping/reads. Its output is
	// captured in the instance; accepted-result recovery never evaluates it.
	Evaluate  *Evaluate
	Branch    *Branch
	Loop      *Loop
	Operation *OperationStep
	Invoke    *Invocation
	// End explicitly ends the current scope (an iteration, branch or flow).
	End         bool
	LoopControl string // break or continue, in the innermost explicit loop scope
	// Output captures an Ask/Wait/Call's result before its control edge runs.
	Output func(c Caller, r *Run) (json.RawMessage, *kernel.Error)
	Next   string
	// Choose picks the next step and gives the reason kept in the trace; data
	// it sets on the run is kept.
	Choose func(c Caller, r *Run) (next, reason string)
	// Timeout ends a Wait or an Ask after this long, at OnTimeout.
	Timeout   time.Duration
	OnTimeout string
	// WorkingDays, when set, times the step out after that many working days
	// of the calendar of whom the instance runs for, or the tenant's (ADR-0028 D7).
	WorkingDays int
	// Fault is where an Act goes once its retries are spent; without it the
	// flow compensates: each completed Act's Undo runs, newest first.
	NoRetry bool // a declared terminal failure, without another invocation
	Fault   string
	Undo    *Act
}

// Compensate, as a next step, undoes what the flow did: each completed Act's
// Undo, newest first; the instance ends compensated.
const Compensate = "@compensate"

// Act submits an action as the app, its own or a protocol's.
type Act struct {
	Protocol string // empty: the app's own action
	Action   string
	// AsMember is a builder's explicit reference to an exposed native action.
	// Its caller is the retained initiating member, with current owner grants;
	// it does not borrow the declaring app's automation authority.
	AsMember    bool
	Inputs      func(Caller, *Run) (json.RawMessage, *kernel.Error)
	TargetValue func(Caller, *Run) (string, *kernel.Error)
	// Target and Payload build the submission from the instance.
	Target  func(c Caller, r *Run) string
	Payload func(c Caller, r *Run) any
	// Done keeps what the action answered on the instance (a booking's ID).
	Done func(c Caller, r *Run, target *pb.EntityRef)
}

// Wait ends when an event arrives that Match accepts (On: an action or
// "<protocol id>#<event>"), when Until holds (checked every second), or at the
// time At gives.
type Wait struct {
	On    string
	Match func(c Caller, r *Run, e Event) bool
	Until func(c Caller, r *Run) bool
	At    func(c Caller, r *Run) time.Time
}

// Ask gives people a task (ADR-0017). Its answer, one of Answers ("done"
// without any), is the instance's Answer for the next step to read. When an
// event Match accepts arrives first (On), the task closes and the answer is "event".
type Ask struct {
	Title   func(c Caller, r *Run) string
	Body    func(c Caller, r *Run) string
	Ref     func(c Caller, r *Run) string
	To      func(c Caller, r *Run) []Recipient
	Answers []string
	On      string
	Match   func(c Caller, r *Run, e Event) bool
	// Reviews names the agent step whose proposal this asks a person to
	// accept (ADR-0021 D7): the first answer accepts it, another corrects it,
	// and an event On bypasses it. What people make of it is kept on the run.
	Reviews string
}

// Call runs another of the app's flows and waits for it to end; its end state
// ("done", "compensated", "canceled") is the Answer.
type Call struct {
	Flow    string
	Version int // zero takes the installed version at start; builders pin it
	Data    func(c Caller, r *Run) any
}

// AgentStep gives one of the app's agents a goal (ADR-0021) and waits for its
// run: its result is the Answer. When the run stops (a budget, a guard, no
// model), the flow goes to the step's Fault, or else a person does the step (To).
type AgentStep struct {
	Agent string // the app's agent, by name
	Goal  func(c Caller, r *Run) string
	Ref   func(c Caller, r *Run) string
	To    func(c Caller, r *Run) []Recipient
}

// Run is an instance as a step's functions see it.
type Run struct {
	ID       string
	Now      time.Time // the current accepted decision time, never wall-clock business logic
	Flow     string
	Version  int
	Key      string
	OnBehalf string // who started it
	Release  string // the activation retained when this instance started
	Sequence int    // the next native action sequence within this instance
	Data     json.RawMessage
	Answer   string                     // the last Ask's or Call's
	Event    *Event                     // the event that started it or ended the last wait
	Sources  []string                   // protected inputs retained across downstream nodes
	Outputs  map[string]json.RawMessage // completed nodes visible in this token's scope
	Frames   []Frame                    // durable enclosing iteration scopes, outermost first
}

// Evaluate and Branch are native step semantics, not a second interpreter.
// Declarative bindings compile to these callbacks in their definition owner.
type Evaluate struct {
	Run func(Caller, *Run) (json.RawMessage, *kernel.Error)
}

type Branch struct {
	Choose func(Caller, *Run) (string, string, *kernel.Error)
	Paths  []string // every statically checked control destination
}

// Loop names an explicit child scope. Its cursor/items/results are persisted
// on the existing flow Token, including while loops that suspend in a body.
type Loop struct {
	Initial       func(Caller, *Run) (json.RawMessage, *kernel.Error)
	Body          string
	Items         func(Caller, *Run) ([]json.RawMessage, *kernel.Error)
	While         func(Caller, *Run) (bool, *kernel.Error)
	MaxIterations int
	Concurrency   int
}

type Frame struct {
	Node  string          `json:"node"`
	Index int             `json:"index"`
	Item  json.RawMessage `json:"item,omitempty"`
}

type OperationStep struct {
	Request func(Caller, *Run) (OperationRequest, *kernel.Error)
}

// Invocation submits an existing owner action once, then reads its durable
// reply without creating another process or inventing an effect mechanism.
type Invocation struct {
	Act    Act
	Result func(Caller, *Run, string) (json.RawMessage, bool, *kernel.Error)
}

// Set replaces the instance's data.
func (r *Run) Set(v any) {
	r.Data, _ = json.Marshal(v)
}

// DataOf reads an instance's data as T.
func DataOf[T any](r *Run) T {
	var v T
	json.Unmarshal(r.Data, &v)
	return v
}
