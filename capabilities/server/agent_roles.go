package platformserver

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// AgentRuntime is the runtime consumer contract of the agent owner. It exposes
// immutable turns and decisions, never definitions or mutable bookkeeping.
type AgentRuntime interface {
	platform.App
	DeclareAgentApp(platform.App) error
	DueAgentTurns(time.Time) []AgentTurn
	IdleAgentTurn(string)
	AgentPrincipal(string) platform.Member
	AllowsAgentRun(AgentRunRecord) bool
	ObserveAgentKnowledge(AgentRunRecord, json.RawMessage, time.Time) json.RawMessage
	ApplyAgentStep(stepBody, time.Time, bool) *kernel.Error
	ApplyAgentStepCaller(platform.Caller, stepBody, time.Time) *kernel.Error
	EvaluateAgent(Evaluation, time.Time, func(*Evaluation) bool) Evaluation
	RecordAgentSignal(platform.Caller, *pb.ChangeRecord, string, Signal, time.Time)
	AgentEffectEnded(platform.Caller, *pb.ChangeRecord, platform.Effect, platform.Outcome, time.Time)
	AgentDeclaration(string) (AgentCardDeclaration, bool)
	AgentOverview() []AgentOverview
}

var _ AgentRuntime = (*Agents)(nil)

// AgentCardDeclaration is the immutable A2A publication projection.
type AgentCardDeclaration struct{ App, Title, Description string }
