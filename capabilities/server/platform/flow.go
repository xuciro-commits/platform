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

type Flow struct {
	Name       string
	Title      string
	Version    int
	Start      Start
	Steps      []Step
	Subject    string
	Owners     []string
	From       map[string]string
	Continuous *Continuous
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

type FlowStateArtifact struct {
	Tenant   string `json:"tenant"`
	Instance string `json:"instance"`
	Version  int    `json:"version"`
	Ticket   string `json:"ticket"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
}

type Start struct {
	Manual     bool
	Continuous bool
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
	Output      func(c Caller, r *Run) (json.RawMessage, *kernel.Error)
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
	Inputs      func(Caller, *Run) (json.RawMessage, *kernel.Error)
	TargetValue func(Caller, *Run) (string, *kernel.Error)
	Target      func(c Caller, r *Run) string
	Payload     func(c Caller, r *Run) any
	Done        func(c Caller, r *Run, target *pb.EntityRef)
}

type Wait struct {
	On    string
	Match func(c Caller, r *Run, e Event) bool
	Until func(c Caller, r *Run) bool
	At    func(c Caller, r *Run) (time.Time, *kernel.Error)
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
	Run func(Caller, *Run) (json.RawMessage, *kernel.Error)
}

type Branch struct {
	Choose func(Caller, *Run) (string, string, *kernel.Error)
	Paths  []string
}

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

type Invocation struct {
	Act    Act
	Result func(Caller, *Run, string) (json.RawMessage, bool, *kernel.Error)
}

func (r *Run) Set(v any) {
	r.Data, _ = json.Marshal(v)
}

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

type Processes interface {
	Consumptions(app string) []Consumption
	Declare(a App) error
	Install(a App, fl Flow) error
	HasPublishedFlow(id string) bool
	InstallationDraft() Processes
	Validate(a App, fl Flow) error
	HasRunningDependency(typ string) (bool, error)
	HasUnboundRunningDependency(typ string) (bool, error)
	Check() error
	Versions() map[string]int
	Pin(versions map[string]int)
	RunEnded(c Caller, run RunEnd, now time.Time)
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
	Install(App, Flow) error
	Validate(App, Flow) error
	HasRunningDependency(string) (bool, error)
	HasUnboundRunningDependency(string) (bool, error)
}

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
	Applications    []AssetBinding             `json:"applications,omitempty" field:"readonly" type:"json"`
	Flow            string                     `json:"flow" field:"readonly,search"`
	Title           string                     `json:"title" field:"readonly,search"`
	Version         int                        `json:"version" field:"readonly"`
	Dependencies    string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release         string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key             string                     `json:"key" field:"readonly,search"`
	Subject         string                     `json:"subject,omitempty" field:"readonly"`
	State           string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf        string                     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data            string                     `json:"data,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Outputs         map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources         []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld        bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer          string                     `json:"answer,omitempty" field:"readonly"`
	Parent          string                     `json:"parent,omitempty" field:"readonly"`
	Tokens          []FlowToken                `json:"tokens" field:"readonly" title:"Where it stands"`
	Batch           *FlowBatchFrame            `json:"batch,omitempty" field:"readonly" type:"json"`
	ArtifactCleanup []FlowStateArtifact        `json:"artifactCleanup,omitempty" field:"readonly" type:"json"`
	CleanupPending  bool                       `json:"cleanupPending,omitempty" field:"readonly"`
	Undo            []FlowUndoEntry            `json:"undo" field:"readonly" title:"To undo"`
	Trace           []FlowTraceLine            `json:"trace" field:"readonly"`
	Seq             int                        `json:"seq" field:"readonly"`
	Resumable       bool                       `json:"resumable,omitempty" field:"readonly"`
}

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
	ID        int                        `json:"id"`
	Step      string                     `json:"step"`
	Branch    string                     `json:"branch,omitempty"`
	Waits     string                     `json:"waits,omitempty"`
	Attempts  int                        `json:"attempts,omitempty"`
	Due       time.Time                  `json:"due,omitzero"`
	Task      string                     `json:"task,omitempty"`
	Child     string                     `json:"child,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Parent    int                        `json:"parent,omitempty"`
	Frames    []Frame                    `json:"frames,omitempty"`
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
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Kind    string   `json:"kind" enum:"action,wait,ask,subflow,fork,agent,transform,branch,foreach,compute,ai,end,break,continue"`
	Next    []string `json:"next"`
	Chooses bool     `json:"chooses,omitempty"`
}

type FlowBatchFrame struct {
	Cursor                 string                     `json:"cursor,omitempty"`
	Predecessor            string                     `json:"predecessor,omitempty"`
	Fingerprint            string                     `json:"fingerprint,omitempty"`
	Watermark              time.Time                  `json:"watermark,omitzero"`
	Consumed               int                        `json:"consumed"`
	Rejected               int                        `json:"rejected"`
	State                  map[string]json.RawMessage `json:"state,omitempty"`
	DeadLetters            []FlowDeadLetter           `json:"deadLetters,omitempty"`
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
	Tick        bool                  `json:"tick,omitempty"`
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
	Outputs     map[string]json.RawMessage `json:"outputs"`
}

type FlowBatchStateReference struct {
	Instance string `json:"instance"`
	Node     string `json:"node"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Output   bool   `json:"output,omitempty"`
}

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

type FlowFrameStore interface {
	Read(FlowStateArtifact) ([]byte, error)
	Seal(instance string, version int, raw []byte, budget int) (FlowStateArtifact, error)
	Discard(FlowStateArtifact) error
}

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
