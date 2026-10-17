package platform

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	FlowApp          = "flow"
	FlowInstanceType = "flow.instance"
)

type FlowInstance struct {
	Record
	Applications []AssetBinding             `json:"applications,omitempty" field:"readonly" type:"json"`
	Flow         string                     `json:"flow" field:"readonly,search"` // "<app>.<name>"
	Title        string                     `json:"title" field:"readonly,search"`
	Version      int                        `json:"version" field:"readonly"`
	Dependencies string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release      string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key          string                     `json:"key" field:"readonly,search"`
	Subject      string                     `json:"subject,omitempty" field:"readonly"` // "<type>/<key>" when the flow declares its subject
	State        string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf     string                     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data         string                     `json:"data,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Outputs      map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources      []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld     bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer       string                     `json:"answer,omitempty" field:"readonly"`
	Parent       string                     `json:"parent,omitempty" field:"readonly"` // the instance that called it
	Tokens       []FlowToken                `json:"tokens" field:"readonly" title:"Where it stands"`
	Undo         []FlowUndoEntry            `json:"undo" field:"readonly" title:"To undo"`
	Trace        []FlowTraceLine            `json:"trace" field:"readonly"`
	Seq          int                        `json:"seq" field:"readonly"` // tokens and tasks made, for their IDs
}

type FlowToken struct {
	ID        int                        `json:"id"`
	Step      string                     `json:"step"`
	Branch    string                     `json:"branch,omitempty"` // the All or Any step it runs in
	Waits     string                     `json:"waits,omitempty"`  // ready, retry, wait, ask, call, join, undo, stuck
	Attempts  int                        `json:"attempts,omitempty"`
	Due       time.Time                  `json:"due,omitzero"` // a retry, a timeout, or a wait's time
	Task      string                     `json:"task,omitempty"`
	Child     string                     `json:"child,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Parent    int                        `json:"parent,omitempty"`
	Frames    []Frame                    `json:"frames,omitempty"`
	Input     json.RawMessage            `json:"input,omitempty"`
	Outputs   map[string]json.RawMessage `json:"outputs,omitempty"`
	Operation string                     `json:"operation,omitempty"`
	Loop      *FlowLoopFrame             `json:"loop,omitempty"`
	Results   map[string]json.RawMessage `json:"results,omitempty"`
}

type FlowLoopFrame struct {
	Items   []json.RawMessage       `json:"items,omitempty"`
	Outer   []string                `json:"outer,omitempty"`
	Next    int                     `json:"next"`
	Results map[int]json.RawMessage `json:"results,omitempty"`
	State   json.RawMessage         `json:"state,omitempty"`
	While   bool                    `json:"while,omitempty"`
}

type FlowUndoEntry struct {
	Step     string          `json:"step"`
	App      string          `json:"app"`
	Protocol string          `json:"protocol,omitempty"`
	Action   string          `json:"action"`
	Target   string          `json:"target"`
	Payload  json.RawMessage `json:"payload"`
}

type FlowTraceLine struct {
	At     time.Time `json:"at"`
	Step   string    `json:"step,omitempty"`
	What   string    `json:"what"`
	Detail string    `json:"detail,omitempty"`
	By     string    `json:"by,omitempty"`
}

type FlowDefinition struct {
	ID      string     `json:"id"`
	App     string     `json:"app"`
	Title   string     `json:"title"`
	Version int        `json:"version"`
	Start   []string   `json:"start"`
	Steps   []FlowStep `json:"steps"`
}

type FlowStep struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Kind  string   `json:"kind" enum:"action,wait,ask,subflow,fork,agent,transform,branch,foreach,compute,ai,end,break,continue"`
	Next  []string `json:"next"`
	// Chooses: the step's code picks the next one as the instance runs.
	Chooses bool `json:"chooses,omitempty"`
}

// UnmarshalJSON maps old durable token positions into the same current
// native semantics. It only decodes saved facts, never repeats an action.
func (x *FlowInstance) UnmarshalJSON(raw []byte) error {
	type image FlowInstance
	var saved image
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	for index := range saved.Tokens {
		token := &saved.Tokens[index]
		if token.Parent == 0 && token.Branch != "" {
			for _, candidate := range saved.Tokens {
				if candidate.Waits == "join" && candidate.Step == token.Branch {
					token.Parent = candidate.ID
					break
				}
			}
		}
		if node, ok := strings.CutPrefix(token.Step, "_function_"); ok && token.Waits == "wait" {
			var calls map[string]string
			if json.Unmarshal([]byte(saved.Data), &calls) == nil && calls[node] != "" {
				token.Step = node
				token.Waits = "invocation"
				token.Child = calls[node]
			}
		}
	}
	*x = FlowInstance(saved)
	return nil
}
