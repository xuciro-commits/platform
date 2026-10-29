package platformserver

import (
	"encoding/json"
	controlowner "platformserver/kernel/control"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ChainNode struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type ChainEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Chain struct {
	Nodes []ChainNode `json:"nodes"`
	Edges []ChainEdge `json:"edges"`
}

func (t *Tenant) chainRead(m platform.Member, ref string, now time.Time) (Chain, *kernel.Error) {
	typ, id, _ := strings.Cut(ref, "/")
	if typ != platform.RunType && typ != platform.FlowInstanceType {
		return Chain{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if _, err := t.recordOf(m, typ, id, now); err != nil {
		return Chain{}, err
	}
	instance := func(id string) (platform.FlowInstance, bool) {
		v, err := t.recordOf(m, platform.FlowInstanceType, id, now)
		if err != nil {
			return platform.FlowInstance{}, false
		}
		x, ok := v.Record.(platform.FlowInstance)
		return x, ok
	}
	find := func(typ, field, value string) []any {
		domain, _ := json.Marshal([]any{[]any{field, "=", value}})
		page, err := t.recordsRead(m, typ, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 50, Archived: true}, now)
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
	top := ""
	if typ == platform.RunType {
		v, _ := t.recordOf(m, platform.RunType, id, now)
		run := v.Record.(platform.AgentRunRecord)
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
	var walk func(id string)
	walk = func(id string) {
		x, ok := instance(id)
		if !ok {
			return
		}
		ref := platform.FlowInstanceType + "/" + id
		add(ChainNode{Ref: ref, Kind: "flow", Title: x.Title, State: x.State, Detail: x.Flow})
		if x.Subject != "" && x.Parent == "" {
			add(ChainNode{Ref: x.Subject, Kind: "record", Title: x.Subject})
			link(x.Subject, ref)
		}
		for _, r := range find(platform.RunType, "flow", id) {
			run := r.(platform.AgentRunRecord)
			link(ref, platform.RunType+"/"+run.ID)
			t.chainRun(m, run, add, link, now)
		}
		for _, c := range find(platform.FlowInstanceType, "parent", id) {
			child := c.(platform.FlowInstance)
			link(ref, platform.FlowInstanceType+"/"+child.ID)
			walk(child.ID)
		}
	}
	walk(top)
	return out, nil
}

func (t *Tenant) chainRun(m platform.Member, run platform.AgentRunRecord, add func(ChainNode), link func(string, string), now time.Time) {
	ref := platform.RunType + "/" + run.ID
	add(ChainNode{Ref: ref, Kind: "run", Title: run.Title, State: run.State, Detail: run.Agent})
	if m.Roles[controlowner.PlatformApp] != controlowner.Admin {
		return
	}
	for _, x := range t.Effects(now) {
		if x.Run == run.ID {
			node := controlowner.EffectType + "/" + x.ID
			add(ChainNode{Ref: node, Kind: "effect", Title: x.Event, State: x.State, Detail: x.Endpoint})
			link(ref, node)
		}
	}
}
