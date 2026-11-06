package lineage

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type fieldState struct {
	sources   []string
	certainty string
}

type fieldGraph struct {
	nodes map[string]*FieldNode
	edges map[string]FieldEdge
}

func Fields(g Graph, in Input) ([]FieldNode, []FieldEdge) {
	graph := fieldGraph{nodes: map[string]*FieldNode{}, edges: map[string]FieldEdge{}}
	visible := map[string]bool{}
	for _, node := range g.Nodes {
		visible[node.ID] = true
	}
	addNode := func(node, kind, record, name, title, certainty string) string {
		if node == "" || name == "" || !visible[node] {
			return ""
		}
		if certainty == "" {
			certainty = "unverified"
		}
		id := node + "#" + name
		if existing, ok := graph.nodes[id]; !ok {
			graph.nodes[id] = &FieldNode{ID: id, Node: node, Kind: kind, Record: record, Name: name, Title: title, Certainty: certainty, Project: in.Meta[node].Project}
		} else {
			if existing.Certainty == "unverified" && certainty != "unverified" {
				existing.Certainty = certainty
			}
			if existing.Title == existing.Name && title != name {
				existing.Title = title
			}
		}
		return id
	}
	addEdge := func(from, to, operation string, step, occurrence int, certainty string) {
		if from == "" || to == "" || from == to {
			return
		}
		id := fmt.Sprintf("%s>%s#%s", from, to, operation)
		if step > 0 || occurrence > 0 {
			id += fmt.Sprintf(":%d:%d", step, occurrence)
		}
		if _, ok := graph.edges[id]; !ok {
			graph.edges[id] = FieldEdge{ID: id, From: from, To: to, Operation: operation, Step: step, Occurrence: occurrence, Certainty: certainty}
		}
	}

	datasets := map[string]dataowner.Dataset{}
	for _, dataset := range in.Datasets {
		datasets[dataset.ID] = dataset
		for _, field := range dataset.Schema {
			addNode("dataset:"+dataset.ID, "dataset", dataset.ID, field.Name, field.Name, "unverified")
		}
	}
	objects := map[string]build.Object{}
	for _, object := range in.Objects {
		name := objectType(object.Name)
		objects[name] = object
		for _, field := range object.Fields {
			addNode("object:"+name, "object", object.ID, field.Name, field.Title, "unverified")
		}
	}

	for _, source := range in.Sources {
		fromNode := "source:" + source.ID
		for occurrence, mapping := range source.Mapping {
			if mapping.From == "" || mapping.To == "" {
				continue
			}
			from := addNode(fromNode, "source", source.ID, mapping.From, mapping.From, "declared")
			targets := ""
			if source.Dataset != "" {
				targets = "dataset:" + source.Dataset
			}
			if source.Object != "" {
				targets = "object:" + objectType(source.Object)
			}
			if targets == "" {
				continue
			}
			if strings.HasPrefix(targets, "dataset:") {
				to := addNode(targets, "dataset", strings.TrimPrefix(targets, "dataset:"), mapping.To, mapping.To, "declared")
				addEdge(from, to, "map", 0, occurrence, "declared")
			} else {
				to := addNode(targets, "object", objects[strings.TrimPrefix(targets, "object:")].ID, mapping.To, mapping.To, "declared")
				addEdge(from, to, "map", 0, occurrence, "declared")
			}
		}
		if source.Key != "" && source.Object != "" {
			from := addNode(fromNode, "source", source.ID, source.Key, source.Key, "declared")
			to := addNode("object:"+objectType(source.Object), "object", objects[objectType(source.Object)].ID, "id", "ID", "declared")
			addEdge(from, to, "key", 0, 0, "declared")
		}
	}

	for _, pipeline := range in.Pipelines {
		pipelineNode := "pipeline:" + pipeline.ID
		current := map[string]fieldState{}
		if input, ok := datasets[pipeline.Input]; ok {
			for _, field := range input.Schema {
				current[field.Name] = fieldState{sources: []string{fieldNode("dataset:"+input.ID, field.Name)}, certainty: "declared"}
			}
		}
		for stepIndex, step := range pipeline.Steps {
			next := cloneFields(current)
			switch step.Kind {
			case "select":
				selected := map[string]fieldState{}
				for _, column := range step.Columns {
					if value, ok := current[column]; ok {
						selected[column] = value
					} else {
						selected[column] = fieldState{certainty: "unverified"}
					}
				}
				next = selected
			case "rename":
				if step.From != "" && step.To != "" {
					state, ok := current[step.From]
					if !ok {
						state = fieldState{certainty: "unverified"}
					}
					delete(next, step.From)
					next[step.To] = state
					produce(addNode, addEdge, pipeline, pipelineNode, step.To, step.To, state, "rename", stepIndex, 0)
				}
			case "cast":
				if step.Column != "" {
					state := current[step.Column]
					next[step.Column] = state
					produce(addNode, addEdge, pipeline, pipelineNode, step.Column, step.Column, state, "cast", stepIndex, 0)
				}
			case "compute":
				if step.To == "" {
					continue
				}
				state := fieldState{certainty: "derived"}
				if used, err := platform.FormulaFields(step.Formula); err == nil {
					for name := range used {
						if value, ok := current[name]; ok {
							state.sources = append(state.sources, value.sources...)
							state.certainty = mergeCertainty(state.certainty, value.certainty)
						}
					}
				}
				slices.Sort(state.sources)
				state.sources = slices.Compact(state.sources)
				next[step.To] = state
				produce(addNode, addEdge, pipeline, pipelineNode, step.To, step.To, state, "compute", stepIndex, 0)
			case "lookup", "join":
				right, ok := datasets[step.Dataset]
				if !ok {
					continue
				}
				columns := step.Columns
				if len(columns) == 0 {
					for _, field := range right.Schema {
						columns = append(columns, field.Name)
					}
				}
				for occurrence, column := range columns {
					target := column
					if step.As != "" {
						target = step.As + "." + column
					}
					state := fieldState{sources: []string{fieldNode("dataset:"+right.ID, column)}, certainty: "declared"}
					next[target] = state
					produce(addNode, addEdge, pipeline, pipelineNode, target, target, state, step.Kind, stepIndex, occurrence)
				}
			case "aggregate":
				aggregated := map[string]fieldState{}
				for _, column := range step.Columns {
					if value, ok := current[column]; ok {
						aggregated[column] = value
					}
				}
				for occurrence, measure := range step.Measures {
					if measure.To == "" {
						continue
					}
					state := fieldState{}
					if measure.Column != "" {
						if value, ok := current[measure.Column]; ok {
							state = value
						} else {
							state = fieldState{sources: []string{fieldNode(pipelineNode, measure.Column)}, certainty: "unverified"}
						}
					}
					aggregated[measure.To] = state
					produce(addNode, addEdge, pipeline, pipelineNode, measure.To, measure.To, state, "aggregate", stepIndex, occurrence)
				}
				next = aggregated
			}
			current = next
		}
		for name, state := range current {
			id := addNode(pipelineNode, "pipeline", pipeline.ID, name, name, state.certainty)
			for _, source := range state.sources {
				addEdge(source, id, "output", 0, 0, state.certainty)
			}
		}
		for name, state := range current {
			from := addNode(pipelineNode, "pipeline", pipeline.ID, name, name, state.certainty)
			if pipeline.OutputDataset != "" {
				to := addNode("dataset:"+pipeline.OutputDataset, "dataset", pipeline.OutputDataset, name, name, state.certainty)
				addEdge(from, to, "output", 0, 0, state.certainty)
			}
			if pipeline.OutputObject != "" {
				to := addNode("object:"+objectType(pipeline.OutputObject), "object", objects[objectType(pipeline.OutputObject)].ID, name, name, state.certainty)
				addEdge(from, to, "output", 0, 0, state.certainty)
			}
		}
	}

	for _, object := range in.Objects {
		objectName := objectType(object.Name)
		objectNode := "object:" + objectName
		for occurrence, binding := range object.DataSources {
			fromNode := binding.Kind + ":" + binding.Source
			for _, column := range binding.Columns {
				from := addNode(fromNode, binding.Kind, binding.Source, column.Column, column.Column, "declared")
				to := addNode(objectNode, "object", object.ID, column.Attribute, column.Attribute, "declared")
				addEdge(from, to, "bind", 0, occurrence, "declared")
			}
			if binding.Key != "" {
				from := addNode(fromNode, binding.Kind, binding.Source, binding.Key, binding.Key, "declared")
				to := addNode(objectNode, "object", object.ID, "id", "ID", "declared")
				addEdge(from, to, "key", 0, occurrence, "declared")
			}
		}
	}

	for _, writeback := range in.Writebacks {
		writebackNode := "writeback:" + writeback.ID
		objectNode := "object:" + objectType(writeback.Object)
		for occurrence, mapping := range writeback.Mapping {
			from := addNode(objectNode, "object", objects[objectType(writeback.Object)].ID, mapping.From, mapping.From, "declared")
			to := addNode(writebackNode, "writeback", writeback.ID, mapping.To, mapping.To, "declared")
			addEdge(from, to, "request", 0, occurrence, "declared")
		}
		for occurrence, mapping := range writeback.Result {
			from := addNode(writebackNode, "writeback", writeback.ID, mapping.From, mapping.From, "declared")
			to := addNode(objectNode, "object", objects[objectType(writeback.Object)].ID, mapping.To, mapping.To, "declared")
			addEdge(from, to, "result", 0, occurrence, "declared")
		}
	}

	nodes := make([]FieldNode, 0, len(graph.nodes))
	for _, node := range graph.nodes {
		nodes = append(nodes, *node)
	}
	slices.SortFunc(nodes, func(a, b FieldNode) int {
		if a.Node != b.Node {
			return strings.Compare(a.Node, b.Node)
		}
		return strings.Compare(a.Name, b.Name)
	})
	edges := make([]FieldEdge, 0, len(graph.edges))
	for _, edge := range graph.edges {
		edges = append(edges, edge)
	}
	slices.SortFunc(edges, func(a, b FieldEdge) int { return strings.Compare(a.ID, b.ID) })
	return nodes, edges
}

func produce(addNode func(string, string, string, string, string, string) string, addEdge func(string, string, string, int, int, string), pipeline dataowner.Pipeline, pipelineNode, name, title string, state fieldState, operation string, step, occurrence int) {
	id := addNode(pipelineNode, "pipeline", pipeline.ID, name, title, state.certainty)
	for _, source := range state.sources {
		addEdge(source, id, operation, step+1, occurrence, state.certainty)
	}
}

func cloneFields(in map[string]fieldState) map[string]fieldState {
	out := make(map[string]fieldState, len(in))
	for name, state := range in {
		out[name] = fieldState{sources: slices.Clone(state.sources), certainty: state.certainty}
	}
	return out
}

func fieldNode(node, name string) string {
	return node + "#" + name
}

func mergeCertainty(a, b string) string {
	if a == "derived" || b == "derived" {
		return "derived"
	}
	if a == "unverified" || b == "unverified" {
		return "derived"
	}
	return "declared"
}

func FieldTrace(nodes []FieldNode, edges []FieldEdge, field string) ([]FieldNode, []FieldEdge) {
	if field == "" {
		return nodes, edges
	}
	known := map[string]bool{}
	for _, node := range nodes {
		known[node.ID] = true
	}
	if !known[field] {
		return nil, nil
	}
	seen := map[string]bool{field: true}
	seenEdges := map[string]bool{}
	queue := []string{field}
	keptEdges := make([]FieldEdge, 0, len(edges))
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, edge := range edges {
			if edge.From != at && edge.To != at {
				continue
			}
			if seenEdges[edge.ID] {
				continue
			}
			seenEdges[edge.ID] = true
			keptEdges = append(keptEdges, edge)
			next := edge.To
			if edge.To == at {
				next = edge.From
			}
			if !known[next] || seen[next] {
				continue
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	keptNodes := make([]FieldNode, 0, len(seen))
	for _, node := range nodes {
		if seen[node.ID] {
			keptNodes = append(keptNodes, node)
		}
	}
	return keptNodes, keptEdges
}

func ScopeFields(g Graph, nodes []FieldNode, edges []FieldEdge) ([]FieldNode, []FieldEdge) {
	keep := map[string]bool{}
	for _, node := range g.Nodes {
		keep[node.ID] = true
	}
	filtered := make([]FieldNode, 0, len(nodes))
	for _, node := range nodes {
		if keep[node.Node] {
			filtered = append(filtered, node)
		}
	}
	ids := map[string]bool{}
	for _, node := range filtered {
		ids[node.ID] = true
	}
	filteredEdges := make([]FieldEdge, 0, len(edges))
	for _, edge := range edges {
		if ids[edge.From] && ids[edge.To] {
			filteredEdges = append(filteredEdges, edge)
		}
	}
	return filtered, filteredEdges
}
