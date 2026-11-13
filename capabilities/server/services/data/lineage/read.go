package lineage

import (
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type LineageNode Node
type LineageEdge Edge
type LineageGroup Group
type LineageTotal Total
type LineageCoverage Coverage
type LineageUnreadable Unreadable
type LineageImpact Impact
type LineageFieldNode FieldNode
type LineageFieldEdge FieldEdge

type LineageScope struct {
	Mode      string `json:"mode"`
	Project   string `json:"project,omitempty"`
	Focus     string `json:"focus,omitempty"`
	Direction string `json:"direction,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Branch    string `json:"branch,omitempty"`
}

type LineageBasis struct {
	Generation uint64 `json:"generation"`
	Preview    string `json:"preview"`
	Guard      string `json:"guard"`
	BuiltAt    string `json:"builtAt"`
}

type Lineage struct {
	Nodes       []LineageNode       `json:"nodes"`
	Edges       []LineageEdge       `json:"edges"`
	Groups      []LineageGroup      `json:"groups,omitempty"`
	Scope       LineageScope        `json:"scope"`
	Basis       LineageBasis        `json:"basis"`
	Total       LineageTotal        `json:"total"`
	Returned    LineageTotal        `json:"returned"`
	Truncated   bool                `json:"truncated"`
	Truncation  []string            `json:"truncationReasons,omitempty"`
	Unreadable  []LineageUnreadable `json:"unreadable,omitempty"`
	Coverage    LineageCoverage     `json:"coverage"`
	Impact      *LineageImpact      `json:"impact,omitempty"`
	FieldNodes  []LineageFieldNode  `json:"fieldNodes,omitempty"`
	FieldEdges  []LineageFieldEdge  `json:"fieldEdges,omitempty"`
	GeneratedAt time.Time           `json:"generatedAt"`
}

type LineageRequest struct {
	Mode      string
	Project   string
	Focus     string
	Ref       string
	Branch    string
	Direction string
	Depth     int
	NodeLimit int
	EdgeLimit int
	Fields    bool
	Field     string
}

func (r Reader) Read(m platform.Member, request LineageRequest) (Lineage, *kernel.Error) {
	if err := r.host.Admits(m); err != nil {
		return Lineage{}, err
	}
	if !m.Holds(build.ID, build.Builder) && !m.Holds(build.ID, build.Integrator) {
		return Lineage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	now := time.Now()
	var snapshot Snapshot
	var meta map[string]Meta
	var visible map[string]bool
	var focus string
	var refusal *kernel.Error
	if request.Branch != "" {
		mainGeneration := r.host.RecordGeneration()
		branch, branchRefusal := r.host.ReadBranch(m, request.Branch, now)
		if branchRefusal != nil {
			return Lineage{}, branchRefusal
		}
		cacheKey := request.Branch + ":" + strconv.FormatUint(mainGeneration, 10) + ":" + strconv.FormatUint(uint64(branch.Revision), 10)
		world, cachedWorld := r.host.LineageCache().World(cacheKey)
		if !cachedWorld {
			built, refusal := r.host.BranchView(m, request.Branch, now)
			if refusal != nil {
				return Lineage{}, refusal
			}
			world = built
			r.host.LineageCache().KeepWorld(cacheKey, world)
		}
		view := NewReader(world)
		inputs, resources, _ := world.LineageState()
		if cached := r.host.LineageCache().Branch(cacheKey); cached != nil {
			snapshot = *cached
		} else {
			snapshot.Raw = inputs
			snapshot.Base = baseMeta(resources, snapshot.Raw)
		}
		meta, visible = view.access(m, snapshot.Base)
		focus, refusal = focusOf(resources, request)
		if refusal != nil {
			return Lineage{}, refusal
		}
		if snapshot.BuiltAt.IsZero() {
			snapshot.Raw.Meta = snapshot.Base
			snapshot = Snapshot{Generation: mainGeneration, Raw: snapshot.Raw, Base: snapshot.Base, Preview: Build(snapshot.Raw, Preview), Guard: r.mainSnapshot().Guard, BuiltAt: now}
			r.host.LineageCache().KeepBranch(cacheKey, snapshot)
		}
	} else {
		focus, refusal = focusOf(r.host.LineageResources(), request)
		if refusal != nil {
			return Lineage{}, refusal
		}
		snapshot = r.mainSnapshot()
		meta, visible = r.access(m, snapshot.Base)
	}
	raw := snapshot.Raw
	raw.Meta = snapshot.Base
	shaped, refusal := Shape(Request{
		Params: Params{Mode: request.Mode, Project: request.Project, Focus: focus, Direction: request.Direction, Depth: request.Depth, NodeLimit: request.NodeLimit, EdgeLimit: request.EdgeLimit},
		Ref:    request.Ref, Branch: request.Branch, Fields: request.Fields, Field: request.Field,
	}, raw, snapshot.Preview.Clone(), snapshot.Guard.Clone(), meta, visible)
	if refusal != nil {
		return Lineage{}, refusal
	}
	graph := shaped.Graph
	var impact *LineageImpact
	if shaped.Impact != nil {
		converted := LineageImpact(*shaped.Impact)
		impact = &converted
	}
	return Lineage{
		Nodes:       responseNodes(graph.Nodes),
		Edges:       responseEdges(graph.Edges),
		Groups:      responseGroups(graph.Groups),
		Scope:       LineageScope{Mode: shaped.Params.Mode, Project: request.Project, Focus: focus, Direction: shaped.Params.Direction, Depth: shaped.Params.Depth, Branch: request.Branch},
		Basis:       LineageBasis{Generation: snapshot.Generation, Preview: lineagePreviewBasis(request.Branch), Guard: "main · accepted definitions", BuiltAt: snapshot.BuiltAt.UTC().Format(time.RFC3339)},
		Total:       LineageTotal(graph.Total),
		Returned:    LineageTotal(graph.Returned),
		Truncated:   graph.Truncated,
		Truncation:  graph.Reasons,
		Unreadable:  responseUnreadable(graph.Unreadable),
		Coverage:    LineageCoverage(graph.Coverage),
		Impact:      impact,
		FieldNodes:  responseFieldNodes(shaped.FieldNodes),
		FieldEdges:  responseFieldEdges(shaped.FieldEdges),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (r Reader) mainSnapshot() Snapshot {
	if cached, ok := r.host.LineageCache().Main(r.host.RecordGeneration()); ok {
		return cached
	}
	raw, resources, generation := r.host.LineageState()
	base := baseMeta(resources, raw)
	raw.Meta = base
	snapshot := Snapshot{Generation: generation, Raw: raw, Base: base, Preview: Build(raw, Preview), Guard: Build(r.Guard(raw), Guard), BuiltAt: time.Now().UTC()}
	r.host.LineageCache().KeepMain(snapshot)
	return snapshot
}

func lineagePreviewBasis(branch string) string {
	if branch != "" {
		return "branch " + branch + " · saved definitions"
	}
	return "main · saved definitions"
}

func baseMeta(resources []build.Resource, raw Input) map[string]Meta {
	meta := map[string]Meta{}
	projectRID := map[string]string{}
	for _, row := range resources {
		if row.Ref.Kind == platform.AssetApp {
			projectRID[row.Draft] = row.ID
			projectRID[row.Ref.Name] = row.ID
		}
	}
	for _, row := range resources {
		node := lineageNodeID(row)
		if node == "" {
			continue
		}
		project := row.Project
		if rid, ok := projectRID[project]; ok {
			project = rid
		}
		meta[node] = Meta{RID: row.ID, Project: project, ProjectName: row.Project, CanOpen: true, CanRead: true, Visible: true}
	}
	for _, node := range NodeIDs(raw) {
		if _, ok := meta[node]; ok {
			continue
		}
		meta[node] = Meta{CanOpen: true, CanRead: true, Visible: true}
	}
	return meta
}

func (r Reader) access(m platform.Member, base map[string]Meta) (map[string]Meta, map[string]bool) {
	meta := make(map[string]Meta, len(base))
	visible := map[string]bool{}
	for node, entry := range base {
		allowed := true
		if entry.RID != "" || entry.Project != "" {
			project := entry.ProjectName
			if project == "" {
				project = entry.Project
			}
			role := r.host.ResourceRole(m, project)
			allowed = role != "" && role != platform.ProjectDiscovererRole
		} else if build.ProjectDelegated(m) {
			typ, id := lineageNodeType(node)
			if typ != "" && typ != build.ObjectType {
				allowed = r.host.ResourceReadable(m, typ, id)
			}
		}
		entry.CanOpen, entry.CanRead, entry.Visible = allowed, allowed, allowed
		meta[node], visible[node] = entry, allowed
	}
	return meta, visible
}

func focusOf(resources []build.Resource, request LineageRequest) (string, *kernel.Error) {
	value := strings.TrimSpace(request.Focus)
	if value == "" {
		value = strings.TrimSpace(request.Ref)
	}
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, ":") {
		return value, nil
	}
	for _, row := range resources {
		if row.ID == value || row.Ref.App+"/"+string(row.Ref.Kind)+"/"+row.Ref.Name == value {
			return lineageNodeID(row), nil
		}
	}
	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		if len(parts) == 3 {
			for _, row := range resources {
				if row.Ref.App == parts[0] && string(row.Ref.Kind) == parts[1] && row.Ref.Name == parts[2] {
					return lineageNodeID(row), nil
				}
			}
		}
	}
	return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This resource does not exist or you cannot open it")
}

func (r Reader) Guard(input Input) Input {
	owner := r.host.DataOwner().WithInventory(lineageInventory(input))
	for i, item := range input.Connections {
		if fixed, err := owner.ConnectionConfiguration(item); err == nil {
			input.Connections[i] = fixed
		} else {
			input.Opaque = append(input.Opaque, "connection:"+item.ID)
		}
	}
	for i, item := range input.Sources {
		if fixed, err := owner.ExecutionSource(item); err == nil {
			input.Sources[i] = fixed
		} else {
			input.Opaque = append(input.Opaque, "source:"+item.ID)
		}
	}
	for i, item := range input.Datasets {
		if fixed, err := owner.ExecutionDataset(item); err == nil {
			input.Datasets[i] = fixed
		} else {
			input.Opaque = append(input.Opaque, "dataset:"+item.ID)
		}
	}
	for i, item := range input.Pipelines {
		if fixed, err := owner.ExecutionPipeline(item); err == nil {
			input.Pipelines[i] = fixed
		} else {
			input.Opaque = append(input.Opaque, "pipeline:"+item.ID)
		}
	}
	for i, item := range input.Writebacks {
		if fixed, err := owner.ExecutionWriteback(item); err == nil {
			input.Writebacks[i] = fixed
		} else {
			input.Opaque = append(input.Opaque, "writeback:"+item.ID)
		}
	}
	for i, item := range input.Objects {
		if fixed, ok := build.ReadPublication[build.Object](item.Published); ok {
			fixed.Record = item.Record
			fixed.Archived = item.Archived
			input.Objects[i] = fixed
		} else if item.Published != "" {
			input.Opaque = append(input.Opaque, "object:"+lineageObjectType(item.Name))
		}
	}
	input.Evidence = lineageEvidence(input)
	return input
}

func lineageInventory(input Input) dataowner.IntegrationInventory {
	records := map[platform.AssetKind][]any{
		platform.AssetConnection: toAny(input.Connections),
		platform.AssetSource:     toAny(input.Sources),
		platform.AssetDataset:    toAny(input.Datasets),
		platform.AssetPipeline:   toAny(input.Pipelines),
		platform.AssetWriteback:  toAny(input.Writebacks),
	}
	return func(kind platform.AssetKind) ([]any, error) {
		return records[kind], nil
	}
}

func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

func lineageEvidence(input Input) []Evidence {
	connections := map[string]dataowner.Connection{}
	for _, item := range input.Connections {
		connections[item.ID] = item
	}
	out := []Evidence{}
	for _, item := range input.Datasets {
		for _, projection := range item.Projections {
			marking := projection.Marking
			compartments := projection.Compartments
			if marking == "" && len(compartments) == 0 {
				marking, compartments = item.Marking, item.Compartments
			}
			out = append(out, Evidence{Target: "object:" + lineageObjectType(projection.Object), Marking: marking, Compartments: slices.Clone(compartments)})
		}
	}
	for _, item := range input.Sources {
		if item.Object == "" || item.Last == nil {
			continue
		}
		marking, compartments := item.Last.Marking, item.Last.Compartments
		if connection, ok := connections[item.Connection]; ok && marking == "" && len(compartments) == 0 {
			marking, compartments = connection.Marking, connection.Compartments
		}
		out = append(out, Evidence{Target: "object:" + lineageObjectType(item.Object), Marking: marking, Compartments: slices.Clone(compartments)})
	}
	for _, item := range input.Pipelines {
		if item.Last == nil || item.Last.Object == "" {
			continue
		}
		out = append(out, Evidence{Target: "object:" + lineageObjectType(item.Last.Object), Marking: item.Last.Marking, Compartments: slices.Clone(item.Last.Compartments)})
	}
	for _, item := range input.Writebacks {
		if item.Object == "" || len(item.Result) == 0 {
			continue
		}
		if connection, ok := connections[item.Connection]; ok {
			out = append(out, Evidence{Target: "object:" + lineageObjectType(item.Object), Marking: connection.Marking, Compartments: slices.Clone(connection.Compartments)})
		}
	}
	return out
}

func lineageNodeID(resource build.Resource) string {
	switch resource.Ref.Kind {
	case platform.AssetObject:
		return "object:" + resource.Ref.Name
	case platform.AssetConnection:
		return "connection:" + resource.Draft
	case platform.AssetSource:
		return "source:" + resource.Draft
	case platform.AssetDataset:
		return "dataset:" + resource.Draft
	case platform.AssetPipeline:
		return "pipeline:" + resource.Draft
	case platform.AssetWriteback:
		return "writeback:" + resource.Draft
	}
	return ""
}

func lineageNodeType(node string) (string, string) {
	kind, id, ok := strings.Cut(node, ":")
	if !ok {
		return "", ""
	}
	switch kind {
	case "connection":
		return dataowner.ConnectionType, id
	case "source":
		return dataowner.SourceType, id
	case "dataset":
		return dataowner.DatasetType, id
	case "pipeline":
		return dataowner.PipelineType, id
	case "writeback":
		return dataowner.WritebackType, id
	case "object":
		return build.ObjectType, id
	}
	return "", ""
}

func lineageObjectType(name string) string {
	if name == "" || strings.Contains(name, ".") {
		return name
	}
	return build.ID + "." + name
}

func responseNodes(nodes []Node) []LineageNode {
	out := make([]LineageNode, len(nodes))
	for i, node := range nodes {
		out[i] = LineageNode(node)
	}
	return out
}

func responseEdges(edges []Edge) []LineageEdge {
	out := make([]LineageEdge, len(edges))
	for i, edge := range edges {
		out[i] = LineageEdge(edge)
	}
	return out
}

func responseGroups(groups []Group) []LineageGroup {
	out := make([]LineageGroup, len(groups))
	for i, group := range groups {
		out[i] = LineageGroup(group)
	}
	return out
}

func responseUnreadable(items []Unreadable) []LineageUnreadable {
	out := make([]LineageUnreadable, len(items))
	for i, item := range items {
		out[i] = LineageUnreadable(item)
	}
	return out
}

func responseFieldNodes(nodes []FieldNode) []LineageFieldNode {
	out := make([]LineageFieldNode, len(nodes))
	for i, node := range nodes {
		out[i] = LineageFieldNode(node)
	}
	return out
}

func responseFieldEdges(edges []FieldEdge) []LineageFieldEdge {
	out := make([]LineageFieldEdge, len(edges))
	for i, edge := range edges {
		out[i] = LineageFieldEdge(edge)
	}
	return out
}
