package protocol

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"time"
)

type FlowStateArtifact struct {
	Tenant   string `json:"tenant"`
	Instance string `json:"instance"`
	Version  int    `json:"version"`
	Ticket   string `json:"ticket"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
}

type FlowHost interface {
	Automation(platform.Caller, string) platform.Caller
	Member(string) (platform.Member, bool)
	Record(string) (any, bool)
	Declares(string) bool
	Calendar(string, platform.Date) platform.Calendar
	Tasks() platform.Tasks
	Runs() platform.Runs
	BindFlow(string, string, int, FlowBinding) (FlowBinding, error)
	FlowExecution(platform.Caller, string, string, int, FlowBinding) (FlowHost, error)
	Invoke(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *platform.Error)
	Action(string) (string, platform.Action, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	OwnerOf(string) (string, bool)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
}

type FlowBinding struct {
	Existing     bool
	Applications []platform.AssetBinding
	Dependencies string
	Release      string
	Assets       []platform.AssetRef
}

type Processes interface {
	Consumptions(app string) []platform.Consumption
	Declare(a platform.Service) error
	Install(a platform.Service, fl platform.Flow) error
	HasPublishedFlow(id string) bool
	Validate(a platform.Service, fl platform.Flow) error
	HasRunningDependency(typ string) (bool, error)
	HasUnboundRunningDependency(typ string) (bool, error)
	Check() error
	Versions() map[string]int
	Pin(versions map[string]int)
	RunEnded(c platform.Caller, run platform.RunEnd, now time.Time)
}

const (
	FlowApp          = "flow"
	FlowInstanceType = "flow.instance"
)

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
	Frames    []platform.Frame           `json:"frames,omitempty"`
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
	platform.Record
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

type FlowInstance struct {
	platform.Record
	Applications    []platform.AssetBinding    `json:"applications,omitempty" field:"readonly" type:"json"`
	Flow            string                     `json:"flow" field:"readonly,search"`
	Title           string                     `json:"title" field:"readonly,search"`
	Version         int                        `json:"version" field:"readonly"`
	Dependencies    string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release         string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key             string                     `json:"key" field:"readonly,search"`
	Subject         string                     `json:"subject,omitempty" field:"readonly"`
	State           string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf        string                     `json:"onBehalf,omitempty" field:"readonly" title:"Represented member" help:"On behalf of"`
	Data            string                     `json:"data,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Outputs         map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources         []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld        bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer          string                     `json:"answer,omitempty" field:"readonly"`
	Parent          string                     `json:"parent,omitempty" field:"readonly"`
	Tokens          []FlowToken                `json:"tokens" field:"readonly" title:"Execution position" help:"Where it stands"`
	Batch           *FlowBatchFrame            `json:"batch,omitempty" field:"readonly" type:"json"`
	ArtifactCleanup []FlowStateArtifact        `json:"artifactCleanup,omitempty" field:"readonly" type:"json"`
	CleanupPending  bool                       `json:"cleanupPending,omitempty" field:"readonly"`
	Undo            []FlowUndoEntry            `json:"undo" field:"readonly" title:"To undo"`
	Trace           []FlowTraceLine            `json:"trace" field:"readonly"`
	Seq             int                        `json:"seq" field:"readonly"`
	Resumable       bool                       `json:"resumable,omitempty" field:"readonly"`
}

type FlowOutput struct {
	platform.Record
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
