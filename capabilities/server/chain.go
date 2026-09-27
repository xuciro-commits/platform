package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/platform"
)

// Traces across agents (ADR-0029 D6): the chain a run or a flow instance
// belongs to — the record the flow is about, the flow and the flows it called,
// the agent runs its steps started, and the effects those runs caused (a held
// reply, a question to another agent over A2A) — from what the records
// already name: a run's flow, an instance's parent and subject, an effect's
// run. Only what the member may read is shown. OpenTelemetry stays the export.

// ChainNode is one link of the chain, by its record ("<type>/<id>").
type ChainNode struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"` // record, flow, run, effect
	Title  string `json:"title"`
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ChainEdge says one link started or caused the next.
type ChainEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Chain is the chain a record belongs to.
type Chain struct {
	Nodes []ChainNode `json:"nodes"`
	Edges []ChainEdge `json:"edges"`
}

// ChainOf is the chain of ref, an agent run or a flow instance, as m may read it.
func (t *Tenant) ChainOf(m platform.Member, ref string, now time.Time) (Chain, *kernel.Error) {
	typ, id, _ := strings.Cut(ref, "/")
	if typ != RunType && typ != flow.InstanceType {
		return Chain{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if _, err := t.RecordOf(m, typ, id, now); err != nil {
		return Chain{}, err
	}
	instance := func(id string) (flow.FlowInstance, bool) {
		v, err := t.RecordOf(m, flow.InstanceType, id, now)
		if err != nil {
			return flow.FlowInstance{}, false
		}
		x, ok := v.Record.(flow.FlowInstance)
		return x, ok
	}
	find := func(typ, field, value string) []any {
		domain, _ := json.Marshal([]any{[]any{field, "=", value}})
		page, err := t.Records(m, typ, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 50, Archived: true}, now)
		if err != nil {
			return nil
		}
		return page.Records
	}
	out := Chain{Nodes: []ChainNode{}, Edges: []ChainEdge{}}
	add := func(n ChainNode) {
		if !slices.ContainsFunc(out.Nodes, func(x ChainNode) bool { return x.Ref == n.Ref }) {
			out.Nodes = append(out.Nodes, n)
		}
	}
	link := func(from, to string) {
		if !slices.Contains(out.Edges, ChainEdge{from, to}) {
			out.Edges = append(out.Edges, ChainEdge{from, to})
		}
	}
	// Up to the first flow: a run's flow, an instance's parents.
	top := ""
	if typ == RunType {
		v, _ := t.RecordOf(m, RunType, id, now)
		run := v.Record.(AgentRunRecord)
		if run.Flow == "" {
			t.chainRun(m, run, add, link, now)
			return out, nil
		}
		top = run.Flow
	} else {
		top = id
	}
	for {
		x, ok := instance(top)
		if !ok || x.Parent == "" {
			break
		}
		top = x.Parent
	}
	// Down from it: the subject, called flows, runs, their effects.
	var walk func(id string)
	walk = func(id string) {
		x, ok := instance(id)
		if !ok {
			return
		}
		ref := flow.InstanceType + "/" + id
		add(ChainNode{Ref: ref, Kind: "flow", Title: x.Title, State: x.State, Detail: x.Flow})
		if x.Subject != "" && x.Parent == "" {
			add(ChainNode{Ref: x.Subject, Kind: "record", Title: x.Subject})
			link(x.Subject, ref)
		}
		for _, r := range find(RunType, "flow", id) {
			run := r.(AgentRunRecord)
			link(ref, RunType+"/"+run.ID)
			t.chainRun(m, run, add, link, now)
		}
		for _, c := range find(flow.InstanceType, "parent", id) {
			child := c.(flow.FlowInstance)
			link(ref, flow.InstanceType+"/"+child.ID)
			walk(child.ID)
		}
	}
	walk(top)
	return out, nil
}

// chainRun adds a run and the effects it caused.
func (t *Tenant) chainRun(m platform.Member, run AgentRunRecord, add func(ChainNode), link func(string, string), now time.Time) {
	ref := RunType + "/" + run.ID
	add(ChainNode{Ref: ref, Kind: "run", Title: run.Title, State: run.State, Detail: run.Agent})
	if m.Roles[PlatformApp] != Admin { // effects are the administrators'
		return
	}
	for _, x := range t.Effects(now) {
		if x.Run == run.ID {
			node := EffectType + "/" + x.ID
			add(ChainNode{Ref: node, Kind: "effect", Title: x.Event, State: x.State, Detail: x.Endpoint})
			link(ref, node)
		}
	}
}
