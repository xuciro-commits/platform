package host

import (
	"slices"
	"strings"
	"testing"

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
		sub := live.submission("build.connection.create", build.ConnectionType, id, map[string]any{
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
	graph, refusal := live.lineage(delegatedMember, LineageRequest{Mode: "overview"})
	if refusal != nil {
		t.Fatalf("delegated lineage: %+v", refusal)
	}
	if !hasNode(graph, "connection:C1") || hasNode(graph, "connection:C2") {
		t.Fatalf("delegated builder saw the wrong projects: %+v", graph.Nodes)
	}
	if graph.Total.Nodes == 0 || graph.Returned.Nodes == 0 {
		t.Fatalf("delegated overview has no resource: %+v", graph)
	}
	if _, refusal := live.lineage(delegatedMember, LineageRequest{Mode: "focus", Focus: "connection:C2"}); refusal == nil {
		t.Fatal("delegated builder focused another project's resource")
	}

	object := invariantObject("guarded", "Guarded")
	object["project"] = "p1"
	object["datasources"] = []platform.DataSourceBinding{}
	live.accept(t, "build.object.create", build.ObjectType, "OBJ", object)
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ", map[string]any{})
	live.accept(t, "build.guarded.create", "build.guarded", "G1", map[string]any{"note": "secret"})
	dataset := map[string]any{"name": "guarded", "title": "Guarded", "marking": "restricted", "project": "p1"}
	live.accept(t, "build.dataset.create", build.DatasetType, "DS1", dataset)
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "DS1", map[string]any{"rows": []map[string]any{{"id": "A1", "note": "secret"}}, "marking": "restricted"})
	object["datasources"] = []platform.DataSourceBinding{{Kind: "dataset", Source: "DS1", Key: "id", Columns: []platform.DataSourceColumn{{Column: "note", Attribute: "note"}}}}
	delete(object, "project")
	edit := live.submission("build.object.edit", build.ObjectType, "OBJ", object)
	if _, refusal := live.Submit(live.member(t), edit, invariantClock); refusal != nil {
		t.Fatalf("add restricted binding: %s: %s", refusal.Code, refusal.Message)
	}
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ", map[string]any{})
	if _, refusal := live.RecordOf(uncleared.Member, "build.guarded", "G1", invariantClock); refusal == nil {
		t.Fatal("uncleared member read a guarded object before the definition change")
	}

	edited := invariantObject("guarded", "Guarded")
	edited["datasources"] = []platform.DataSourceBinding{}
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ", edited)
	if _, refusal := live.RecordOf(uncleared.Member, "build.guarded", "G1", invariantClock); refusal == nil {
		t.Fatal("unactivated object definition lowered the accepted guard")
	}
	preview, refusal := live.lineage(live.member(t), LineageRequest{Mode: "overview"})
	if refusal != nil {
		t.Fatalf("preview lineage: %+v", refusal)
	}
	node, found := lineageNode(preview, "object:build.guarded")
	if !found || node.Guard != "restricted" || node.MarkingStatus != "preview-only" {
		t.Fatalf("preview/guard mismatch: %+v", node)
	}
}

func hasNode(graph Lineage, id string) bool {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func lineageNode(graph Lineage, id string) (LineageNode, bool) {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return LineageNode{}, false
}

func TestInvariantK12ExportUsesTheDatasetMarking(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-export")
	live.accept(t, "build.dataset.create", build.DatasetType, "OPEN", map[string]any{"name": "open", "title": "Open", "marking": "internal"})
	live.accept(t, "build.dataset.create", build.DatasetType, "CLOSED", map[string]any{"name": "closed", "title": "Closed", "marking": "restricted"})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "OPEN", map[string]any{"rows": []map[string]any{{"id": "A1"}}, "marking": "internal"})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "CLOSED", map[string]any{"rows": []map[string]any{{"id": "B1"}}, "marking": "restricted"})
	row := live.records.Type(build.DatasetVersionType).Rows().Get(build.VersionID("OPEN", 1))
	if row == nil {
		t.Fatal("open dataset version is missing")
	}
	index := live.markingsIn(live.records, false)
	if got := index.highest(build.DatasetVersionType, row); got == "restricted" {
		t.Fatal("open dataset inherited another dataset's restriction")
	}
}

func TestInvariantK12LineageImpactFieldsAndScope(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k12-fields")
	live.accept(t, "build.dataset.create", build.DatasetType, "INPUT", map[string]any{"name": "input", "title": "Input", "key": "id", "keep": 10})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{{"id": "A1", "note": "from input"}}})
	live.accept(t, "build.dataset.create", build.DatasetType, "OUTPUT", map[string]any{"name": "output", "title": "Output", "key": "id", "keep": 10})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "OUTPUT", map[string]any{"rows": []map[string]any{{"id": "A1", "title": "from pipeline"}}})
	live.accept(t, "build.pipeline.create", build.PipelineType, "PIPE", map[string]any{
		"name": "rename", "title": "Rename", "input": "INPUT", "outputDataset": "OUTPUT",
		"steps": []platform.TransformStep{{Kind: "rename", From: "note", To: "title"}},
	})
	object := invariantObject("alpha", "Alpha")
	object["fields"] = append(object["fields"].([]build.Field), build.Field{Name: "manual", Title: "Manual", Type: "text"})
	binding := platform.DataSourceBinding{Kind: "dataset", Source: "OUTPUT", Key: "id", Columns: []platform.DataSourceColumn{{Column: "title", Attribute: "note"}}}
	object["datasources"] = []platform.DataSourceBinding{binding, binding}
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", object)

	shallow, refusal := live.lineage(live.member(t), LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 1, Fields: true})
	if refusal != nil {
		t.Fatalf("shallow lineage: %+v", refusal)
	}
	if shallow.Impact == nil || shallow.Impact.Upstream < 3 {
		t.Fatalf("impact was not computed on the full visible graph: %+v", shallow.Impact)
	}
	deep, refusal := live.lineage(live.member(t), LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 3, Fields: true})
	if refusal != nil {
		t.Fatalf("deep lineage: %+v", refusal)
	}
	if deep.Impact == nil || deep.Impact.Upstream != shallow.Impact.Upstream || deep.Impact.Downstream != shallow.Impact.Downstream {
		t.Fatalf("depth changed impact counts: shallow %+v deep %+v", shallow.Impact, deep.Impact)
	}
	fields := map[string]LineageFieldNode{}
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

	limited, refusal := live.lineage(live.member(t), LineageRequest{Mode: "focus", Focus: "object:build.alpha", Depth: 3, NodeLimit: 1, Fields: true})
	if refusal != nil {
		t.Fatalf("limited lineage: %+v", refusal)
	}
	if !limited.Truncated || !slices.Contains(limited.Truncation, "node-limit") || limited.Returned.Nodes != 1 {
		t.Fatalf("node limit was not reported: %+v", limited)
	}
	if limited.Impact == nil || limited.Impact.Upstream != shallow.Impact.Upstream {
		t.Fatalf("truncation changed impact counts: %+v", limited.Impact)
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
