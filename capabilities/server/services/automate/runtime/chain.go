package automation

import (
	"encoding/json"
	"slices"
	"strings"

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

type ChainReader struct {
	Record  func(typ, id string) (any, *kernel.Error)
	Find    func(typ string, query platform.Query) []any
	Effects func() []platform.Effect
}

func (c ChainReader) Read(ref string) (Chain, *kernel.Error) {
	typ, id, _ := strings.Cut(ref, "/")
	if typ != platform.RunType && typ != platform.FlowInstanceType {
		return Chain{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if _, err := c.Record(typ, id); err != nil {
		return Chain{}, err
	}
	instance := func(id string) (platform.FlowInstance, bool) {
		v, err := c.Record(platform.FlowInstanceType, id)
		if err != nil {
			return platform.FlowInstance{}, false
		}
		x, ok := v.(platform.FlowInstance)
		return x, ok
	}
	find := func(typ, field, value string) []any {
		domain, _ := json.Marshal([]any{[]any{field, "=", value}})
		return c.Find(typ, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 50, Archived: true})
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
		v, _ := c.Record(platform.RunType, id)
		run := v.(platform.AgentRunRecord)
		if run.Flow == "" {
			c.run(run, add, link)
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
			c.run(run, add, link)
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

func (c ChainReader) run(run platform.AgentRunRecord, add func(ChainNode), link func(string, string)) {
	ref := platform.RunType + "/" + run.ID
	add(ChainNode{Ref: ref, Kind: "run", Title: run.Title, State: run.State, Detail: run.Agent})
	for _, x := range c.Effects() {
		if x.Run == run.ID {
			node := "platform.effect/" + x.ID
			add(ChainNode{Ref: node, Kind: "effect", Title: x.Event, State: x.State, Detail: x.Endpoint})
			link(ref, node)
		}
	}
}
