package platform

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Flow is a long-running process an app declares (ADR-0020): a trigger, then
// named steps joined by transitions, with Go functions for the logic. The host
// runs each instance as a record of its flow app, acting as the declaring
// app's automation principal on behalf of whoever started it; every step's
// outcome and reason is kept on the instance as its trace. Several versions of
// a flow may be declared: new instances take the highest, running instances
// keep theirs until they end or are moved to the next.
type Flow struct {
	Name    string // unique in the app; the flow is "<app>.<name>"
	Title   string
	Version int // from 1
	Start   Start
	Steps   []Step // an instance starts at the first
	// Owners are roles of the app asked when an instance is stuck: a failed
	// compensation, or a version the code no longer runs.
	Owners []string
	// From moves running instances of the previous version to this one: their
	// step there → the step here (a decision, taken by an administrator).
	From map[string]string
}

// Start is what starts an instance: an event (an action of the app, or a
// protocol event "<protocol id>#<event>" the app consumes) and the decision
// whether it starts one, with the instance's key (one running instance per
// key) and its first data.
type Start struct {
	On    string
	Begin func(c Caller, e Event) (key string, data any, ok bool)
}

// Step is one step: exactly one of Act, Wait, Ask, Call, All, Any or Agent.
// Next is the step after it; Choose, when set, picks it instead and says why.
// An empty next step ends the flow (or the branch).
type Step struct {
	Name  string
	Title string
	Act   *Act
	Wait  *Wait
	Ask   *Ask
	Call  *Call
	All   []string // branches, by their first step: the flow goes on when all have ended
	Any   []string // branches: the flow goes on when the first has ended; the others are canceled
	Agent *AgentStep
	Next  string
	// Choose picks the next step and gives the reason kept in the trace.
	Choose func(c Caller, r *Run) (next, reason string)
	// Timeout ends a Wait or an Ask after this long, at OnTimeout.
	Timeout   time.Duration
	OnTimeout string
	// Fault is where an Act goes once its retries are spent; without it the
	// flow compensates: each completed Act's Undo runs, newest first.
	Fault string
	Undo  *Act
}

// Compensate, as a next step, undoes what the flow did: each completed Act's
// Undo, newest first; the instance ends compensated.
const Compensate = "@compensate"

// Act submits an action as the app, its own or a protocol's.
type Act struct {
	Protocol string // empty: the app's own action
	Action   string
	// Target and Payload build the submission from the instance.
	Target  func(c Caller, r *Run) string
	Payload func(c Caller, r *Run) any
	// Done keeps what the action answered on the instance (a booking's ID).
	Done func(c Caller, r *Run, target *pb.EntityRef)
}

// Wait ends when an event arrives that Match accepts (On: an action or
// "<protocol id>#<event>"), when Until holds (checked every second), or at the
// time At gives.
type Wait struct {
	On    string
	Match func(c Caller, r *Run, e Event) bool
	Until func(c Caller, r *Run) bool
	At    func(c Caller, r *Run) time.Time
}

// Ask gives people a task (ADR-0017). Its answer, one of Answers ("done"
// without any), is the instance's Answer for the next step to read. When an
// event Match accepts arrives first (On), the task closes and the answer is "event".
type Ask struct {
	Title   func(c Caller, r *Run) string
	Body    func(c Caller, r *Run) string
	Ref     func(c Caller, r *Run) string
	To      func(c Caller, r *Run) []Recipient
	Answers []string
	On      string
	Match   func(c Caller, r *Run, e Event) bool
}

// Call runs another of the app's flows and waits for it to end; its end state
// ("done", "compensated", "canceled") is the Answer.
type Call struct {
	Flow string
	Data func(c Caller, r *Run) any
}

// AgentStep is a goal for an agent (stage 5): the actions it may take, a
// budget, and until agents run, the person it falls back to (a task).
type AgentStep struct {
	Goal    string
	Actions []string
	Budget  int
	To      func(c Caller, r *Run) []Recipient
}

// Run is an instance as a step's functions see it.
type Run struct {
	ID       string
	Flow     string
	Version  int
	Key      string
	OnBehalf string // who started it
	Data     json.RawMessage
	Answer   string // the last Ask's or Call's
	Event    *Event // the event that started it or ended the last wait
}

// Set replaces the instance's data.
func (r *Run) Set(v any) {
	r.Data, _ = json.Marshal(v)
}

// DataOf reads an instance's data as T.
func DataOf[T any](r *Run) T {
	var v T
	json.Unmarshal(r.Data, &v)
	return v
}
