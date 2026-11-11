package lineage

import (
	"maps"
	"slices"
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type Basis string

const (
	Preview Basis = "preview"
	Guard   Basis = "guard"
)

type Meta struct {
	RID         string
	Project     string
	ProjectName string
	CanOpen     bool
	CanRead     bool
	Visible     bool
}

type Evidence struct {
	Target       string
	Marking      string
	Compartments []string
}

type Input struct {
	Connections []dataowner.Connection
	Sources     []dataowner.Source
	Datasets    []dataowner.Dataset
	Pipelines   []dataowner.Pipeline
	Objects     []build.Object
	Writebacks  []dataowner.Writeback
	Meta        map[string]Meta
	Evidence    []Evidence
	Unreadable  []Unreadable
	Opaque      []string
}

type Unreadable struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type Boundary struct {
	Upstream   int    `json:"upstream"`
	Downstream int    `json:"downstream"`
	Reason     string `json:"reason,omitempty"`
}

type Node struct {
	ID                    string     `json:"id"`
	Kind                  string     `json:"kind"`
	Record                string     `json:"record,omitempty"`
	RID                   string     `json:"rid,omitempty"`
	Project               string     `json:"project,omitempty"`
	Name                  string     `json:"name"`
	Title                 string     `json:"title"`
	State                 string     `json:"state,omitempty"`
	Status                string     `json:"status"`
	StaleReasons          []string   `json:"staleReasons,omitempty"`
	Declared              string     `json:"declared,omitempty"`
	DeclaredCompartments  []string   `json:"-"`
	Effective             string     `json:"effective,omitempty"`
	EffectiveCompartments []string   `json:"-"`
	Guard                 string     `json:"guard,omitempty"`
	GuardCompartments     []string   `json:"-"`
	MarkingStatus         string     `json:"markingStatus"`
	DefinitionVersion     int        `json:"definitionVersion,omitempty"`
	ExecutionVersion      int        `json:"executionVersion,omitempty"`
	DatasetVersion        int        `json:"datasetVersion,omitempty"`
	Rows                  int        `json:"rows,omitempty"`
	At                    *time.Time `json:"at,omitempty"`
	ErrorCode             string     `json:"errorCode,omitempty"`
	CanOpen               bool       `json:"canOpen"`
	CanReadData           bool       `json:"canReadData"`
	Connected             bool       `json:"connected"`
	Distance              int        `json:"distance,omitempty"`
	Boundary              *Boundary  `json:"boundary,omitempty"`
	Unreadable            bool       `json:"unreadable,omitempty"`
}

type Edge struct {
	ID              string   `json:"id"`
	From            string   `json:"from"`
	To              string   `json:"to"`
	Role            string   `json:"role"`
	Occurrence      int      `json:"occurrence,omitempty"`
	DeclaredBy      []string `json:"declaredBy,omitempty"`
	ConsumedVersion int      `json:"consumedVersion,omitempty"`
	CurrentVersion  int      `json:"currentVersion,omitempty"`
	Stale           bool     `json:"stale,omitempty"`
	CrossesProject  bool     `json:"crossesProject,omitempty"`
	Unresolved      bool     `json:"unresolved,omitempty"`
}

type FieldNode struct {
	ID        string `json:"id"`
	Node      string `json:"node"`
	Kind      string `json:"kind"`
	Record    string `json:"record,omitempty"`
	Project   string `json:"project,omitempty"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Certainty string `json:"certainty"`
}

type FieldEdge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Operation  string `json:"operation"`
	Step       int    `json:"step,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
	Certainty  string `json:"certainty"`
}

type Group struct {
	Project     string         `json:"project"`
	Resources   int            `json:"resources"`
	ByKind      map[string]int `json:"byKind"`
	Stale       int            `json:"stale"`
	Failed      int            `json:"failed"`
	NeverRun    int            `json:"neverRun"`
	Unreadable  int            `json:"unreadable"`
	CrossIn     int            `json:"crossIn"`
	CrossOut    int            `json:"crossOut"`
	Unconnected int            `json:"unconnected"`
	Updated     *time.Time     `json:"updated,omitempty"`
	Guard       string         `json:"guard,omitempty"`
}

type ImpactItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Project  string `json:"project,omitempty"`
	Title    string `json:"title"`
	Distance int    `json:"distance"`
	Status   string `json:"status"`
}

type Impact struct {
	Upstream   int            `json:"upstream"`
	Downstream int            `json:"downstream"`
	ByKind     map[string]int `json:"byKind"`
	ByProject  map[string]int `json:"byProject"`
	Stale      int            `json:"stale"`
	Failed     int            `json:"failed"`
	Fields     int            `json:"fields"`
	Items      []ImpactItem   `json:"items"`
	AtLeast    bool           `json:"atLeast"`
}

type Total struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
}

type Graph struct {
	Nodes      []Node
	Edges      []Edge
	Groups     []Group
	Total      Total
	Returned   Total
	Truncated  bool
	Reasons    []string
	Unreadable []Unreadable
	Coverage   Coverage
	Impact     *Impact
	FieldNodes []FieldNode
	FieldEdges []FieldEdge
}

type Coverage struct {
	MarkingsComplete bool `json:"markingsComplete"`
	StatusComplete   bool `json:"statusComplete"`
	FieldsComplete   bool `json:"fieldsComplete"`
}

func (g Graph) node(id string) (Node, bool) {
	for _, node := range g.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Node{}, false
}

func (g Graph) byID() map[string]int {
	out := make(map[string]int, len(g.Nodes))
	for i := range g.Nodes {
		out[g.Nodes[i].ID] = i
	}
	return out
}

func NodeIDs(in Input) []string {
	out := make([]string, 0, len(in.Connections)+len(in.Sources)+len(in.Datasets)+len(in.Pipelines)+len(in.Objects)+len(in.Writebacks))
	for _, item := range in.Connections {
		out = append(out, "connection:"+item.ID)
	}
	for _, item := range in.Sources {
		out = append(out, "source:"+item.ID)
	}
	for _, item := range in.Datasets {
		out = append(out, "dataset:"+item.ID)
	}
	for _, item := range in.Pipelines {
		out = append(out, "pipeline:"+item.ID)
	}
	for _, item := range in.Objects {
		out = append(out, "object:"+nameOf(item.Name))
	}
	for _, item := range in.Writebacks {
		out = append(out, "writeback:"+item.ID)
	}
	return out
}

func nameOf(name string) string {
	if name == "" || containsDot(name) {
		return name
	}
	return "build." + name
}

func containsDot(value string) bool {
	for _, char := range value {
		if char == '.' {
			return true
		}
	}
	return false
}

func (g Graph) normalized() Graph {
	slices.SortStableFunc(g.Nodes, func(a, b Node) int {
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
	slices.SortStableFunc(g.Edges, func(a, b Edge) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return g
}

func (g Graph) Clone() Graph {
	out := g
	out.Nodes = slices.Clone(g.Nodes)
	for i := range out.Nodes {
		out.Nodes[i].StaleReasons = slices.Clone(out.Nodes[i].StaleReasons)
		out.Nodes[i].DeclaredCompartments = slices.Clone(out.Nodes[i].DeclaredCompartments)
		out.Nodes[i].EffectiveCompartments = slices.Clone(out.Nodes[i].EffectiveCompartments)
		out.Nodes[i].GuardCompartments = slices.Clone(out.Nodes[i].GuardCompartments)
		if out.Nodes[i].Boundary != nil {
			boundary := *out.Nodes[i].Boundary
			out.Nodes[i].Boundary = &boundary
		}
	}
	out.Edges = slices.Clone(g.Edges)
	for i := range out.Edges {
		out.Edges[i].DeclaredBy = slices.Clone(out.Edges[i].DeclaredBy)
	}
	out.Unreadable = slices.Clone(g.Unreadable)
	out.Groups = make([]Group, len(g.Groups))
	for i, group := range g.Groups {
		out.Groups[i] = group
		out.Groups[i].ByKind = maps.Clone(group.ByKind)
	}
	if g.Impact != nil {
		impact := *g.Impact
		impact.ByKind = maps.Clone(g.Impact.ByKind)
		impact.ByProject = maps.Clone(g.Impact.ByProject)
		impact.Items = slices.Clone(g.Impact.Items)
		out.Impact = &impact
	}
	out.FieldNodes = slices.Clone(g.FieldNodes)
	out.FieldEdges = slices.Clone(g.FieldEdges)
	return out
}

func ApplyAccess(g Graph, meta map[string]Meta) Graph {
	for i := range g.Nodes {
		value, ok := meta[g.Nodes[i].ID]
		if !ok {
			continue
		}
		g.Nodes[i].RID = value.RID
		g.Nodes[i].Project = value.Project
		g.Nodes[i].CanOpen = value.CanOpen
		g.Nodes[i].CanReadData = value.CanRead
	}
	return g
}

func (g Graph) group() []Group {
	groups := map[string]*Group{}
	at := g.byID()
	for _, node := range g.Nodes {
		project := node.Project
		if project == "" {
			project = "unknown"
		}
		group := groups[project]
		if group == nil {
			group = &Group{Project: project, ByKind: map[string]int{}}
			groups[project] = group
		}
		group.Resources++
		group.ByKind[node.Kind]++
		if !node.Connected {
			group.Unconnected++
		}
		if node.At != nil && (group.Updated == nil || group.Updated.Before(*node.At)) {
			updated := *node.At
			group.Updated = &updated
		}
		switch node.Status {
		case "stale":
			group.Stale++
		case "failed":
			group.Failed++
		case "never-run":
			group.NeverRun++
		case "unknown", "unreadable":
			group.Unreadable++
		}
		group.Guard = platform.HigherMarking(group.Guard, node.Guard)
	}
	for _, edge := range g.Edges {
		from, fromOK := at[edge.From]
		to, toOK := at[edge.To]
		if !fromOK || !toOK {
			continue
		}
		fromProject, toProject := g.Nodes[from].Project, g.Nodes[to].Project
		if fromProject == "" || toProject == "" || fromProject == toProject {
			continue
		}
		if group := groups[fromProject]; group != nil {
			group.CrossOut++
		}
		if group := groups[toProject]; group != nil {
			group.CrossIn++
		}
	}
	out := make([]Group, 0, len(groups))
	for _, group := range groups {
		out = append(out, *group)
	}
	slices.SortFunc(out, func(a, b Group) int {
		if a.Project < b.Project {
			return -1
		}
		if a.Project > b.Project {
			return 1
		}
		return 0
	})
	return out
}
