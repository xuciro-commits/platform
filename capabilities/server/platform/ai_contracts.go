package platform

import (
	"encoding/json"
)

type Agent struct {
	Name, Title, Description, Instructions, Model string
	ModelOptions                                  ModelOptions
	Tools                                         []string
	Budget                                        Budget
	Checkpoints                                   []string
	Guard                                         func(c Caller, r AgentRun, action, target string, payload json.RawMessage) *Error
	To                                            func(c Caller, r AgentRun) []Recipient
	Cases                                         []Case
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
	ID, Agent, Goal, Ref, OnBehalf string
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
	Entities  []RecordTypeInfo
	Actions   []AgentAction
	Functions []AgentFunction
}
