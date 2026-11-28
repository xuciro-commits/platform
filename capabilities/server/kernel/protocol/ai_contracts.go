package protocol

import (
	"encoding/json"
	"platformserver/platform"
	"slices"
	"time"
)

type TrainingEvidenceOwner interface {
	TrainingCandidates(platform.Caller, platform.Event, map[string]json.RawMessage) []AITrainingCandidate
}

const (
	AgentApp                  = "agent"
	RunType                   = "agent.run"
	AgentAdmin                = "admin"
	SettingAgentModel         = "model"
	SettingAgentDaily         = "daily-tokens"
	SchemaRunCancel           = "agent.run.cancel"
	DelegationType            = "agent.delegation"
	DelegationWorkerType      = "agent.worker"
	DelegationEventType       = "agent.delegation-event"
	DelegationJobType         = "agent.delegation-job"
	SchemaDelegationCreate    = "agent.delegation.create"
	SchemaDelegationEdit      = "agent.delegation.edit"
	SchemaDelegationPause     = "agent.delegation.pause"
	SchemaDelegationResume    = "agent.delegation.resume"
	SchemaDelegationSweep     = "agent.delegation.sweep"
	SchemaDelegationWorkerKey = "agent.delegation.worker-key"
	SchemaDelegationIngest    = "agent.delegation.ingest"
	SchemaDelegationClaim     = "agent.delegation.claim"
	SchemaDelegationStarted   = "agent.delegation.started"
	SchemaDelegationReceipt   = "agent.delegation.receipt"
)

type AgentRunRecord struct {
	platform.Record
	Scopes            []string                 `json:"scopes,omitempty" field:"readonly"`
	Reviewers         []string                 `json:"reviewers,omitempty" field:"readonly"`
	ToolOnly          bool                     `json:"toolOnly,omitempty" field:"readonly"`
	Handoff           string                   `json:"handoff,omitempty" field:"readonly" source:"record"`
	Applications      []platform.AssetBinding  `json:"applications,omitempty" field:"readonly" type:"json"`
	Release           string                   `json:"release,omitempty" field:"readonly" title:"Startup release" source:"record"`
	DefinitionVersion string                   `json:"definitionVersion,omitempty" field:"readonly" title:"Agent definition" help:"Startup agent definition" source:"record"`
	Agent             string                   `json:"agent" field:"readonly,search" source:"record"`
	Title             string                   `json:"title" field:"readonly,search" source:"record"`
	Goal              string                   `json:"goal" field:"readonly" type:"longtext" source:"record"`
	Ref               string                   `json:"ref,omitempty" field:"readonly" source:"record"`
	Seen              string                   `json:"seen,omitempty" field:"readonly" type:"longtext" title:"Record snapshot" help:"What it saw of the record"`
	OnBehalf          string                   `json:"onBehalf,omitempty" field:"readonly" title:"Represented member" help:"On behalf of" source:"record"`
	Acts              bool                     `json:"acts,omitempty" field:"readonly" title:"Direct actions" help:"Acts without drafts"`
	Language          string                   `json:"language,omitempty" field:"readonly" source:"record"`
	Flow              string                   `json:"flow,omitempty" field:"readonly" source:"record"`
	Token             int                      `json:"token,omitempty" field:"readonly"`
	Step              string                   `json:"step,omitempty" field:"readonly" source:"record"`
	State             string                   `json:"state" field:"readonly" choices:"running,waiting,done,stopped"`
	Model             string                   `json:"model,omitempty" field:"readonly" source:"record"`
	ModelBinding      *platform.AIModelBinding `json:"modelBinding,omitempty" field:"readonly" source:"record" type:"json"`
	Protocol          map[int]json.RawMessage  `json:"protocol,omitempty" field:"readonly" type:"json" read:"-"`

	Steps       []RunStep           `json:"steps" field:"readonly" type:"json"`
	Pending     []platform.ToolCall `json:"pending,omitempty" field:"readonly"`
	StepsUsed   int                 `json:"stepsUsed" field:"readonly" title:"Model turns"`
	TokensUsed  int                 `json:"tokensUsed" field:"readonly" title:"Tokens"`
	ActionsUsed int                 `json:"actionsUsed" field:"readonly" title:"Actions"`
	Cost        float64             `json:"cost,omitempty" field:"readonly"`
	Result      string              `json:"result,omitempty" field:"readonly" type:"longtext"`
	Task        string              `json:"task,omitempty" field:"readonly" source:"record"`
	Stopped     string              `json:"stopped,omitempty" field:"readonly" title:"Stop reason" help:"Why it stopped" source:"record"`
	Proposals   []Draft             `json:"proposals,omitempty" field:"readonly" type:"json" read:"-"`
	Draft       []Draft             `json:"draft,omitempty" field:"readonly" title:"Confirmation draft" help:"Draft to confirm"`
	Citations   []Citation          `json:"citations,omitempty" field:"readonly" title:"Read sources" help:"Sources it read"`
	Signals     []Signal            `json:"signals,omitempty" field:"readonly" title:"Feedback" help:"What people made of it"`
	Withheld    bool                `json:"withheld,omitempty" field:"readonly" title:"Withheld trace" help:"Part of this trace is no longer readable to you"`
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
	At        time.Time           `json:"at"`
	Tool      string              `json:"tool"`
	Arguments string              `json:"arguments,omitempty"`
	Rationale string              `json:"rationale,omitempty"`
	Outcome   string              `json:"outcome"`
	Tokens    int                 `json:"tokens,omitempty"`
	Calls     []platform.ToolCall `json:"calls,omitempty"`
	CallID    string              `json:"callId,omitempty"`
	Content   string              `json:"content,omitempty"`
	Sources   []string            `json:"sources,omitempty" title:"Read records" help:"Records it read"`
}

const (
	SwitchType    = "agent.switch"
	SchemaSuspend = "agent.suspend"
	SchemaResume  = "agent.resume"
)

type Switch struct {
	platform.Record
	Suspended bool   `json:"suspended" field:"readonly"`
	By        string `json:"by,omitempty" field:"readonly" title:"Changed by"`
	Reason    string `json:"reason,omitempty" field:"readonly" type:"longtext"`
}

const (
	EvaluationType = "agent.evaluation"
)

type Evaluation struct {
	platform.Record
	Agent            string                   `json:"agent" field:"readonly,search"`
	Model            string                   `json:"model" field:"readonly,search" title:"Candidate model"`
	State            string                   `json:"state" field:"readonly" choices:"queued,done"`
	Agrees           int                      `json:"agrees" field:"readonly" title:"Agreements" help:"Agrees with accepted"`
	Differs          int                      `json:"differs" field:"readonly" title:"Differences" help:"Differs from accepted"`
	Repeats          int                      `json:"repeats" field:"readonly" title:"Repeated corrections" help:"Repeats a correction"`
	Avoids           int                      `json:"avoids" field:"readonly" title:"Corrections avoided" help:"Avoids a correction"`
	Asks             int                      `json:"asks" field:"readonly"`
	Fails            int                      `json:"fails" field:"readonly"`
	Score            float64                  `json:"score" field:"readonly"`
	Tokens           int                      `json:"tokens" field:"readonly"`
	Cost             float64                  `json:"cost,omitempty" field:"readonly" title:"Cost (USD)" help:"Model cost, USD"`
	Cases            []EvalCase               `json:"cases" field:"readonly"`
	Suite            bool                     `json:"suite,omitempty" field:"readonly" title:"Declared cases"`
	Candidate        string                   `json:"candidate,omitempty" field:"readonly" title:"Release candidate"`
	DefinitionDigest string                   `json:"definitionDigest,omitempty" field:"readonly" title:"Evaluated definition"`
	Binding          *platform.AIModelBinding `json:"binding,omitempty" field:"readonly" type:"json"`
	ModelConfig      string                   `json:"modelConfig,omitempty" field:"readonly" title:"Model configuration" help:"Evaluated model configuration"`
	Inputs           []EvaluationInput        `json:"inputs,omitempty" field:"readonly"`
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
	platform.Record
	Agent    string    `json:"agent" field:"readonly,search"`
	Fact     string    `json:"fact" field:"readonly,search" type:"longtext"`
	For      string    `json:"for,omitempty" field:"readonly" title:"About"`
	Run      string    `json:"run,omitempty" field:"readonly" title:"From run"`
	Sources  []string  `json:"sources,omitempty" field:"readonly" title:"Source records" help:"Records behind it"`
	State    string    `json:"state" field:"readonly" choices:"proposed,active,forgotten"`
	Expires  time.Time `json:"expires,omitzero" field:"readonly"`
	Withheld bool      `json:"withheld,omitempty" field:"readonly" title:"Withheld data" help:"Part of this is no longer readable to you"`
}

const (
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
