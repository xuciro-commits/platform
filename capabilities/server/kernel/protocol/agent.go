package protocol

import (
	"encoding/json"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"time"
)

type AgentHost interface {
	platform.AppCatalog
	Automation(platform.Caller, string) platform.Caller
	Member(string) (platform.Member, bool)
	ActiveRelease() string
	AgentSuspended(string) bool
	PermittedActions(platform.Member) []string
	LoadDelegationRecords(platform.Member, string, json.RawMessage, time.Time) []map[string]any
	EventHops() int
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

type AgentRuntime interface {
	platform.Runs
	platform.Service
	ApplyAgentStep(AgentResultStep, time.Time, bool) *platform.Error
	MintWorkerToken(string) string
	ResolveDelegationMCP(string, time.Time) (platform.Member, bool)
	ServeAutoloop(http.ResponseWriter, *http.Request, func() time.Time, func(*http.Request) (platform.Member, bool), func(platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error))
	SweepDelegations(time.Time, func(platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error))
}
