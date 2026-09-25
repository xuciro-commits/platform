package platform

import (
	"encoding/json"

	"platformkernel/kernel"
)

// Agent is an AI agent an app declares (ADR-0021): instructions, the tools it
// may use, budgets, guards and the people it hands its goal to. It is a
// principal of its own, "agent:<app>.<name>": it may do what its tools allow
// and, running for a person, only what that person may do too. What it causes
// that cannot be recalled waits for a person (ADR-0014 D6). Each run is a
// record of the host's agent app; each step the model chose is journaled with
// its rationale, and replay applies it without calling the model.
type Agent struct {
	Name         string // unique in the app; the agent is "<app>.<name>"
	Title        string
	Instructions string
	// Tools are the app's actions, protocol actions "<protocol id>#<action>" it
	// consumes, and its reads as "read:<name>". Every agent also has context,
	// search, ask and finish.
	Tools  []string
	Budget Budget
	// Guard may refuse an action the model chose, before it is submitted.
	Guard func(c Caller, r AgentRun, action, target string, payload json.RawMessage) *kernel.Error
	// To is who takes the goal over when a run stops, and who its questions go to
	// when it runs for no one.
	To func(c Caller, r AgentRun) []Recipient
}

// Budget bounds one run: model turns, tokens in and out, and actions taken.
type Budget struct {
	Steps, Tokens, Actions int
}

// AgentRun is a run as an agent's functions see it.
type AgentRun struct {
	ID       string
	Agent    string // "<app>.<name>"
	Goal     string
	Ref      string // the record it is about, "<type>/<id>"
	OnBehalf string // the person it runs for; empty when a flow or the app started it
}
