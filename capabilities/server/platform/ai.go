package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"time"
)

type Agent struct {
	Name         string
	Title        string
	Description  string
	Instructions string
	Model        string
	ModelOptions ModelOptions
	Tools        []string
	Budget       Budget
	Checkpoints  []string
	Guard        func(c Caller, r AgentRun, action, target string, payload json.RawMessage) *kernel.Error
	To           func(c Caller, r AgentRun) []Recipient
	Cases        []Case
}

type Case struct {
	Name, Goal, Ref string
	Check           func(CaseRun) string
}

type CaseRun struct {
	Actions []string
	Result  string
	Asked   bool
}

type Budget struct {
	Steps, Tokens, Actions int
	Cost                   float64
}

type AgentRun struct {
	ID       string
	Agent    string
	Goal     string
	Ref      string
	OnBehalf string
}

type AgentObservation struct {
	Body    json.RawMessage
	Sources []string
}

type AgentAction struct {
	Owner      string
	Action     Action
	Conditions []string
}

type AgentFunction struct {
	Owner    string
	Function AIFunction
	Call     AgentAction
}

type AgentSurface struct {
	Entities  []EntityInfo
	Actions   []AgentAction
	Functions []AgentFunction
}

type DynamicAgentDefinitions interface {
	App
	AgentStamp() string
	AgentVersion(name string) int
}

type AgentHost interface {
	ApplicationBindings(AssetRef, string, string) ([]AssetBinding, error)
	AppCatalog
	Automation(Caller, string) Caller
	Caller(Caller, Member, string) Caller
	Member(string) (Member, bool)
	OwnerOf(string) (string, bool)
	Record(string) (any, bool)
	Entity(string) (EntityInfo, bool)
	ActiveRelease() string
	AgentTasks() TaskClosure
	AgentFlows() AgentFlowCompletion
	ProtocolActions(string) []Action
	ProtocolProvider(string) (App, string, bool)
	InvokeDecision(Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)
	AgentContext(*Member, string, string, time.Time) (AgentObservation, *kernel.Error)
	AgentSearch(*Member, string, time.Time) AgentObservation
	AgentQuery(Member, NamedQuery, string, time.Time) (AgentObservation, *kernel.Error)
	AgentRead(Member, string, ...json.RawMessage) (AgentObservation, *kernel.Error)
	AgentSurface(Member, string) AgentSurface
	AgentObjects(Member, string, json.RawMessage, time.Time) (AgentObservation, *kernel.Error)
	AgentSources(any) []string
	AgentKnowledge(*Member, string, string, int, time.Time) json.RawMessage
	AgentAppReader(string) *Member
	AgentMeaning(string) string
	AgentGlossary(string) string
	AgentLanguage(string) string
	AgentSuspended(string) bool
	ExternalAgentMembers() []Member
	AgentDecisionMembers() []string
	AgentUsage() []AIUsage
	BeginAgentRead() func()
	ScopeAgentRun(string) func()
	ResolveAgentModel(string, ModelOptions, string) (AIModelBinding, AIProvider, *kernel.Error)
	AgentModel(string) (AIModel, AIProvider, *kernel.Error)
	AgentModelConfigurationDigest(AIModel, AIProvider, *AIModelBinding) (string, error)
	AgentProviderAvailable(string, time.Time) bool
	AgentAnswerText(Outcome) string
	CallAgentModel(AIProvider, AIModel, Member, ChatRequest, time.Time) (ChatAnswer, *AIError)
	MeterAgentUsage(Member, AIUsage) *kernel.Error
	ReplayAgentUsage(AIUsage)
}

type AttachedAgent interface{ AttachAgent(AgentHost) }

type AgentEvaluationDefinitions interface {
	AgentEvaluationApp(candidate, agent string) (App, string, error)
}

type TaskClosure interface {
	Close(Caller, *pb.ChangeRecord, string)
}

type AgentFlowCompletion interface {
	RunEnded(Caller, RunEnd, time.Time)
}

type TrainingEvidenceOwner interface {
	TrainingCandidates(Caller, Event, map[string]json.RawMessage) []AITrainingCandidate
}

const (
	AgentApp          = "agent"
	RunType           = "agent.run"
	AgentAdmin        = "admin"
	SchemaRunStart    = "agent.run.start"
	SchemaToolPropose = "agent.tool.propose"
	SchemaRunStep     = "agent.run.step"
	SchemaRunCancel   = "agent.run.cancel"
	SchemaRunConfirm  = "agent.run.confirm"
	SchemaRunReject   = "agent.run.reject"
	SettingAgentModel = "model"
	SettingAgentDaily = "daily-tokens"
)

type AgentRunRecord struct {
	Record
	Scopes            []string                `json:"scopes,omitempty" field:"readonly"`
	Reviewers         []string                `json:"reviewers,omitempty" field:"readonly"`
	ToolOnly          bool                    `json:"toolOnly,omitempty" field:"readonly"`
	Handoff           string                  `json:"handoff,omitempty" field:"readonly" source:"record"`
	Applications      []AssetBinding          `json:"applications,omitempty" field:"readonly" type:"json"`
	Release           string                  `json:"release,omitempty" field:"readonly" title:"Startup release" source:"record"`
	DefinitionVersion string                  `json:"definitionVersion,omitempty" field:"readonly" title:"Startup agent definition" source:"record"`
	Agent             string                  `json:"agent" field:"readonly,search" source:"record"`
	Title             string                  `json:"title" field:"readonly,search" source:"record"`
	Goal              string                  `json:"goal" field:"readonly" type:"longtext" source:"record"`
	Ref               string                  `json:"ref,omitempty" field:"readonly" source:"record"`
	Seen              string                  `json:"seen,omitempty" field:"readonly" type:"longtext" title:"What it saw of the record"`
	OnBehalf          string                  `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of" source:"record"`
	Acts              bool                    `json:"acts,omitempty" field:"readonly" title:"Acts without drafts"`
	Language          string                  `json:"language,omitempty" field:"readonly" source:"record"`
	Flow              string                  `json:"flow,omitempty" field:"readonly" source:"record"`
	Token             int                     `json:"token,omitempty" field:"readonly"`
	Step              string                  `json:"step,omitempty" field:"readonly" source:"record"`
	State             string                  `json:"state" field:"readonly" choices:"running,waiting,done,stopped"`
	Model             string                  `json:"model,omitempty" field:"readonly" source:"record"`
	ModelBinding      *AIModelBinding         `json:"modelBinding,omitempty" field:"readonly" source:"record" type:"json"`
	Protocol          map[int]json.RawMessage `json:"protocol,omitempty" field:"readonly" type:"json" read:"-"`

	Steps       []RunStep  `json:"steps" field:"readonly" type:"json"`
	Pending     []ToolCall `json:"pending,omitempty" field:"readonly"`
	StepsUsed   int        `json:"stepsUsed" field:"readonly" title:"Model turns"`
	TokensUsed  int        `json:"tokensUsed" field:"readonly" title:"Tokens"`
	ActionsUsed int        `json:"actionsUsed" field:"readonly" title:"Actions"`
	Cost        float64    `json:"cost,omitempty" field:"readonly"`
	Result      string     `json:"result,omitempty" field:"readonly" type:"longtext"`
	Task        string     `json:"task,omitempty" field:"readonly" source:"record"`
	Stopped     string     `json:"stopped,omitempty" field:"readonly" title:"Why it stopped" source:"record"`
	Proposals   []Draft    `json:"proposals,omitempty" field:"readonly" type:"json" read:"-"`
	Draft       []Draft    `json:"draft,omitempty" field:"readonly" title:"Draft to confirm"`
	Citations   []Citation `json:"citations,omitempty" field:"readonly" title:"Sources it read"`
	Signals     []Signal   `json:"signals,omitempty" field:"readonly" title:"What people made of it"`
	Withheld    bool       `json:"withheld,omitempty" field:"readonly" title:"Part of this trace is no longer readable to you"`
}

type Draft struct {
	Authorization   string            `json:"authorization,omitempty"`
	ExpectedSources map[string]uint32 `json:"expectedSources,omitempty"`
	Evidence        string            `json:"evidence,omitempty"`
	Accepted        string            `json:"accepted,omitempty"`
	Rejected        string            `json:"rejected,omitempty"`
	Result          string            `json:"result,omitempty"`
	Outcome         string            `json:"outcome,omitempty"`
	Owner           string            `json:"owner,omitempty"`
	Definition      string            `json:"definition,omitempty"`
	Sources         map[string]uint32 `json:"sources,omitempty"`
	Simulation      string            `json:"simulation,omitempty"`
	Kind            string            `json:"kind"`
	Action          string            `json:"action"`
	Target          string            `json:"target"`
	Type            string            `json:"type,omitempty"`
	Payload         string            `json:"payload"`
	Rationale       string            `json:"rationale,omitempty"`
	Step            int               `json:"step"`
}

type Citation struct {
	Document string `json:"document"`
	Title    string `json:"title" source:"record"`
	Chunk    int    `json:"chunk"`
	Step     int    `json:"step"`
}

type Signal struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	By     string    `json:"by"`
	Detail string    `json:"detail,omitempty"`
	Value  string    `json:"value,omitempty"`
}

type RunStep struct {
	At        time.Time  `json:"at"`
	Tool      string     `json:"tool"`
	Arguments string     `json:"arguments,omitempty"`
	Rationale string     `json:"rationale,omitempty"`
	Outcome   string     `json:"outcome"`
	Tokens    int        `json:"tokens,omitempty"`
	Calls     []ToolCall `json:"calls,omitempty"`
	CallID    string     `json:"callId,omitempty"`
	Content   string     `json:"content,omitempty"`
	Sources   []string   `json:"sources,omitempty" title:"Records it read"`
}

type AgentInfo struct {
	ID           string   `json:"id"`
	App          string   `json:"app"`
	Title        string   `json:"title"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	Budget       Budget   `json:"budget"`
}

const (
	SwitchType    = "agent.switch"
	SchemaSuspend = "agent.suspend"
	SchemaResume  = "agent.resume"
)

type Switch struct {
	Record
	Suspended bool   `json:"suspended" field:"readonly"`
	By        string `json:"by,omitempty" field:"readonly" title:"Changed by"`
	Reason    string `json:"reason,omitempty" field:"readonly" type:"longtext"`
}

type AgentOverview struct {
	Member    string         `json:"member"`
	Title     string         `json:"title"`
	Outside   bool           `json:"outside,omitempty"`
	Suspended bool           `json:"suspended"`
	Runs      int            `json:"runs"`
	Live      int            `json:"live"`
	Actions   int            `json:"actions"`
	Calls     int            `json:"calls"`
	Tokens    int            `json:"tokens"`
	Cost      float64        `json:"cost"`
	Judged    map[string]int `json:"judged"`
}

type AgentResultStep struct {
	Protocol    json.RawMessage `json:"protocol,omitempty"`
	Run         string          `json:"run"`
	Calls       []ToolCall      `json:"calls,omitempty"`
	CallID      string          `json:"callId,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	Content     string          `json:"content,omitempty"`
	Usage       AIUsage         `json:"usage"`
	Failure     string          `json:"failure,omitempty"`
	Stop        string          `json:"stop,omitempty"`
	Observation json.RawMessage `json:"observation,omitempty"`
	Evaluation  *Evaluation     `json:"evaluation,omitempty"`
}

type AgentTurn struct {
	Run      AgentRunRecord
	Model    AIModel
	Provider AIProvider
	Request  ChatRequest
	Stop     string
}

const (
	EvaluationType  = "agent.evaluation"
	SchemaEvalStart = "agent.evaluation.start"
)

type Evaluation struct {
	Record
	Agent            string            `json:"agent" field:"readonly,search"`
	Model            string            `json:"model" field:"readonly,search" title:"Candidate model"`
	State            string            `json:"state" field:"readonly" choices:"queued,done"`
	Agrees           int               `json:"agrees" field:"readonly" title:"Agrees with accepted"`
	Differs          int               `json:"differs" field:"readonly" title:"Differs from accepted"`
	Repeats          int               `json:"repeats" field:"readonly" title:"Repeats a correction"`
	Avoids           int               `json:"avoids" field:"readonly" title:"Avoids a correction"`
	Asks             int               `json:"asks" field:"readonly"`
	Fails            int               `json:"fails" field:"readonly"`
	Score            float64           `json:"score" field:"readonly"`
	Tokens           int               `json:"tokens" field:"readonly"`
	Cost             float64           `json:"cost,omitempty" field:"readonly" title:"Model cost, USD"`
	Cases            []EvalCase        `json:"cases" field:"readonly"`
	Suite            bool              `json:"suite,omitempty" field:"readonly" title:"Declared cases"`
	Candidate        string            `json:"candidate,omitempty" field:"readonly" title:"Release candidate"`
	DefinitionDigest string            `json:"definitionDigest,omitempty" field:"readonly" title:"Evaluated definition"`
	Binding          *AIModelBinding   `json:"binding,omitempty" field:"readonly" type:"json"`
	ModelConfig      string            `json:"modelConfig,omitempty" field:"readonly" title:"Evaluated model configuration"`
	Inputs           []EvaluationInput `json:"inputs,omitempty" field:"readonly"`
}

type EvaluationInput struct {
	Run      string `json:"run"`
	Revision uint32 `json:"revision"`
}

type EvalCase struct {
	Case      string  `json:"case,omitempty"`
	Passes    int     `json:"passes,omitempty"`
	Runs      int     `json:"runs,omitempty"`
	Run       string  `json:"run"`
	Signal    string  `json:"signal"`
	Verdict   string  `json:"verdict"`
	Reference string  `json:"reference"`
	Candidate string  `json:"candidate"`
	Steps     int     `json:"steps"`
	Tokens    int     `json:"tokens"`
	Cost      float64 `json:"cost,omitempty"`
}

const (
	MemoryType = "agent.memory"
)

type Memory struct {
	Record
	Agent    string    `json:"agent" field:"readonly,search"`
	Fact     string    `json:"fact" field:"readonly,search" type:"longtext"`
	For      string    `json:"for,omitempty" field:"readonly" title:"About"`
	Run      string    `json:"run,omitempty" field:"readonly" title:"From run"`
	Sources  []string  `json:"sources,omitempty" field:"readonly" title:"Records behind it"`
	State    string    `json:"state" field:"readonly" choices:"proposed,active,forgotten"`
	Expires  time.Time `json:"expires,omitzero" field:"readonly"`
	Withheld bool      `json:"withheld,omitempty" field:"readonly" title:"Part of this is no longer readable to you"`
}

type AgentRuntime interface {
	Runs
	App
	DeclareAgentApp(App) error
	DueAgentTurns(time.Time) []AgentTurn
	IdleAgentTurn(string)
	AgentPrincipal(string) Member
	AllowsAgentRun(AgentRunRecord) bool
	ObserveAgentKnowledge(AgentRunRecord, json.RawMessage, time.Time) json.RawMessage
	ApplyAgentStep(AgentResultStep, time.Time, bool) *kernel.Error
	ApplyAgentStepCaller(Caller, AgentResultStep, time.Time) *kernel.Error
	AgentEvaluationRuntime() AgentEvaluationRuntime
	RecordAgentSignal(Caller, *pb.ChangeRecord, string, Signal, time.Time)
	AgentEffectEnded(Caller, *pb.ChangeRecord, Effect, Outcome, time.Time)
	AgentDeclaration(string) (AgentCardDeclaration, bool)
	AgentOverview() []AgentOverview
}

type AgentCardDeclaration struct{ App, Title, Description string }

const (
	SettingPublished      = "published"
	SettingTranscriptDays = "transcript-days"
)

func (run AgentRunRecord) SourceRefs() []string {
	out := []string{}
	if run.Ref != "" {
		out = append(out, run.Ref)
	}
	for _, step := range run.Steps {
		out = append(out, step.Sources...)
	}
	for _, c := range run.Citations {
		out = append(out, c.Document)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

type CompiledAgentTool struct {
	Owner  string
	Preset map[string]any
	Inputs []Field
	Query  NamedQuery
	Kind   string
	Schema string
	Target string
	Field
	Tool Tool
}

type CompiledAgent struct {
	App string
	Agent
	Toolset map[string]CompiledAgentTool
}

type AgentEvaluationRuntime struct {
	Tenant           string
	Host             AgentHost
	Definition       func(string) *CompiledAgent
	Compile          func(string, string) (*CompiledAgent, string, error)
	Version          func(*CompiledAgent) string
	Prompt           func(Caller, *CompiledAgent, AgentRunRecord, string, time.Time) ChatRequest
	Principal        func(string) Member
	Reader           func(AgentRunRecord) *Member
	Automation       func(string, bool) Caller
	View             func(AgentRunRecord) AgentRun
	Use              func(Caller, *CompiledAgent, *AgentRunRecord, CompiledAgentTool, map[string]any, json.RawMessage, *RunStep, time.Time) (string, func(*pb.ChangeRecord))
	ObserveKnowledge func(AgentRunRecord, json.RawMessage, time.Time) json.RawMessage
	Compiled         func(AgentRunRecord, *CompiledAgent) *CompiledAgent
}

type AgentEvaluator interface {
	Evaluate(AgentEvaluationRuntime, Evaluation, time.Time, func(*Evaluation) bool) Evaluation
}
