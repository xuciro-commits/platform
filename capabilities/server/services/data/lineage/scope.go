package lineage

import (
	"slices"
)

type Params struct {
	Mode      string
	Project   string
	Focus     string
	Direction string
	Depth     int
	NodeLimit int
	EdgeLimit int
}

const (
	defaultNodeLimit = 200
	defaultEdgeLimit = 400
	maxNodeLimit     = 2000
	maxEdgeLimit     = 5000
)

func Project(g Graph, visible func(Node) bool, reason string) Graph {
	if visible == nil {
		return g
	}
	shown := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		shown[node.ID] = visible(node)
	}
	index := g.byID()
	hidden := 0
	for _, edge := range g.Edges {
		from, fromOK := index[edge.From]
		to, toOK := index[edge.To]
		if !fromOK || !toOK {
			continue
		}
		if shown[edge.From] && !shown[edge.To] {
			if g.Nodes[from].Boundary == nil {
				g.Nodes[from].Boundary = &Boundary{Reason: reason}
			}
			g.Nodes[from].Boundary.Downstream++
		}
		if !shown[edge.From] && shown[edge.To] {
			if g.Nodes[to].Boundary == nil {
				g.Nodes[to].Boundary = &Boundary{Reason: reason}
			}
			g.Nodes[to].Boundary.Upstream++
		}
		hidden += boolInt(!shown[edge.To])
	}
	nodes := make([]Node, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if shown[node.ID] {
			nodes = append(nodes, node)
		}
	}
	edges := make([]Edge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		if shown[edge.From] && shown[edge.To] {
			edges = append(edges, edge)
		}
	}
	g.Nodes, g.Edges = nodes, edges
	if hidden > 0 {
		g.Unreadable = append(g.Unreadable, Unreadable{Reason: reason, Count: hidden})
	}
	return g.normalized()
}

func Scope(g Graph, p Params) Graph {
	if p.Depth <= 0 {
		p.Depth = 1
	}
	if p.Depth > 3 {
		p.Depth = 3
	}
	if p.Direction == "" {
		p.Direction = "both"
	}
	if p.Mode == "" {
		if p.Focus != "" {
			p.Mode = "focus"
		} else {
			p.Mode = "overview"
		}
	}
	if p.NodeLimit <= 0 {
		p.NodeLimit = defaultNodeLimit
	}
	if p.NodeLimit > maxNodeLimit {
		p.NodeLimit = maxNodeLimit
	}
	if p.EdgeLimit <= 0 {
		p.EdgeLimit = defaultEdgeLimit
	}
	if p.EdgeLimit > maxEdgeLimit {
		p.EdgeLimit = maxEdgeLimit
	}
	g.Total = Total{Nodes: len(g.Nodes), Edges: len(g.Edges)}
	if p.Mode == "overview" && p.Project == "" {
		g.Groups = g.group()
		g.Returned = g.Total
		return g
	}
	if p.Mode == "overview" {
		return projectScope(g, p)
	}
	return focusScope(g, p)
}

func projectScope(g Graph, p Params) Graph {
	index := g.byID()
	keep := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		keep[node.ID] = node.Project == p.Project
	}
	for _, edge := range g.Edges {
		from, fromOK := index[edge.From]
		to, toOK := index[edge.To]
		if !fromOK || !toOK {
			continue
		}
		if keep[edge.From] && !keep[edge.To] {
			if g.Nodes[from].Boundary == nil {
				g.Nodes[from].Boundary = &Boundary{Reason: "other-project"}
			}
			g.Nodes[from].Boundary.Downstream++
		}
		if !keep[edge.From] && keep[edge.To] {
			if g.Nodes[to].Boundary == nil {
				g.Nodes[to].Boundary = &Boundary{Reason: "other-project"}
			}
			g.Nodes[to].Boundary.Upstream++
		}
	}
	nodes := make([]Node, 0, len(g.Nodes))
	edges := make([]Edge, 0, len(g.Edges))
	for _, node := range g.Nodes {
		if keep[node.ID] {
			nodes = append(nodes, node)
		}
	}
	for _, edge := range g.Edges {
		if keep[edge.From] && keep[edge.To] {
			edges = append(edges, edge)
		}
	}
	g.Nodes, g.Edges = nodes, edges
	g.Total = Total{Nodes: len(nodes), Edges: len(edges)}
	g.Groups = g.group()
	return capGraph(g, p)
}

func focusScope(g Graph, p Params) Graph {
	index := g.byID()
	_, ok := index[p.Focus]
	if !ok {
		g.Returned = Total{}
		g.Reasons = append(g.Reasons, "focus-missing")
		g.Coverage.StatusComplete = false
		return g
	}
	distance := map[string]int{p.Focus: 0}
	queue := []string{p.Focus}
	adjacency := map[string][]string{}
	for _, edge := range g.Edges {
		if edge.Unresolved {
			continue
		}
		if p.Direction != "downstream" {
			adjacency[edge.To] = append(adjacency[edge.To], edge.From)
		}
		if p.Direction != "upstream" {
			adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		}
	}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if distance[at] >= p.Depth {
			continue
		}
		for _, next := range adjacency[at] {
			if _, seen := distance[next]; seen {
				continue
			}
			distance[next] = distance[at] + 1
			queue = append(queue, next)
		}
	}
	nodes := make([]Node, 0, len(distance))
	for id, d := range distance {
		i, found := index[id]
		if !found {
			continue
		}
		node := g.Nodes[i]
		node.Distance = d
		nodes = append(nodes, node)
	}
	keep := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		keep[node.ID] = true
	}
	edges := make([]Edge, 0, len(g.Edges))
	for _, edge := range g.Edges {
		if keep[edge.From] && keep[edge.To] {
			edges = append(edges, edge)
			continue
		}
		if keep[edge.From] && edge.To != "" {
			if i, ok := index[edge.From]; ok {
				if g.Nodes[i].Boundary == nil {
					g.Nodes[i].Boundary = &Boundary{Reason: "depth"}
				}
				g.Nodes[i].Boundary.Downstream++
			}
		}
		if keep[edge.To] && edge.From != "" {
			if i, ok := index[edge.To]; ok {
				if g.Nodes[i].Boundary == nil {
					g.Nodes[i].Boundary = &Boundary{Reason: "depth"}
				}
				g.Nodes[i].Boundary.Upstream++
			}
		}
	}
	g.Nodes, g.Edges = nodes, edges
	g.Groups = nil
	g.Total = Total{Nodes: len(nodes), Edges: len(edges)}
	return capGraph(g, p)
}

func capGraph(g Graph, p Params) Graph {
	g.normalized()
	if len(g.Nodes) > p.NodeLimit {
		g.Nodes = g.Nodes[:p.NodeLimit]
		g.Truncated = true
		g.Reasons = append(g.Reasons, "node-limit")
	}
	ids := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		ids[node.ID] = true
	}
	filtered := g.Edges[:0]
	for _, edge := range g.Edges {
		if ids[edge.From] && ids[edge.To] {
			filtered = append(filtered, edge)
		}
	}
	g.Edges = filtered
	if len(g.Edges) > p.EdgeLimit {
		g.Edges = g.Edges[:p.EdgeLimit]
		g.Truncated = true
		g.Reasons = append(g.Reasons, "edge-limit")
	}
	g.Returned = Total{Nodes: len(g.Nodes), Edges: len(g.Edges)}
	g.Reasons = slices.Compact(g.Reasons)
	return g
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

const impactLimit = 200

func ComputeImpact(g Graph, focus string) *Impact {
	if focus == "" {
		return nil
	}
	at := g.byID()
	if _, ok := at[focus]; !ok {
		return nil
	}
	up := reach(g, focus, false)
	down := reach(g, focus, true)
	seen := map[string]int{}
	for id, distance := range up {
		seen[id] = distance
	}
	for id, distance := range down {
		if old, ok := seen[id]; !ok || distance < old {
			seen[id] = distance
		}
	}
	impact := &Impact{Upstream: len(up), Downstream: len(down), ByKind: map[string]int{}, ByProject: map[string]int{}, Items: []ImpactItem{}}
	for id, distance := range seen {
		node := g.Nodes[at[id]]
		impact.ByKind[node.Kind]++
		if node.Project != "" {
			impact.ByProject[node.Project]++
		}
		if node.Status == "failed" {
			impact.Failed++
		}
		if len(node.StaleReasons) > 0 {
			impact.Stale++
		}
		impact.Items = append(impact.Items, ImpactItem{ID: id, Kind: node.Kind, Project: node.Project, Title: node.Title, Distance: distance, Status: node.Status})
	}
	slices.SortFunc(impact.Items, func(a, b ImpactItem) int {
		if a.Distance != b.Distance {
			if a.Distance < b.Distance {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	if len(impact.Items) > impactLimit {
		impact.Items = impact.Items[:impactLimit]
		impact.AtLeast = true
	}
	return impact
}

func reach(g Graph, start string, forward bool) map[string]int {
	distance := map[string]int{}
	queue := []string{start}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, edge := range g.Edges {
			if edge.Unresolved {
				continue
			}
			next := ""
			if forward && edge.From == at {
				next = edge.To
			} else if !forward && edge.To == at {
				next = edge.From
			}
			if next == "" || next == start {
				continue
			}
			if _, ok := distance[next]; ok {
				continue
			}
			distance[next] = distance[at] + 1
			queue = append(queue, next)
		}
	}
	return distance
}
