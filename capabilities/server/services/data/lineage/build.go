package lineage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func Build(in Input, basis Basis) Graph {
	g := Graph{
		Nodes:      []Node{},
		Edges:      []Edge{},
		Unreadable: slices.Clone(in.Unreadable),
		Coverage:   Coverage{MarkingsComplete: true, StatusComplete: true, FieldsComplete: false},
	}
	meta := func(node string) Meta {
		if value, ok := in.Meta[node]; ok {
			return value
		}
		return Meta{Visible: true, CanOpen: true, CanRead: true}
	}
	add := func(node Node) {
		if _, ok := g.node(node.ID); ok {
			return
		}
		value := meta(node.ID)
		node.RID, node.Project, node.CanOpen, node.CanReadData = value.RID, value.Project, value.CanOpen, value.CanRead
		if node.MarkingStatus == "" {
			node.MarkingStatus = "consistent"
		}
		g.Nodes = append(g.Nodes, node)
	}
	include := func(archived bool) bool {
		return basis == Guard || !archived
	}
	when := func(value time.Time) *time.Time {
		if value.IsZero() {
			return nil
		}
		return &value
	}
	for _, item := range in.Connections {
		if !include(item.Archived) {
			continue
		}
		node := Node{ID: "connection:" + item.ID, Kind: "connection", Record: item.ID, Name: item.Name, Title: item.Title, State: item.State, Declared: item.Marking, DeclaredCompartments: slices.Clone(item.Compartments), DefinitionVersion: item.DefinitionVersion}
		if item.Last != nil {
			node.At = when(item.Last.At)
			if !item.Last.OK || item.Last.Error != "" {
				node.Status = "failed"
				node.ErrorCode = "connection-check-failed"
			}
		}
		add(node)
	}
	versions := map[string]int{}
	for _, item := range in.Datasets {
		if !include(item.Archived) {
			continue
		}
		versions[item.ID] = item.Version
		node := Node{ID: "dataset:" + item.ID, Kind: "dataset", Record: item.ID, Name: item.Name, Title: item.Title, Declared: item.Marking, DeclaredCompartments: slices.Clone(item.Compartments), DefinitionVersion: item.DefinitionVersion, DatasetVersion: item.Version}
		if item.Last != nil {
			node.Rows = item.Last.Rows
			node.At = when(item.Last.At)
		}
		add(node)
	}
	for _, item := range in.Sources {
		if !include(item.Archived) {
			continue
		}
		node := Node{ID: "source:" + item.ID, Kind: "source", Record: item.ID, Name: item.Name, Title: item.Title, State: item.State, DefinitionVersion: item.DefinitionVersion}
		if item.Last != nil {
			node.Rows = item.Last.Rows
			node.At = when(item.Last.At)
			node.ExecutionVersion = executionVersion(item.Last.DefinitionVersion)
			if item.Last.Error != "" || item.Last.Failed > 0 {
				node.Status = "failed"
				node.ErrorCode = "pull-failed"
			}
		}
		add(node)
	}
	for _, item := range in.Pipelines {
		if !include(item.Archived) {
			continue
		}
		node := Node{ID: "pipeline:" + item.ID, Kind: "pipeline", Record: item.ID, Name: item.Name, Title: item.Title, State: item.State, Declared: item.Marking, DeclaredCompartments: slices.Clone(item.Compartments), DefinitionVersion: item.DefinitionVersion}
		if item.Last != nil {
			node.Rows = item.Last.Written
			node.At = when(item.Last.At)
			node.ExecutionVersion = executionVersion(item.Last.DefinitionVersion)
			if item.Last.Error != "" {
				node.Status = "failed"
				node.ErrorCode = "run-failed"
			}
		}
		add(node)
	}
	for _, item := range in.Writebacks {
		if !include(item.Archived) {
			continue
		}
		node := Node{ID: "writeback:" + item.ID, Kind: "writeback", Record: item.ID, Name: item.Name, Title: item.Title, State: item.State, Rows: item.Sent, DefinitionVersion: item.DefinitionVersion}
		if item.Failed > 0 || item.Rejected > 0 {
			node.Status = "failed"
			node.ErrorCode = "delivery-failed"
		}
		if len(item.Answers) > 0 {
			node.At = when(item.Answers[len(item.Answers)-1].At)
		}
		add(node)
	}
	for _, item := range in.Objects {
		if !include(item.Archived) {
			continue
		}
		name := objectType(item.Name)
		add(Node{ID: "object:" + name, Kind: "object", Record: item.ID, Name: name, Title: item.Title, State: item.State})
	}
	for _, item := range g.Nodes {
		if item.Status != "" {
			continue
		}
		switch item.Kind {
		case "connection", "source", "pipeline":
			g.Nodes[index(g.Nodes, item.ID)].Status = "current"
		case "dataset":
			if item.DatasetVersion == 0 {
				g.Nodes[index(g.Nodes, item.ID)].Status = "never-run"
			} else {
				g.Nodes[index(g.Nodes, item.ID)].Status = "current"
			}
		case "writeback":
			if item.Rows == 0 {
				g.Nodes[index(g.Nodes, item.ID)].Status = "never-run"
			} else {
				g.Nodes[index(g.Nodes, item.ID)].Status = "current"
			}
		case "object":
			g.Nodes[index(g.Nodes, item.ID)].Status = "current"
		}
	}
	for _, item := range in.Connections {
		if item.Last == nil && include(item.Archived) {
			setStatus(&g, "connection:"+item.ID, "never-run", "")
		}
	}
	for _, item := range in.Sources {
		if item.Last == nil && include(item.Archived) {
			setStatus(&g, "source:"+item.ID, "never-run", "")
		}
	}
	for _, item := range in.Pipelines {
		if item.Last == nil && include(item.Archived) {
			setStatus(&g, "pipeline:"+item.ID, "never-run", "")
		}
	}

	edges := map[string]Edge{}
	link := func(edge Edge) {
		if edge.From == "" || edge.To == "" || edge.From == edge.To {
			return
		}
		if edge.ID == "" {
			edge.ID = edge.From + ">" + edge.To + "#" + edge.Role
			if edge.Occurrence > 0 {
				edge.ID += ":" + fmt.Sprint(edge.Occurrence)
			}
		}
		if old, ok := edges[edge.ID]; ok {
			old.DeclaredBy = append(old.DeclaredBy, edge.DeclaredBy...)
			old.Unresolved = old.Unresolved || edge.Unresolved
			edges[edge.ID] = old
			return
		}
		edges[edge.ID] = edge
	}
	exists := func(id string) bool {
		_, ok := g.node(id)
		return ok
	}
	for _, item := range in.Sources {
		if !include(item.Archived) {
			continue
		}
		from := "connection:" + item.Connection
		to := "source:" + item.ID
		link(Edge{From: from, To: to, Role: "read", DeclaredBy: []string{"source:" + item.ID}, Unresolved: !exists(from)})
		if item.Dataset != "" {
			target := "dataset:" + item.Dataset
			link(Edge{From: to, To: target, Role: "write", DeclaredBy: []string{"source:" + item.ID}, Unresolved: !exists(target)})
		}
		if item.Object != "" {
			target := "object:" + objectType(item.Object)
			link(Edge{From: to, To: target, Role: "write", DeclaredBy: []string{"source:" + item.ID}, Unresolved: !exists(target)})
		}
	}
	for _, item := range in.Pipelines {
		if !include(item.Archived) {
			continue
		}
		id := "pipeline:" + item.ID
		inputs := append([]string{item.Input}, item.StepDatasets()...)
		for occurrence, input := range inputs {
			if input == "" {
				continue
			}
			role := "input"
			if occurrence > 0 {
				role = "join"
			}
			from := "dataset:" + input
			edge := Edge{From: from, To: id, Role: role, Occurrence: occurrence, DeclaredBy: []string{"pipeline:" + item.ID}, CurrentVersion: versions[input], Unresolved: !exists(from)}
			if item.Last != nil && item.Last.Execution != nil {
				if consumed, ok := item.Last.Execution.Inputs[input]; ok {
					edge.ConsumedVersion = consumed
					edge.Stale = consumed != versions[input]
				}
			}
			link(edge)
		}
		if item.OutputDataset != "" {
			target := "dataset:" + item.OutputDataset
			link(Edge{From: id, To: target, Role: "output", DeclaredBy: []string{"pipeline:" + item.ID}, CurrentVersion: versions[item.OutputDataset], Unresolved: !exists(target)})
		}
		if item.OutputObject != "" {
			target := "object:" + objectType(item.OutputObject)
			link(Edge{From: id, To: target, Role: "output", DeclaredBy: []string{"pipeline:" + item.ID}, Unresolved: !exists(target)})
		}
	}
	for _, item := range in.Objects {
		if !include(item.Archived) {
			continue
		}
		object := "object:" + objectType(item.Name)
		for occurrence, binding := range item.DataSources {
			source := binding.Kind + ":" + binding.Source
			link(Edge{From: source, To: object, Role: "binding", Occurrence: occurrence, DeclaredBy: []string{object}, Unresolved: !exists(source)})
		}
	}
	for _, item := range in.Writebacks {
		if !include(item.Archived) {
			continue
		}
		id := "writeback:" + item.ID
		object := "object:" + objectType(item.Object)
		link(Edge{From: object, To: id, Role: "request", DeclaredBy: []string{"writeback:" + item.ID}, Unresolved: !exists(object)})
		connection := "connection:" + item.Connection
		link(Edge{From: id, To: connection, Role: "send", DeclaredBy: []string{"writeback:" + item.ID}, Unresolved: !exists(connection)})
		if len(item.Result) > 0 {
			link(Edge{From: id, To: object, Role: "result", DeclaredBy: []string{"writeback:" + item.ID}, Unresolved: !exists(object)})
		}
	}
	for _, edge := range edges {
		if edge.Unresolved {
			g.Unreadable = append(g.Unreadable, Unreadable{Reason: "missing-reference", Count: 1})
		}
		g.Edges = append(g.Edges, edge)
	}
	projects := map[string]string{}
	for _, node := range g.Nodes {
		projects[node.ID] = node.Project
	}
	for i := range g.Edges {
		from, to := projects[g.Edges[i].From], projects[g.Edges[i].To]
		g.Edges[i].CrossesProject = from != "" && to != "" && from != to
	}
	for i := range g.Nodes {
		g.Nodes[i].Connected = connected(g.Edges, g.Nodes[i].ID)
	}
	propagate(&g, basis, in)
	statuses(&g, in)
	g.Groups = g.group()
	g.Total = Total{Nodes: len(g.Nodes), Edges: len(g.Edges)}
	g.Returned = g.Total
	return g.normalized()
}

func objectType(name string) string {
	if name == "" || strings.Contains(name, ".") {
		return name
	}
	return build.ID + "." + name
}

func executionVersion(value string) int {
	version, _ := strconv.Atoi(value)
	return version
}

func index(nodes []Node, id string) int {
	for i := range nodes {
		if nodes[i].ID == id {
			return i
		}
	}
	return -1
}

func setStatus(g *Graph, id, status, code string) {
	i := index(g.Nodes, id)
	if i < 0 {
		return
	}
	g.Nodes[i].Status, g.Nodes[i].ErrorCode = status, code
}

func connected(edges []Edge, id string) bool {
	for _, edge := range edges {
		if edge.From == id || edge.To == id {
			return true
		}
	}
	return false
}

func propagate(g *Graph, basis Basis, in Input) {
	for i := range g.Nodes {
		g.Nodes[i].Effective = g.Nodes[i].Declared
		g.Nodes[i].EffectiveCompartments = slices.Clone(g.Nodes[i].DeclaredCompartments)
		g.Nodes[i].Guard = g.Nodes[i].Effective
		g.Nodes[i].GuardCompartments = slices.Clone(g.Nodes[i].EffectiveCompartments)
	}
	if len(in.Opaque) > 0 {
		g.Coverage.MarkingsComplete = false
		for _, id := range in.Opaque {
			i := index(g.Nodes, id)
			if i < 0 {
				continue
			}
			g.Nodes[i].Effective = "restricted"
			g.Nodes[i].Guard = "restricted"
		}
	}
	declassified := map[string][]string{}
	for _, item := range in.Pipelines {
		declassified["pipeline:"+item.ID] = item.Declassifications()
	}
	at := g.byID()
	for changed := true; changed; {
		changed = false
		for _, edge := range g.Edges {
			if edge.Unresolved || edge.Role == "result" {
				continue
			}
			from, ok := at[edge.From]
			if !ok {
				continue
			}
			to, ok := at[edge.To]
			if !ok || g.Nodes[to].Kind == "connection" {
				continue
			}
			incoming := platform.MarkingSet{Level: g.Nodes[from].Effective, Compartments: g.Nodes[from].EffectiveCompartments}
			if g.Nodes[to].Kind == "pipeline" {
				incoming = incoming.Without(declassified[g.Nodes[to].ID])
			}
			level := platform.HigherMarking(g.Nodes[to].Effective, incoming.Level)
			compartments := platform.JoinCompartments(g.Nodes[to].EffectiveCompartments, incoming.Compartments)
			if level != g.Nodes[to].Effective || !slices.Equal(compartments, g.Nodes[to].EffectiveCompartments) {
				g.Nodes[to].Effective, g.Nodes[to].EffectiveCompartments, changed = level, compartments, true
			}
		}
	}
	for i := range g.Nodes {
		g.Nodes[i].Guard = platform.HigherMarking(g.Nodes[i].Guard, g.Nodes[i].Effective)
		g.Nodes[i].GuardCompartments = platform.JoinCompartments(g.Nodes[i].GuardCompartments, g.Nodes[i].EffectiveCompartments)
	}
	for _, evidence := range in.Evidence {
		i, ok := at[evidence.Target]
		if !ok {
			continue
		}
		g.Nodes[i].Guard = platform.HigherMarking(g.Nodes[i].Guard, evidence.Marking)
		g.Nodes[i].GuardCompartments = platform.JoinCompartments(g.Nodes[i].GuardCompartments, evidence.Compartments)
	}
	for i := range g.Nodes {
		g.Nodes[i].MarkingStatus = "consistent"
		if g.Nodes[i].Guard != g.Nodes[i].Effective || !slices.Equal(g.Nodes[i].GuardCompartments, g.Nodes[i].EffectiveCompartments) {
			g.Nodes[i].MarkingStatus = "preview-only"
		}
		if !g.Coverage.MarkingsComplete {
			g.Nodes[i].MarkingStatus = "unknown"
		}
	}
}

func statuses(g *Graph, in Input) {
	at := g.byID()
	for _, edge := range g.Edges {
		if edge.Unresolved || !edge.Stale {
			continue
		}
		i, ok := at[edge.To]
		if !ok {
			continue
		}
		g.Nodes[i].StaleReasons = append(g.Nodes[i].StaleReasons, "input-newer")
	}
	for _, item := range in.Pipelines {
		i, ok := at["pipeline:"+item.ID]
		if !ok {
			continue
		}
		if item.Last != nil && item.Last.DefinitionVersion != "" && fmt.Sprint(item.DefinitionVersion) != item.Last.DefinitionVersion {
			g.Nodes[i].StaleReasons = append(g.Nodes[i].StaleReasons, "definition-changed")
		}
	}
	for changed := true; changed; {
		changed = false
		for _, edge := range g.Edges {
			if edge.Unresolved || edge.Role == "result" {
				continue
			}
			from, ok := at[edge.From]
			if !ok {
				continue
			}
			to, ok := at[edge.To]
			if !ok {
				continue
			}
			if g.Nodes[from].Status == "failed" || g.Nodes[from].Status == "stale" {
				if !slices.Contains(g.Nodes[to].StaleReasons, "upstream-stale") {
					g.Nodes[to].StaleReasons = append(g.Nodes[to].StaleReasons, "upstream-stale")
					changed = true
				}
			}
		}
	}
	for i := range g.Nodes {
		node := &g.Nodes[i]
		if len(node.StaleReasons) > 0 && node.Status != "failed" && node.Status != "never-run" {
			node.Status = "stale"
		}
	}
	producers := map[string][]string{}
	for _, edge := range g.Edges {
		if edge.Unresolved || !strings.HasPrefix(edge.To, "object:") {
			continue
		}
		producers[edge.To] = append(producers[edge.To], edge.From)
	}
	for i := range g.Nodes {
		node := &g.Nodes[i]
		if node.Kind != "object" {
			continue
		}
		status := "current"
		for _, producer := range producers[node.ID] {
			j, ok := at[producer]
			if !ok {
				continue
			}
			switch g.Nodes[j].Status {
			case "failed":
				status = "failed"
			case "stale", "never-run":
				if status == "current" {
					status = g.Nodes[j].Status
				}
			}
		}
		node.Status = status
	}
}
