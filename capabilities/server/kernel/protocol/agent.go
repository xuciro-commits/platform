package protocol

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"time"
)

type DynamicAgentDefinitions interface {
	platform.Service
	AgentStamp() string
	AgentVersion(name string) int
}

type AgentHost interface {
	ApplicationBindings(platform.AssetRef, string, string) ([]platform.AssetBinding, error)
	platform.AppCatalog
	Automation(platform.Caller, string) platform.Caller
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Member(string) (platform.Member, bool)
	OwnerOf(string) (string, bool)
	Record(string) (any, bool)
	Entity(string) (platform.RecordTypeInfo, bool)
	ActiveRelease() string
	AgentTasks() TaskClosure
	AgentFlows() AgentFlowCompletion
	ProtocolActions(string) []platform.Action
	ProtocolProvider(string) (platform.Service, string, bool)
	InvokeDecision(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *pb.ChangeRecord, *platform.Error)
	AgentContext(*platform.Member, string, string, time.Time) (AgentObservation, *platform.Error)
	AgentSearch(*platform.Member, string, time.Time) AgentObservation
	AgentQuery(platform.Member, platform.NamedQuery, string, time.Time) (AgentObservation, *platform.Error)
	AgentRead(platform.Member, string, ...json.RawMessage) (AgentObservation, *platform.Error)
	AgentSurface(platform.Member, string) platform.AgentSurface
	AgentObjects(platform.Member, string, json.RawMessage, time.Time) (AgentObservation, *platform.Error)
	AgentSources(any) []string
	AgentKnowledge(*platform.Member, string, string, int, time.Time) json.RawMessage
	AgentAppReader(string) *platform.Member
	AgentMeaning(string) string
	AgentGlossary(string) string
	AgentLanguage(string) string
	AgentSuspended(string) bool
	ExternalAgentMembers() []platform.Member
	AgentDecisionMembers() []string
	AgentUsage() []platform.AIUsage
	DynamicAgents() []DynamicAgentDefinitions
	DynamicAgent(string) (DynamicAgentDefinitions, bool)
	WithAgentRead(func(AgentEvaluationRuntime) *platform.Error) *platform.Error
	ResolveAgentModel(string, platform.ModelOptions, string) (platform.AIModelBinding, platform.AIProvider, *platform.Error)
	AgentModel(string) (platform.AIModel, platform.AIProvider, *platform.Error)
	AgentModelConfigurationDigest(platform.AIModel, platform.AIProvider, *platform.AIModelBinding) (string, error)
	AgentProviderAvailable(string, time.Time) bool
	AgentAnswerText(platform.Outcome) string
	CallAgentModel(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	MeterAgentUsage(platform.Member, platform.AIUsage) *platform.Error
	ReplayAgentUsage(platform.AIUsage)
}

type AgentEvaluationDefinitions interface {
	AgentEvaluationApp(candidate, agent string) (platform.Service, string, error)
}

type TaskClosure interface {
	Close(platform.Caller, *pb.ChangeRecord, string)
}

type AgentFlowCompletion interface {
	RunEnded(platform.Caller, platform.RunEnd, time.Time)
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
	Protocol    json.RawMessage     `json:"protocol,omitempty"`
	Run         string              `json:"run"`
	Calls       []platform.ToolCall `json:"calls,omitempty"`
	CallID      string              `json:"callId,omitempty"`
	Tool        string              `json:"tool,omitempty"`
	Arguments   json.RawMessage     `json:"arguments,omitempty"`
	Content     string              `json:"content,omitempty"`
	Usage       platform.AIUsage    `json:"usage"`
	Failure     string              `json:"failure,omitempty"`
	Stop        string              `json:"stop,omitempty"`
	Observation json.RawMessage     `json:"observation,omitempty"`
	Evaluation  *Evaluation         `json:"evaluation,omitempty"`
}

type AgentTurn struct {
	Run      AgentRunRecord
	Model    platform.AIModel
	Provider platform.AIProvider
	Request  platform.ChatRequest
	Stop     string
}

type AgentRuntime interface {
	platform.Runs
	platform.Service
	DeclareAgentApp(platform.Service) error
	DueAgentTurns(time.Time) []AgentTurn
	ClaimAgentTurn(string) bool
	IdleAgentTurn(string)
	AgentPrincipal(string) platform.Member
	AllowsAgentRun(AgentRunRecord) bool
	ObserveAgentKnowledge(AgentRunRecord, json.RawMessage, time.Time) json.RawMessage
	ApplyAgentStep(AgentResultStep, time.Time, bool) *platform.Error
	ApplyAgentStepCaller(platform.Caller, AgentResultStep, time.Time) *platform.Error
	AgentEvaluationRuntime() AgentEvaluationRuntime
	RecordAgentSignal(platform.Caller, *pb.ChangeRecord, string, Signal, time.Time)
	AgentEffectEnded(platform.Caller, *pb.ChangeRecord, platform.Effect, platform.Outcome, time.Time)
	AgentDeclaration(string) (AgentCardDeclaration, bool)
	AgentOverview() []AgentOverview
}

type AgentCardDeclaration struct{ App, Title, Description string }

type CompiledAgentTool struct {
	Owner  string
	Preset map[string]any
	Inputs []platform.Field
	Query  platform.NamedQuery
	Kind   string
	Schema string
	Target string
	platform.Field
	Tool platform.Tool
}

type CompiledAgent struct {
	App string
	platform.Agent
	Toolset map[string]CompiledAgentTool
}

type AgentEvaluationRuntime struct {
	Tenant           string
	Host             AgentHost
	Definition       func(string) *CompiledAgent
	Compile          func(string, string) (*CompiledAgent, string, error)
	Version          func(*CompiledAgent) string
	Prompt           func(platform.Caller, *CompiledAgent, AgentRunRecord, string, time.Time) platform.ChatRequest
	Principal        func(string) platform.Member
	Reader           func(AgentRunRecord) *platform.Member
	Automation       func(string, bool) platform.Caller
	View             func(AgentRunRecord) platform.AgentRun
	Use              func(platform.Caller, *CompiledAgent, *AgentRunRecord, CompiledAgentTool, map[string]any, json.RawMessage, *RunStep, time.Time) (string, func(*pb.ChangeRecord))
	ObserveKnowledge func(AgentRunRecord, json.RawMessage, time.Time) json.RawMessage
	Compiled         func(AgentRunRecord, *CompiledAgent) *CompiledAgent
}

type AgentEvaluator interface {
	Evaluate(AgentEvaluationRuntime, Evaluation, time.Time, func(*Evaluation) bool) Evaluation
}
