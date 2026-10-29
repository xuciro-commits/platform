package platform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"strings"
	"time"
)

// Flow is a long-running process an app declares: a trigger, then
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
	// record's page shows the processes about it.
	Subject string
	// Owners are roles of the app asked when an instance is stuck: a failed
	// compensation, or a version the code no longer runs.
	Owners []string
	// From moves running instances of the previous version to this one: their
	// step there → the step here (a decision, taken by an administrator).
	From map[string]string
	// Continuous declares a flow an arriving batch advances, instead of one
	// decision starting one instance (plan A).
	Continuous *Continuous
}

// Continuous is a flow's declared batch contract: the real
// source it consumes, the batch and state budgets, and whether dead letters are
// kept. The batch identity, predecessor and watermark travel with each batch;
// the instance's frame keeps what was consumed.
type Continuous struct {
	// Source names the connector or protocol the batches arrive from.
	Source string
	// Entry is the retained Compute step that receives one ready event-time
	// window. Empty keeps the original fold-only delivery contract.
	Entry string `json:"entry,omitempty"`
	// Batch is the most signals one batch may carry; 0 keeps the flow's own
	// budget. A batch over it is refused, never trimmed.
	Batch int
	// State is the most bytes one node's state may hold; 0: no extra bound.
	State int
	// FrameBytes bounds the complete accepted frame, including dead letters
	// and its cursor. Zero retains the historical declaration's budget.
	FrameBytes int
	// CheckpointEvery retains a full sealed checkpoint after this many accepted
	// source batches; zero disables periodic checkpoint snapshots.
	CheckpointEvery int `json:"checkpointEvery,omitempty"`
	// Window declares the native event-time profile. Nil preserves the older
	// scalar fold; a published streaming graph explicitly freezes this rule.
	Window *StreamWindow
	// Intake binds a controlled source record and its event columns. Nil keeps
	// the existing native batch delivery contract.
	Intake *StreamIntake `json:"Intake,omitempty"`
	// DeadLetter keeps signals that could not be folded, with the reason.
	DeadLetter bool
	// DeadLetterMaxRecords and DeadLetterTTLMs bound the retained replay view.
	DeadLetterMaxRecords int `json:"deadLetterMaxRecords,omitempty"`
	DeadLetterTTLMs      int `json:"deadLetterTtlMs,omitempty"`
	// Concurrency bounds actual partition work on the outside preparation lane.
	// Zero preserves sequential historical declarations; at most eight workers.
	Concurrency     int              `json:"concurrency,omitempty"`
	Aggregate       *StreamAggregate `json:"aggregate,omitempty"`
	Threshold       *StreamThreshold `json:"threshold,omitempty"`
	Output          *StreamOutput    `json:"output,omitempty"`
	ErrorPolicy     string           `json:"errorPolicy,omitempty"`
	DeadLetterRetry bool             `json:"deadLetterRetry,omitempty"`
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

// StreamWindow is a bounded state rule owned by the existing Flow frame.
// Millisecond settings retain the source graph's timing contract.
type StreamWindow struct {
	Node     string `json:"node"`
	WindowMS int    `json:"windowMs"`
	SlideMS  int    `json:"slideMs"`
	// WatermarkMS is the event-time grace held back from the observed
	// high-water mark before a slide closes and expired rows are evicted.
	WatermarkMS int                     `json:"watermarkMs"`
	MaxRecords  int                     `json:"maxRecords"`
	Retry       *StreamPreparationRetry `json:"retry,omitempty"`
	LateEvents  string                  `json:"lateEvents"` // sideOutput, accept or reject beyond the retained window and grace
}

// FlowStateArtifact identifies a frozen frame in the original file store.
// The host chooses Ticket and derives the storage key; no path or URL is
// accepted from a source, member or worker.
type FlowStateArtifact struct {
	Tenant   string `json:"tenant"`
	Instance string `json:"instance"`
	Version  int    `json:"version"`
	Ticket   string `json:"ticket"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
}

// Start is what starts an instance: events (actions of the app, or protocol
// events "<protocol id>#<event>" the app consumes) and the decision whether one
// starts an instance, with the instance's key (one running instance per key)
// and its first data.
type Start struct {
	// Manual is a typed, explicitly submitted entry point. It shares the
	// same instance, tokens and accepted-result path as state/event starts.
	Manual bool
	// Continuous starts one stable, long-lived instance for a controlled stream.
	// Enabled binds that start to its current published owner; ending the instance
	// is durable and does not silently create a replacement.
	Continuous bool
	Enabled    func(Caller) bool
	On         []string
	Begin      func(c Caller, e Event) (key string, data any, ok bool)
	// Type and When start an instance the first time a decision brings a
	// record of Type into a state When accepts, whichever record the decision
	// named; its key is the record's ID. Either this or On.
	Type string
	When func(c Caller, record any) bool
	// Every starts an instance once per period ( E1, a tenant's
	// scheduled automation): its key is the period's start time, so a tick
	// never starts the same period twice. At least a minute. OnBehalf is the
	// member its steps act as — the one who published the schedule or stream.
	Every    time.Duration
	OnBehalf string
}

// RetryPolicy is the bounded retry schedule of one retained step. Attempts is
// the total number of tries, including the first; zero keeps the platform default.
type RetryPolicy struct {
	Attempts int
	Backoff  time.Duration
}

// Step is one step: exactly one of Act, Wait, Ask, Call, All, Any or Agent.
// Next is the step after it; Choose, when set, picks it instead and says why.
// An empty next step ends the flow (or the branch).
type Step struct {
	Name  string
	Title string
	// Retry overrides the Flow's default total attempts and exponential delay.
	// Backoff is fixed between attempts when positive; zero uses the default.
	Retry *RetryPolicy
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
	// of the calendar of whom the instance runs for, or the tenant's.
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
	At    func(c Caller, r *Run) (time.Time, *kernel.Error)
}

// Ask gives people a task. Its answer, one of Answers ("done"
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
	// accept: the first answer accepts it, another corrects it,
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

// AgentStep gives one of the app's agents a goal and waits for its
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

// FlowHost is the runtime surface consumed by the original Flow owner.
// Declarations, member reads and execution collaborators remain explicit;
// an instance cannot access unrelated installation or host management APIs.
type FlowHost interface {
	Automation(Caller, string) Caller
	Member(string) (Member, bool)
	Record(string) (any, bool)
	Declares(string) bool
	Calendar(string, Date) Calendar
	Tasks() Tasks
	Runs() Runs
	BindFlow(string, string, int, FlowBinding) (FlowBinding, error)
	FlowExecution(Caller, string, string, int, FlowBinding) (FlowHost, error)
	Invoke(Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *kernel.Error)
	Action(string) (string, Action, bool)
	Caller(Caller, Member, string) Caller
	OwnerOf(string) (string, bool)
	Submit(Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type AttachedFlow interface{ AttachFlow(FlowHost) }

type FlowBinding struct {
	Existing     bool
	Applications []AssetBinding
	Dependencies string
	Release      string
	Assets       []AssetRef
}

// Listener is a platform app delivered other apps' events as owned work, with
// every name an event goes by (its action and the protocol events it is).
type Processes interface {
	Declare(a App) error
	// Install registers a flow an app composed at runtime as its next version;
	// Validate checks one without registering it (#132).
	Install(a App, fl Flow) error
	// HasPublishedFlow checks published membership against the original flow owner.
	HasPublishedFlow(id string) bool
	// InstallationDraft isolates declarations for a release trial, without running instances.
	InstallationDraft() Processes
	Validate(a App, fl Flow) error
	// HasRunningDependency protects a composed flow's source and transitive
	// object dependencies while its instances are running, waiting, compensating or stuck.
	HasRunningDependency(typ string) (bool, error)
	HasUnboundRunningDependency(typ string) (bool, error)
	// Check refuses to start when a running instance needs a version the code no longer declares.
	Check() error
	// Versions are those new instances took during the input being handled
	// (then forgotten), journaled with it; Pin gives a replay those of its entry.
	Versions() map[string]int
	Pin(versions map[string]int)
	RunEnded(c Caller, run RunEnd, now time.Time)
}

// Runs are agent runs a flow's agent step starts and signals.
type Runs interface {
	// Start puts a new run with the decision r.
	Start(c Caller, r *pb.ChangeRecord, run RunStart, now time.Time)
	Signal(c Caller, r *pb.ChangeRecord, run string, s RunSignal, now time.Time)
	// Finished are the IDs of a flow instance's finished runs (at step, when
	// given), oldest first.
	Finished(c Caller, flow, step string) []string
}

// RunStart is an agent run a flow's step starts: agent is "<app>.<name>".
type RunStart struct {
	ID, Agent, Goal, Ref, Flow, Step string
	Token                            int
}

// RunSignal is what people made of a run's result.
type RunSignal struct {
	At               time.Time
	Kind, By, Detail string
}

// RunEnd is how a flow's agent run ended: done, with its result, or stopped.
type RunEnd struct {
	ID, Agent, Flow, State, Result, Stopped string
	Token                                   int
}

// Tasks serves Caller.Assign for every app and closes tasks a platform app
// no longer waits for.

type ProcessDefinitions interface {
	Install(App, Flow) error
	Validate(App, Flow) error
	HasRunningDependency(string) (bool, error)
	HasUnboundRunningDependency(string) (bool, error)
}

// StreamPreparationRetry freezes the source/window preparation policy with
// its native definition. Zero attempts preserves the old single preparation.
// No cursor, action or authoritative state advances while preparation retries.
type StreamPreparationRetry struct {
	Attempts  int `json:"attempts"`
	BackoffMS int `json:"backoffMs"`
}

func (r *StreamPreparationRetry) Check() error {
	if r == nil {
		return nil
	}
	if r.Attempts < 0 || r.Attempts > 20 || r.BackoffMS < 0 || r.BackoffMS > 5000 || (r.Attempts-1)*r.BackoffMS > 30000 {
		return fmt.Errorf("stream preparation retries must fit 20 attempts and 30 seconds of bounded backoff")
	}
	return nil
}
func (r *StreamPreparationRetry) Tries() int {
	if r == nil || r.Attempts == 0 {
		return 1
	}
	return r.Attempts
}

const (
	FlowApp          = "flow"
	FlowInstanceType = "flow.instance"
)

type FlowInstance struct {
	Record
	Applications []AssetBinding             `json:"applications,omitempty" field:"readonly" type:"json"`
	Flow         string                     `json:"flow" field:"readonly,search"` // "<app>.<name>"
	Title        string                     `json:"title" field:"readonly,search"`
	Version      int                        `json:"version" field:"readonly"`
	Dependencies string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release      string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key          string                     `json:"key" field:"readonly,search"`
	Subject      string                     `json:"subject,omitempty" field:"readonly"` // "<type>/<key>" when the flow declares its subject
	State        string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf     string                     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data         string                     `json:"data,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Outputs      map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources      []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld     bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer       string                     `json:"answer,omitempty" field:"readonly"`
	Parent       string                     `json:"parent,omitempty" field:"readonly"` // the instance that called it
	Tokens       []FlowToken                `json:"tokens" field:"readonly" title:"Where it stands"`
	// Batch is the continuous instance's frame: the cursor it
	// consumed through, its watermark, node state and dead letters.
	Batch *FlowBatchFrame `json:"batch,omitempty" field:"readonly" type:"json"`
	// ArtifactCleanup is a durable retirement intent for already-superseded
	// frame/checkpoint objects. The host deletes and acknowledges it idempotently.
	ArtifactCleanup []FlowStateArtifact `json:"artifactCleanup,omitempty" field:"readonly" type:"json"`
	CleanupPending  bool                `json:"cleanupPending,omitempty" field:"readonly"`
	Undo            []FlowUndoEntry     `json:"undo" field:"readonly" title:"To undo"`
	Trace           []FlowTraceLine     `json:"trace" field:"readonly"`
	Seq             int                 `json:"seq" field:"readonly"` // tokens and tasks made, for their IDs
	Resumable       bool                `json:"resumable,omitempty" field:"readonly"`
}

// FlowOutput is an application-bound read-only asset published by the original
// Flow decision. Record history retains each accepted batch version.
type FlowOutput struct {
	Record
	App      string          `json:"app" field:"readonly"`
	Instance string          `json:"instance" field:"readonly"`
	Flow     string          `json:"flow" field:"readonly"`
	Version  int             `json:"version" field:"readonly"`
	Name     string          `json:"name" field:"readonly"`
	Batch    string          `json:"batch" field:"readonly"`
	OnBehalf string          `json:"onBehalf" field:"readonly"`
	Sources  []string        `json:"sources,omitempty" field:"readonly"`
	Value    json.RawMessage `json:"value" field:"readonly" type:"json"`
	Withheld bool            `json:"withheld,omitempty" field:"readonly"`
}

type FlowToken struct {
	ID       int       `json:"id"`
	Step     string    `json:"step"`
	Branch   string    `json:"branch,omitempty"` // the All or Any step it runs in
	Waits    string    `json:"waits,omitempty"`  // ready, retry, wait, ask, call, join, undo, stuck
	Attempts int       `json:"attempts,omitempty"`
	Due      time.Time `json:"due,omitzero"` // a retry, a timeout, or a wait's time
	Task     string    `json:"task,omitempty"`
	Child    string    `json:"child,omitempty"`
	Error    string    `json:"error,omitempty"`
	Parent   int       `json:"parent,omitempty"`
	Frames   []Frame   `json:"frames,omitempty"`
	// Input is token-local data supplied for one step. Prepared continuous
	// windows use it only while the retained entry Compute request is built;
	// the host clears it before the accepted Flow record is persisted.
	Input     json.RawMessage            `json:"input,omitempty"`
	Outputs   map[string]json.RawMessage `json:"outputs,omitempty"`
	Operation string                     `json:"operation,omitempty"`
	Loop      *FlowLoopFrame             `json:"loop,omitempty"`
	Results   map[string]json.RawMessage `json:"results,omitempty"`
}

type FlowLoopFrame struct {
	Items   []json.RawMessage       `json:"items,omitempty"`
	Outer   []string                `json:"outer,omitempty"`
	Next    int                     `json:"next"`
	Results map[int]json.RawMessage `json:"results,omitempty"`
	State   json.RawMessage         `json:"state,omitempty"`
	While   bool                    `json:"while,omitempty"`
}

type FlowUndoEntry struct {
	Step     string          `json:"step"`
	App      string          `json:"app"`
	Protocol string          `json:"protocol,omitempty"`
	Action   string          `json:"action"`
	Target   string          `json:"target"`
	Payload  json.RawMessage `json:"payload"`
}

type FlowTraceLine struct {
	At     time.Time `json:"at"`
	Step   string    `json:"step,omitempty"`
	What   string    `json:"what"`
	Detail string    `json:"detail,omitempty"`
	By     string    `json:"by,omitempty"`
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
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Kind  string   `json:"kind" enum:"action,wait,ask,subflow,fork,agent,transform,branch,foreach,compute,ai,end,break,continue"`
	Next  []string `json:"next"`
	// Chooses: the step's code picks the next one as the instance runs.
	Chooses bool `json:"chooses,omitempty"`
}

type FlowBatchFrame struct {
	Cursor                 string                     `json:"cursor,omitempty"` // the batch identity consumed last
	Predecessor            string                     `json:"predecessor,omitempty"`
	Fingerprint            string                     `json:"fingerprint,omitempty"` // binds that cursor to the accepted batch bytes
	Watermark              time.Time                  `json:"watermark,omitzero"`    // event time through which signals were folded
	Consumed               int                        `json:"consumed"`              // signals folded, ever
	Rejected               int                        `json:"rejected"`              // signals the flow refused, ever
	State                  map[string]json.RawMessage `json:"state,omitempty"`       // node name → its state
	DeadLetters            []FlowDeadLetter           `json:"deadLetters,omitempty"` // what could not be folded, and why
	DeadLetterDropped      int                        `json:"deadLetterDropped,omitempty"`
	DeadLetterSequence     int64                      `json:"deadLetterSequence,omitempty"`
	Workers                int                        `json:"workers,omitempty"`
	OperatorOutputs        map[string]json.RawMessage `json:"operatorOutputs,omitempty"`
	StateBytes             int                        `json:"stateBytes,omitempty"`
	CheckpointEvery        int                        `json:"checkpointEvery,omitempty"`
	BatchesSinceCheckpoint int                        `json:"batchesSinceCheckpoint,omitempty"`
	CheckpointCursor       string                     `json:"checkpointCursor,omitempty"`
	CheckpointArtifact     *FlowStateArtifact         `json:"checkpointArtifact,omitempty"`
	Sealed                 *FlowStateArtifact         `json:"sealed,omitempty"`
	Source                 *FlowSourceCheckpoint      `json:"source,omitempty"`
}

type FlowSourceCheckpoint struct {
	Record      string   `json:"record"`
	Config      string   `json:"config"`
	Cursor      string   `json:"cursor"`
	Sources     []string `json:"sources"`
	Offset      string   `json:"offset,omitempty"`
	Initialized bool     `json:"initialized,omitempty"`
}

type FlowDeadLetter struct {
	Seq        int64     `json:"seq,omitempty"`
	Node       string    `json:"node,omitempty"`
	Batch      string    `json:"batch"`
	Partition  string    `json:"partition,omitempty"`
	Key        string    `json:"key,omitempty"`
	At         time.Time `json:"at,omitzero"`
	RecordedAt time.Time `json:"recordedAt,omitzero"`
	Reason     string    `json:"reason"`
	Value      string    `json:"value,omitempty"`
}

type FlowDeadLetterAsset struct {
	Record
	App      string          `json:"app" field:"readonly"`
	Instance string          `json:"instance" field:"readonly"`
	Flow     string          `json:"flow" field:"readonly"`
	Version  int             `json:"version" field:"readonly"`
	OnBehalf string          `json:"onBehalf" field:"readonly"`
	Sources  []string        `json:"sources,omitempty" field:"readonly"`
	Letter   FlowDeadLetter  `json:"letter" field:"readonly" type:"json"`
	Action   string          `json:"action,omitempty" field:"readonly"`
	Protocol string          `json:"protocol,omitempty" field:"readonly"`
	Target   string          `json:"target,omitempty" field:"readonly"`
	Payload  json.RawMessage `json:"payload,omitempty" field:"readonly" type:"json"`
	Key      string          `json:"key,omitempty" field:"readonly"`
	AsMember bool            `json:"asMember,omitempty" field:"readonly"`
	Replayed bool            `json:"replayed,omitempty" field:"readonly"`
	Withheld bool            `json:"withheld,omitempty" field:"readonly"`
}

type FlowBatch struct {
	ID          string
	Predecessor string
	Signals     []FlowSignal
	Bootstrap   bool                  `json:"bootstrap,omitempty"`
	Tick        bool                  `json:"tick,omitempty"` // host slide tick; never advances the source checkpoint
	Source      *FlowSourceCheckpoint `json:"Source,omitempty"`
}

type FlowSignal struct {
	Key       string
	Partition string
	At        time.Time
	Value     json.RawMessage
}

type FlowBatchOutcome struct {
	Cursor    string    `json:"cursor"`
	Watermark time.Time `json:"watermark,omitzero"`
	Consumed  int       `json:"consumed"`
	Rejected  int       `json:"rejected"`
	StateSize int       `json:"stateSize"`
}

type FlowWindowInput struct {
	Source      string                     `json:"source"`
	Batch       string                     `json:"batch"`
	Predecessor string                     `json:"predecessor,omitempty"`
	Cursor      string                     `json:"cursor"`
	Watermark   time.Time                  `json:"watermark"`
	WindowStart time.Time                  `json:"windowStart"`
	WindowEnd   time.Time                  `json:"windowEnd"`
	EmittedAt   time.Time                  `json:"emittedAt"`
	Consumed    int                        `json:"consumed"`
	Rejected    int                        `json:"rejected"`
	Signals     []FlowSignal               `json:"signals"`
	Outputs     map[string]json.RawMessage `json:"outputs"` // the previous accepted Flow outputs, for stateful Compute steps
}

// FlowBatchStateReference identifies exact bytes in the member-authorized
// frame. Output selects OperatorOutputs instead of State; absent/false retains
// the historical state-reference shape. It never grants file access.
type FlowBatchStateReference struct {
	Instance string `json:"instance"`
	Node     string `json:"node"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Output   bool   `json:"output,omitempty"`
}

// UnmarshalJSON maps old durable token positions into the same current
// native semantics. It only decodes saved facts, never repeats an action.
func (x *FlowInstance) UnmarshalJSON(raw []byte) error {
	type image FlowInstance
	var saved image
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	for index := range saved.Tokens {
		token := &saved.Tokens[index]
		if token.Parent == 0 && token.Branch != "" {
			for _, candidate := range saved.Tokens {
				if candidate.Waits == "join" && candidate.Step == token.Branch {
					token.Parent = candidate.ID
					break
				}
			}
		}
		if node, ok := strings.CutPrefix(token.Step, "_function_"); ok && token.Waits == "wait" {
			var calls map[string]string
			if json.Unmarshal([]byte(saved.Data), &calls) == nil && calls[node] != "" {
				token.Step = node
				token.Waits = "invocation"
				token.Child = calls[node]
			}
		}
	}
	*x = FlowInstance(saved)
	return nil
}

// FlowFrameStore is the existing artifact owner on the outside I/O lane.
// Prepared frames are not accepted state until their original result is saved.
type FlowFrameStore interface {
	Read(FlowStateArtifact) ([]byte, error)
	Seal(instance string, version int, raw []byte, budget int) (FlowStateArtifact, error)
	Discard(FlowStateArtifact) error
}

// ContinuousFlowOwner captures retained definitions while the host holds its
// single-writer lock; opaque preparation and artifact I/O then run outside it.
type ContinuousFlowOwner interface {
	App
	PlanBatch(FlowInstance, Caller) (FlowBatchPreparation, *kernel.Error)
	PlanComputeRetry(FlowInstance, Caller, time.Time) (FlowComputeRetryPreparation, *kernel.Error)
	PlanArtifactCleanup(FlowInstance, time.Time) (FlowArtifactCleanupPreparation, *kernel.Error)
	PlanDeadLetterReplay(FlowInstance, Caller, FlowDeadLetterAsset) (FlowDeadLetterReplayPreparation, *kernel.Error)
	ExpandFrame(FlowInstance, FlowFrameStore) (*FlowBatchFrame, error)
}
type FlowDeadLetterReplayPreparation interface {
	Prepare(*pb.Submission, time.Time, FlowFrameStore) (FlowPreparedBatch, *kernel.Error)
}
type FlowBatchPreparation interface {
	Intake() (string, *StreamIntake, int)
	WindowTick(time.Time, FlowFrameStore) (FlowBatch, bool, *kernel.Error)
	Prepare(FlowBatch, time.Time, FlowFrameStore) (FlowPreparedBatch, *kernel.Error)
}
type FlowPreparedDecision interface {
	Decision() App
	Submission(tenant string) *pb.Submission
	Matches(FlowInstance) bool
}
type FlowPreparedBatch interface {
	FlowPreparedDecision
	Artifact() FlowStateArtifact
	Artifacts() []FlowStateArtifact
	OperationRequest() (OperationRequest, bool)
}
type FlowComputeRetryPreparation interface {
	Instance() string
	Prepare(time.Time, FlowFrameStore) (FlowPreparedComputeRetry, *kernel.Error)
}
type FlowPreparedComputeRetry interface {
	FlowPreparedDecision
	Instance() string
	OperationRequest() OperationRequest
}
type FlowArtifactCleanupPreparation interface {
	FlowPreparedDecision
	Instance() string
	Artifacts() []FlowStateArtifact
}

func FlowBatchKey(instance string, version int, batch string) string {
	return fmt.Sprintf("flow-batch:%x", sha256.Sum256(Raw([]any{instance, version, batch})))
}
