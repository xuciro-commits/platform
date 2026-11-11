package host

import (
	"platformserver/services/resources/merge"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/data/lineage"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK12LineageGuardAndProjectScope(t *testing.T) {
	delegated := platform.Seat{Subjects: []string{"delegated"}, Member: platform.Member{ID: "delegated", Grants: []platform.Grant{{App: build.ID, Role: build.Builder, By: "project:p1"}}}}
	uncleared := platform.Seat{Subjects: []string{"uncleared"}, Member: platform.Member{ID: "uncleared"}}
	live := newInvariantTenant(t, "invariant-k12", delegated, uncleared)
	live.accept(t, build.AppType+".create", build.AppType, "p1", map[string]any{"name": "projectone", "title": "Project one"})
	live.accept(t, build.AppType+".create", build.AppType, "p2", map[string]any{"name": "projecttwo", "title": "Project two"})
	project := func(id, title string, members []platform.ProjectMember) {
		t.Helper()
		sub := live.submission(platform.SchemaProjectSave, platform.ProjectType, id, map[string]any{"title": title, "members": members})
		sub.Authority = "platform"
		if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal != nil {
			t.Fatalf("%s project save: %s: %s", id, refusal.Code, refusal.Message)
		}
	}
	project("p1", "Project one", []platform.ProjectMember{{Member: "builder", Role: platform.ProjectOwnerRole}, {Member: "delegated", Role: platform.ProjectEditorRole}})
	project("p2", "Project two", []platform.ProjectMember{{Member: "builder", Role: platform.ProjectOwnerRole}})
	connection := func(project, id string) {
		t.Helper()
		sub := live.submission("build.connection.create", dataowner.ConnectionType, id, map[string]any{
			"name": strings.ToLower(id), "title": id, "kind": "http", "address": "https://example.test", "marking": "restricted", "project": project,
		})
		if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal != nil {
			t.Fatalf("%s connection create: %s: %s", id, refusal.Code, refusal.Message)
		}
	}
	connection("p1", "C1")
	connection("p2", "C2")
	delegatedMember, ok := live.Member("delegated")
	if !ok {
		t.Fatal("delegated member is not seated")
	}
	graph, refusal := live.lineage(t.Context(), delegatedMember, lineage.LineageRequest{Mode: "overview"})
	if refusal != nil {
		t.Fatalf("delegated lineage: %+v", refusal)
	}
	if !hasNode(graph, "connection:C1") || hasNode(graph, "connection:C2") {
		t.Fatalf("delegated builder saw the wrong projects: %+v", graph.Nodes)
	}
	if graph.Total.Nodes == 0 || graph.Returned.Nodes == 0 {
		t.Fatalf("delegated overview has no resource: %+v", graph)
	}
	if _, refusal := live.lineage(t.Context(), delegatedMember, lineage.LineageRequest{Mode: "focus", Focus: "connection:C2"}); refusal == nil {
		t.Fatal("delegated builder focused another project's resource")
	}

	object := invariantObject("guarded", "Guarded")
	object["project"] = "p1"
	object["datasources"] = []platform.DataSourceBinding{}
	live.accept(t, "build.object.create", build.ObjectType, "OBJ", object)
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ", map[string]any{})
	live.accept(t, "build.guarded.create", "build.guarded", "G1", map[string]any{"note": "secret"})
	dataset := map[string]any{"name": "guarded", "title": "Guarded", "marking": "restricted", "project": "p1"}
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "DS1", dataset)
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "DS1", map[string]any{"rows": []map[string]any{{"id": "A1", "note": "secret"}}, "marking": "restricted"})
	object["datasources"] = []platform.DataSourceBinding{{Kind: "dataset", Source: "DS1", Key: "id", Columns: []platform.DataSourceColumn{{Column: "note", Attribute: "note"}}}}
	delete(object, "project")
	edit := live.submission("build.object.edit", build.ObjectType, "OBJ", object)
	if _, refusal := live.Submit(live.member(t), edit, invariantClock); refusal != nil {
		t.Fatalf("add restricted binding: %s: %s", refusal.Code, refusal.Message)
	}
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ", map[string]any{})
	if _, refusal := live.RecordOf(t.Context(), uncleared.Member, "build.guarded", "G1", invariantClock); refusal == nil {
		t.Fatal("uncleared member read a guarded object before the definition change")
	}

	edited := invariantObject("guarded", "Guarded")
	edited["datasources"] = []platform.DataSourceBinding{}
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ", edited)
	if _, refusal := live.RecordOf(t.Context(), uncleared.Member, "build.guarded", "G1", invariantClock); refusal == nil {
		t.Fatal("unactivated object definition lowered the accepted guard")
	}
	preview, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "overview"})
	if refusal != nil {
		t.Fatalf("preview lineage: %+v", refusal)
	}
	node, found := lineageNode(preview, "object:build.guarded")
	if !found || node.Guard != "restricted" || node.MarkingStatus != "preview-only" {
		t.Fatalf("preview/guard mismatch: %+v", node)
	}
}

func hasNode(graph lineage.Lineage, id string) bool {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func lineageNode(graph lineage.Lineage, id string) (lineage.LineageNode, bool) {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return lineage.LineageNode{}, false
}

func TestInvariantK12ExportUsesTheDatasetMarking(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-export")
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "OPEN", map[string]any{"name": "open", "title": "Open", "marking": "internal"})
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "CLOSED", map[string]any{"name": "closed", "title": "Closed", "marking": "restricted"})
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "OPEN", map[string]any{"rows": []map[string]any{{"id": "A1"}}, "marking": "internal"})
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "CLOSED", map[string]any{"rows": []map[string]any{{"id": "B1"}}, "marking": "restricted"})
	row := live.Projection.Type(dataowner.DatasetVersionType).Rows().Get(dataowner.VersionID("OPEN", 1))
	if row == nil {
		t.Fatal("open dataset version is missing")
	}
	index := live.markingsIn(live.Projection, false)
	if got := index.Highest(dataowner.DatasetVersionType, row.Value); got == "restricted" {
		t.Fatal("open dataset inherited another dataset's restriction")
	}
}

func TestInvariantK12LineageImpactFieldsAndScope(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-fields")
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "INPUT", map[string]any{"name": "input", "title": "Input", "key": "id", "keep": 10})
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{{"id": "A1", "note": "from input"}}})
	live.accept(t, "build.dataset.create", dataowner.DatasetType, "OUTPUT", map[string]any{"name": "output", "title": "Output", "key": "id", "keep": 10})
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "OUTPUT", map[string]any{"rows": []map[string]any{{"id": "A1", "title": "from pipeline"}}})
	live.accept(t, "build.pipeline.create", dataowner.PipelineType, "PIPE", map[string]any{
		"name": "rename", "title": "Rename", "input": "INPUT", "outputDataset": "OUTPUT",
		"steps": []platform.TransformStep{{Kind: "rename", From: "note", To: "title"}},
	})
	object := invariantObject("alpha", "Alpha")
	object["fields"] = append(object["fields"].([]build.Field), build.Field{Name: "manual", Title: "Manual", Type: "text"})
	binding := platform.DataSourceBinding{Kind: "dataset", Source: "OUTPUT", Key: "id", Columns: []platform.DataSourceColumn{{Column: "title", Attribute: "note"}}}
	object["datasources"] = []platform.DataSourceBinding{binding, binding}
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", object)

	shallow, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 1, Fields: true})
	if refusal != nil {
		t.Fatalf("shallow lineage: %+v", refusal)
	}
	if shallow.Impact == nil || shallow.Impact.Upstream < 3 {
		t.Fatalf("impact was not computed on the full visible graph: %+v", shallow.Impact)
	}
	deep, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 3, Fields: true})
	if refusal != nil {
		t.Fatalf("deep lineage: %+v", refusal)
	}
	if deep.Impact == nil || deep.Impact.Upstream != shallow.Impact.Upstream || deep.Impact.Downstream != shallow.Impact.Downstream {
		t.Fatalf("depth changed impact counts: shallow %+v deep %+v", shallow.Impact, deep.Impact)
	}
	fields := map[string]lineage.LineageFieldNode{}
	for _, field := range deep.FieldNodes {
		fields[field.ID] = field
	}
	for _, id := range []string{"dataset:OUTPUT#title", "pipeline:PIPE#title", "object:build.alpha#note"} {
		if _, ok := fields[id]; !ok {
			t.Fatalf("field %s is missing from the graph: %+v", id, deep.FieldNodes)
		}
	}
	if got := fields["object:build.alpha#manual"].Certainty; got != "unverified" {
		t.Fatalf("a field without a declared source is %q, want unverified", got)
	}
	edge := false
	for _, item := range deep.FieldEdges {
		if item.From == "dataset:OUTPUT#title" && item.To == "object:build.alpha#note" && item.Operation == "bind" {
			edge = true
		}
	}
	if !edge {
		t.Fatalf("object binding is missing from field edges: %+v", deep.FieldEdges)
	}
	bindings := map[string]bool{}
	for _, item := range deep.Edges {
		if item.From == "dataset:OUTPUT" && item.To == "object:build.alpha" && item.Role == "binding" {
			bindings[item.ID] = true
		}
	}
	if len(bindings) != 2 {
		t.Fatalf("same-pair binding edges were merged: %+v", bindings)
	}

	limited, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 3, NodeLimit: 1, Fields: true})
	if refusal != nil {
		t.Fatalf("limited lineage: %+v", refusal)
	}
	if !limited.Truncated || !slices.Contains(limited.Truncation, "node-limit") || limited.Returned.Nodes != 1 {
		t.Fatalf("node limit was not reported: %+v", limited)
	}
	if _, ok := lineageNode(limited, "object:build.alpha"); !ok {
		t.Fatalf("node limit dropped the focus: %+v", limited.Nodes)
	}
	if limited.Impact == nil || limited.Impact.Upstream != shallow.Impact.Upstream {
		t.Fatalf("truncation changed impact counts: %+v", limited.Impact)
	}
	edgeLimited, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 3, EdgeLimit: 1})
	if refusal != nil {
		t.Fatalf("edge-limited lineage: %+v", refusal)
	}
	if !edgeLimited.Truncated || !slices.Contains(edgeLimited.Truncation, "edge-limit") || edgeLimited.Returned.Edges != 1 || edgeLimited.Total.Edges <= edgeLimited.Returned.Edges {
		t.Fatalf("edge limit lost its counts or reason: %+v", edgeLimited)
	}
}

func TestInvariantK12UnresolvedLineageEdgesRemainVisible(t *testing.T) {
	graph := lineage.Build(lineage.Input{
		Sources: []dataowner.Source{{Record: platform.Record{ID: "S"}, Name: "feed", Title: "Feed", Connection: "missing", Dataset: "missing"}},
	}, lineage.Preview)
	if len(graph.Nodes) != 1 || graph.Nodes[0].ID != "source:S" {
		t.Fatalf("the source root was lost: %+v", graph.Nodes)
	}
	unresolved := 0
	for _, edge := range graph.Edges {
		if edge.Unresolved {
			unresolved++
		}
	}
	if unresolved != 2 {
		t.Fatalf("dangling edges were dropped: %+v", graph.Edges)
	}
	if len(graph.Unreadable) == 0 || graph.Unreadable[0].Reason != "missing-reference" {
		t.Fatalf("unresolved edges were not counted: %+v", graph.Unreadable)
	}
}

func TestInvariantK12LineageGuardNeverFallsBelowWriteEvidence(t *testing.T) {
	graph := lineage.Build(lineage.Input{
		Objects:  []build.Object{{Name: "alpha", Title: "Alpha"}},
		Evidence: []lineage.Evidence{{Target: "object:build.alpha", Marking: "restricted"}},
	}, lineage.Guard)
	for _, node := range graph.Nodes {
		if node.ID == "object:build.alpha" {
			if node.Guard != "restricted" {
				t.Fatalf("guard fell below write evidence: %+v", node)
			}
			return
		}
	}
	t.Fatal("object node is missing")
}

func TestInvariantK12LineageGuardFailsClosedWhenAcceptedDefinitionIsUnreadable(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-unreadable")
	guard := lineage.NewReader(live.Tenant).Guard(lineage.Input{
		Objects: []build.Object{{Name: "alpha", Title: "Alpha", Published: "{"}},
	})
	graph := lineage.Build(guard, lineage.Guard)
	if graph.Coverage.MarkingsComplete {
		t.Fatal("an unreadable accepted definition must make marking coverage incomplete")
	}
	for _, node := range graph.Nodes {
		if node.ID == "object:build.alpha" {
			if node.Guard != "restricted" {
				t.Fatalf("unreadable accepted definition did not fail closed: %+v", node)
			}
			return
		}
	}
	t.Fatal("object node is missing")
}

func TestInvariantK12LineageBranchReadsStagedDefinitionsWithoutChangingMain(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-branch")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ", invariantObject("alpha", "Alpha"))
	branch, refusal := live.CreateBranch(live.member(t), branches.BranchCreateRequest{Name: "lineage", Title: "Lineage"}, invariantClock)
	if refusal != nil {
		t.Fatalf("create branch: %+v", refusal)
	}
	changed := invariantObject("alpha", "Alpha branch")
	changed["fields"] = append(changed["fields"].([]build.Field), build.Field{Name: "detail", Title: "Detail", Type: "text"})
	if _, refusal = live.SaveBranchResource(live.member(t), branch.ID, build.ObjectType, "OBJ", branches.BranchSaveRequest{Spec: platform.Raw(changed)}, invariantClock); refusal != nil {
		t.Fatalf("save branch object: %+v", refusal)
	}
	main, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Focus: "object:build.alpha"})
	if refusal != nil {
		t.Fatalf("main lineage: %+v", refusal)
	}
	branched, refusal := live.lineage(t.Context(), live.member(t), lineage.LineageRequest{Mode: "focus", Branch: branch.ID, Focus: "object:build.alpha"})
	if refusal != nil {
		t.Fatalf("branch lineage: %+v", refusal)
	}
	if node, ok := lineageNode(main, "object:build.alpha"); !ok || node.Title != "Alpha" {
		t.Fatalf("main lineage changed by branch save: %+v", node)
	}
	node, ok := lineageNode(branched, "object:build.alpha")
	if !ok || node.Title != "Alpha branch" || branched.Scope.Branch != branch.ID {
		t.Fatalf("branch lineage did not read staged definitions: %+v scope=%+v", node, branched.Scope)
	}
}

func TestInvariantK12LineageBuildsThousandResourcesWithoutSilentLoss(t *testing.T) {
	input := lineage.Input{Meta: map[string]lineage.Meta{}}
	for i := 0; i < 1200; i++ {
		id := "DS" + strconv.Itoa(i)
		input.Datasets = append(input.Datasets, dataowner.Dataset{
			Record: platform.Record{ID: id},
			Name:   "dataset" + strconv.Itoa(i),
			Title:  "Dataset " + strconv.Itoa(i),
			Schema: []dataowner.DatasetField{{Name: "id", Type: "text"}},
		})
		input.Meta["dataset:"+id] = lineage.Meta{Project: "p1"}
	}
	start := time.Now()
	graph := lineage.Build(input, lineage.Preview)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("building 1200 resources took %s", elapsed)
	}
	if len(graph.Nodes) != 1200 || graph.Total.Nodes != 1200 || graph.Truncated {
		t.Fatalf("1200 resources were silently lost or truncated: nodes=%d total=%+v truncated=%v", len(graph.Nodes), graph.Total, graph.Truncated)
	}
	scoped := lineage.Scope(graph, lineage.Params{Mode: "overview", Project: "p1", NodeLimit: 2000, EdgeLimit: 5000})
	if scoped.Total.Nodes != 1200 || scoped.Returned.Nodes != 1200 || len(scoped.Nodes) != 1200 || scoped.Truncated {
		t.Fatalf("1200 scoped resources were silently lost or truncated: nodes=%d total=%+v returned=%+v truncated=%v", len(scoped.Nodes), scoped.Total, scoped.Returned, scoped.Truncated)
	}
	limited := lineage.Scope(graph.Clone(), lineage.Params{Mode: "overview", Project: "p1", NodeLimit: 100, EdgeLimit: 5000})
	if limited.Total.Nodes != 1200 || limited.Returned.Nodes != 100 || !limited.Truncated || !slices.Contains(limited.Reasons, "node-limit") {
		t.Fatalf("scoped truncation lost its counts or reason: total=%+v returned=%+v truncated=%v reasons=%v", limited.Total, limited.Returned, limited.Truncated, limited.Reasons)
	}
}

func TestInvariantK12LineageFieldDerivationAndWriteback(t *testing.T) {
	input := lineage.Input{
		Connections: []dataowner.Connection{{Record: platform.Record{ID: "C"}, Name: "conn", Title: "Connection"}},
		Datasets: []dataowner.Dataset{
			{Record: platform.Record{ID: "IN"}, Name: "input", Title: "Input", Schema: []dataowner.DatasetField{{Name: "note", Type: "text"}, {Name: "key", Type: "text"}}},
			{Record: platform.Record{ID: "LOOK"}, Name: "lookup", Title: "Lookup", Schema: []dataowner.DatasetField{{Name: "value", Type: "text"}, {Name: "other", Type: "text"}, {Name: "key", Type: "text"}}},
			{Record: platform.Record{ID: "OUT"}, Name: "output", Title: "Output", Schema: []dataowner.DatasetField{{Name: "label", Type: "text"}, {Name: "value", Type: "text"}}},
			{Record: platform.Record{ID: "JOINED"}, Name: "joined", Title: "Joined", Schema: []dataowner.DatasetField{{Name: "value", Type: "text"}}},
		},
		Pipelines: []dataowner.Pipeline{
			{Record: platform.Record{ID: "COMPUTE"}, Name: "compute", Title: "Compute", Input: "IN", OutputDataset: "OUT",
				Steps: []platform.TransformStep{{Kind: "compute", To: "label", FormulaExpr: &platform.Expr{Kind: "text", Ref: "note"}}}},
			{Record: platform.Record{ID: "LOOKUP"}, Name: "lookup", Title: "Lookup", Input: "IN", OutputDataset: "OUT",
				Steps: []platform.TransformStep{{Kind: "lookup", Dataset: "LOOK", Columns: []string{"value"}, Column: "key", Match: "key"}}},
			{Record: platform.Record{ID: "JOIN"}, Name: "join", Title: "Join", Input: "IN", OutputDataset: "JOINED",
				Steps: []platform.TransformStep{{Kind: "join", Dataset: "LOOK"}}},
		},
		Objects: []build.Object{{
			Record: platform.Record{ID: "OBJ"}, Name: "alpha", Title: "Alpha",
			Fields: []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "manual", Title: "Manual", Type: "text"}},
		}},
		Writebacks: []dataowner.Writeback{{
			Record: platform.Record{ID: "WB"}, Name: "write", Title: "Write", Connection: "C", Object: "alpha",
			Mapping: []dataowner.SourceField{{From: "note", To: "body"}},
			Result:  []dataowner.SourceField{{From: "status", To: "note"}},
		}},
	}
	graph := lineage.Build(input, lineage.Preview)
	fields, edges := lineage.Fields(graph, input)
	find := func(id string) (lineage.FieldNode, bool) {
		for _, field := range fields {
			if field.ID == id {
				return field, true
			}
		}
		return lineage.FieldNode{}, false
	}
	hasEdge := func(from, to, operation string) bool {
		for _, edge := range edges {
			if edge.From == from && edge.To == to && edge.Operation == operation {
				return true
			}
		}
		return false
	}
	if field, ok := find("pipeline:COMPUTE#label"); !ok || field.Certainty != "derived" {
		t.Fatalf("FormulaExpr field was not derived: %+v", field)
	}
	if !hasEdge("dataset:IN#note", "pipeline:COMPUTE#label", "compute") {
		t.Fatalf("FormulaExpr did not trace the source field: %+v", edges)
	}
	if !hasEdge("dataset:LOOK#value", "pipeline:LOOKUP#value", "lookup") || hasEdge("dataset:LOOK#other", "pipeline:LOOKUP#other", "lookup") {
		t.Fatalf("lookup traced fields it did not declare: %+v", edges)
	}
	if !hasEdge("dataset:IN#key", "pipeline:LOOKUP#key", "key") || !hasEdge("dataset:LOOK#key", "pipeline:LOOKUP#key", "key") {
		t.Fatalf("lookup keys were not traced: %+v", edges)
	}
	if field, ok := find("pipeline:JOIN#other"); !ok || field.Certainty != "derived" {
		t.Fatalf("join did not derive its unlisted fields: %+v", field)
	}
	if !hasEdge("object:build.alpha#note", "writeback:WB#body", "request") || !hasEdge("writeback:WB#status", "object:build.alpha#note", "result") {
		t.Fatalf("writeback request/result fields were not traced: %+v", edges)
	}
}
