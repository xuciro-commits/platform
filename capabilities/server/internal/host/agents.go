package host

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// AgentObservation is one original authorized read with its dependency refs.
// It carries immutable response bytes, not a second query or state engine.
type AgentObservation struct {
	Body    json.RawMessage
	Sources []string
}

// DynamicAgentDefinitions owns the publication version of controlled agents.
// The agent runtime does not depend on a concrete builder implementation.
type DynamicAgentDefinitions interface {
	platform.App
	AgentStamp() string
	AgentVersion(name string) int
}

// AgentHost is the execution and read context actually consumed by Agents.
// The owner never receives the tenant's component fields or record-store locks.
type AgentHost interface {
	ApplicationBindings(platform.AssetRef, string, string) ([]platform.AssetBinding, error)
	AppCatalog
	Automation(platform.Caller, string) platform.Caller
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Member(string) (platform.Member, bool)
	OwnerOf(string) (string, bool)
	Record(string) (any, bool)
	Entity(string) (platform.EntityInfo, bool)
	ActiveRelease() string
	AgentTasks() TaskClosure
	AgentFlows() AgentFlowCompletion
	ProtocolActions(string) []platform.Action
	ProtocolProvider(string) (platform.App, string, bool)
	InvokeDecision(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error)
	AgentContext(*platform.Member, string, string, time.Time) (AgentObservation, *kernel.Error)
	AgentSearch(*platform.Member, string, time.Time) AgentObservation
	AgentQuery(platform.Member, string, string, string, time.Time) (AgentObservation, *kernel.Error)
	AgentRead(platform.Member, string) (AgentObservation, *kernel.Error)
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
	BeginAgentRead() func()
	ScopeAgentRun(string) func()
	AgentModel(string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	ReserveAgentModel(platform.Member, platform.AIModel, time.Time) string
	AgentProviderAvailable(string, time.Time) bool
	CallAgentModel(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	MeterAgentUsage(platform.Member, platform.AIUsage) *kernel.Error
	ReplayAgentUsage(platform.AIUsage)
}

type AttachedAgent interface{ AttachAgent(AgentHost) }

// Candidate declarations are compiled by their original owner. Evaluations
// retain this immutable selection while all reads still use current grants.
type AgentEvaluationDefinitions interface {
	AgentEvaluationApp(candidate, agent string) (platform.App, string, error)
}

// The agent owner closes its own waiting task and reports a completed run;
// it cannot install, pin or redeclare a flow through either collaborator.
type TaskClosure interface {
	Close(platform.Caller, *pb.ChangeRecord, string)
}
type AgentFlowCompletion interface {
	RunEnded(platform.Caller, RunEnd, time.Time)
}
