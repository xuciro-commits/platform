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
