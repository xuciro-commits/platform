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
	Name  string // unique in the app; the agent is "<app>.<name>"
	Title string
	// Description says what it does for others: its A2A card's (ADR-0022),
	// where its instructions stay private.
	Description  string
	Instructions string
	// Tools are the app's actions, protocol actions "<protocol id>#<action>" it
	// consumes, its reads as "read:<name>", and its effect kinds as
	// "emit:<kind>": the agent sends one and waits for the receiver's answer
	// (an external agent over A2A, ADR-0022). Every agent also has context,
	// search, knowledge, remember, ask and finish.
	Tools  []string
	Budget Budget
	// Guard may refuse an action the model chose, before it is submitted.
	Guard func(c Caller, r AgentRun, action, target string, payload json.RawMessage) *kernel.Error
	// To is who takes the goal over when a run stops, and who its questions go to
	// when it runs for no one.
	To func(c Caller, r AgentRun) []Recipient
	// Cases are its evaluation suite (ADR-0029 D6): run dry against a model,
	// each three times, before it meets people and when its model changes.
	Cases []Case
}

// Case is one test of an agent: a goal about a record, and what a run of it
// should come to. Check says why the run fails the case, or "" when it passes.
type Case struct {
	Name, Goal, Ref string
	Check           func(CaseRun) string
}

// CaseRun is what a dry run of a case came to: the actions it would take, as
// "<action> <target> <payload JSON>", its result, and whether it asked a person.
type CaseRun struct {
	Actions []string
	Result  string
	Asked   bool
}

// Budget bounds one run: model turns, tokens in and out, and actions taken.
type Budget struct {
	Steps, Tokens, Actions int
	// Cost caps what a run may spend on models, in USD, across its calls; 0:
	// no cap. It is measured from what the providers report (ADR-0050 D6).
	Cost float64
}

// AgentRun is a run as an agent's functions see it.
type AgentRun struct {
	ID       string
	Agent    string // "<app>.<name>"
	Goal     string
	Ref      string // the record it is about, "<type>/<id>"
	OnBehalf string // the person it runs for; empty when a flow or the app started it
}
