package platformserver

import (
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

type LineageNode struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Record    string     `json:"record,omitempty"`
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	State     string     `json:"state,omitempty"`
	Marking   string     `json:"marking,omitempty"`
	Effective string     `json:"effective,omitempty"`
	Version   int        `json:"version,omitempty"`
	Rows      int        `json:"rows,omitempty"`
	At        *time.Time `json:"at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Stale     bool       `json:"stale,omitempty"`
}

type LineageEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Version int    `json:"version,omitempty"`
	Stale   bool   `json:"stale,omitempty"`
}

type Lineage struct {
	Nodes []LineageNode `json:"nodes"`
	Edges []LineageEdge `json:"edges"`
}

var markingRank = map[string]int{"internal": 1, "confidential": 2, "restricted": 3}

func lineageRecords[T any](c platform.Caller) []T {
	rows, _, refusal := platform.Find[T](c, platform.Query{Sort: []string{"id"}, Limit: 1000})
	if refusal != nil {
		return nil
	}
	return rows
}

func lineageObjectType(name string) string {
	if name == "" || strings.Contains(name, ".") {
		return name
	}
	return build.ID + "." + name
}

func (t *Tenant) lineage(m platform.Member) (Lineage, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return Lineage{}, err
	}
	if !m.Holds(build.ID, build.Builder) && !m.Holds(build.ID, build.Integrator) {
		return Lineage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	c := t.caller(m, t.app(build.ID), false)
	out := Lineage{Nodes: []LineageNode{}, Edges: []LineageEdge{}}
	at := map[string]int{}
	add := func(node LineageNode) {
		if _, seen := at[node.ID]; seen {
			return
		}
		at[node.ID] = len(out.Nodes)
		out.Nodes = append(out.Nodes, node)
	}
	edges := map[string]bool{}
	link := func(edge LineageEdge) {
		if edge.From == "" || edge.To == "" || edge.From == edge.To {
			return
		}
		if _, ok := at[edge.From]; !ok {
			return
		}
		if _, ok := at[edge.To]; !ok {
			return
		}
		key := edge.From + ">" + edge.To
		if edges[key] {
			return
		}
		edges[key] = true
		out.Edges = append(out.Edges, edge)
	}
	when := func(v time.Time) *time.Time {
		if v.IsZero() {
			return nil
		}
		return &v
	}

	connections := lineageRecords[build.Connection](c)
	for _, conn := range connections {
		node := LineageNode{ID: "connection:" + conn.ID, Kind: "connection", Record: conn.ID, Name: conn.Name, Title: conn.Title, State: conn.State, Marking: conn.Marking}
		if conn.Last != nil {
			node.At = when(conn.Last.At)
			node.Error = conn.Last.Error
		}
		add(node)
	}
	datasets := lineageRecords[build.Dataset](c)
	versions := map[string]int{}
	for _, ds := range datasets {
		versions[ds.ID] = ds.Version
		node := LineageNode{ID: "dataset:" + ds.ID, Kind: "dataset", Record: ds.ID, Name: ds.Name, Title: ds.Title, Marking: ds.Marking, Version: ds.Version}
		if ds.Last != nil {
			node.Rows = ds.Last.Rows
			node.At = when(ds.Last.At)
		}
		add(node)
	}
	objects := lineageRecords[build.Object](c)
	for _, o := range objects {
		add(LineageNode{ID: "object:" + lineageObjectType(o.Name), Kind: "object", Record: o.ID, Name: lineageObjectType(o.Name), Title: o.Title, State: o.State})
	}
	sources := lineageRecords[build.Source](c)
	sourceByName := map[string]string{}
	for _, s := range sources {
		sourceByName[s.Name] = "source:" + s.ID
		node := LineageNode{ID: "source:" + s.ID, Kind: "source", Record: s.ID, Name: s.Name, Title: s.Title, State: s.State}
		if s.Last != nil {
			node.Rows = s.Last.Rows
			node.At = when(s.Last.At)
			node.Error = s.Last.Error
		}
		add(node)
	}
	pipelines := lineageRecords[build.Pipeline](c)
	pipelineByName := map[string]string{}
	for _, p := range pipelines {
		pipelineByName[p.Name] = "pipeline:" + p.ID
		node := LineageNode{ID: "pipeline:" + p.ID, Kind: "pipeline", Record: p.ID, Name: p.Name, Title: p.Title, State: p.State, Marking: p.Marking}
		if p.Last != nil {
			node.Rows = p.Last.Written
			node.At = when(p.Last.At)
			node.Error = p.Last.Error
		}
		add(node)
	}
	writebacks := lineageRecords[build.Writeback](c)
	for _, w := range writebacks {
		add(LineageNode{ID: "writeback:" + w.ID, Kind: "writeback", Record: w.ID, Name: w.Name, Title: w.Title, State: w.State, Rows: w.Sent})
	}

	for _, s := range sources {
		id := "source:" + s.ID
		link(LineageEdge{From: "connection:" + s.Connection, To: id})
		link(LineageEdge{From: id, To: "dataset:" + s.Dataset})
		link(LineageEdge{From: id, To: "object:" + lineageObjectType(s.Object)})
	}
	for _, p := range pipelines {
		id := "pipeline:" + p.ID
		stale := false
		for _, input := range append([]string{p.Input}, p.StepDatasets()...) {
			edge := LineageEdge{From: "dataset:" + input, To: id}
			if p.Last != nil && p.Last.Execution != nil {
				if consumed, ok := p.Last.Execution.Inputs[input]; ok {
					edge.Version = consumed
					edge.Stale = consumed != versions[input]
				}
			}
			stale = stale || edge.Stale
			link(edge)
		}
		out.Nodes[at[id]].Stale = stale
		link(LineageEdge{From: id, To: "dataset:" + p.OutputDataset})
		link(LineageEdge{From: id, To: "object:" + lineageObjectType(p.OutputObject)})
	}
	for _, o := range objects {
		id := "object:" + lineageObjectType(o.Name)
		for _, binding := range o.DataSources {
			switch binding.Kind {
			case "source":
				link(LineageEdge{From: sourceByName[binding.Source], To: id})
			case "pipeline":
				link(LineageEdge{From: pipelineByName[binding.Source], To: id})
			}
		}
	}
	for _, w := range writebacks {
		id := "writeback:" + w.ID
		link(LineageEdge{From: "object:" + lineageObjectType(w.Object), To: id})
		link(LineageEdge{From: id, To: "connection:" + w.Connection})
	}

	linked := map[string]bool{}
	for _, edge := range out.Edges {
		linked[edge.From], linked[edge.To] = true, true
	}
	kept := out.Nodes[:0]
	for _, node := range out.Nodes {
		if node.Kind != "object" || linked[node.ID] {
			kept = append(kept, node)
		}
	}
	out.Nodes = kept
	clear(at)
	for i := range out.Nodes {
		at[out.Nodes[i].ID] = i
		out.Nodes[i].Effective = out.Nodes[i].Marking
	}
	for changed := true; changed; {
		changed = false
		for _, edge := range out.Edges {
			from, to := &out.Nodes[at[edge.From]], &out.Nodes[at[edge.To]]
			if to.Kind == "connection" || markingRank[from.Effective] <= markingRank[to.Effective] {
				continue
			}
			to.Effective = from.Effective
			changed = true
		}
	}
	slices.SortStableFunc(out.Edges, func(a, b LineageEdge) int { return strings.Compare(a.From+">"+a.To, b.From+">"+b.To) })
	return out, nil
}
