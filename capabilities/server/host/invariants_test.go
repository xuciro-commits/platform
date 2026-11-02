package host

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantK1ReplayIsByteIdentical(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k1")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.create", build.ObjectType, "OBJ2", invariantObject("beta", "Beta"))
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, revised"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "first"})
	live.accept(t, "build.alpha.create", "build.alpha", "A2", map[string]any{"note": "second"})
	stale := live.submission("build.object.edit", build.ObjectType, "OBJ2", invariantObject("beta", "Beta, stale"))
	zero := uint32(0)
	stale.ExpectedRevision = &zero
	if _, refusal := live.Submit(live.member(t), stale, invariantClock); refusal == nil {
		t.Fatal("the stale submission was accepted")
	}
	if len(live.entries) != 7 {
		t.Fatalf("the journal holds %d entries; want 6 accepted results and 1 sealed refusal", len(live.entries))
	}
	want := live.recordImage(t)
	for _, typ := range []string{build.ObjectType, "build.alpha"} {
		if len(want[typ]) == 0 {
			t.Fatalf("the live record store has no %s rows", typ)
		}
	}
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs from the live record store in %v", replay, differing)
		}
	}
}

func TestInvariantK1ObjectViewReleaseReplayPreservesHistory(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k1-object-view")
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}
	list := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "alphalist"}
	detail := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "alphadetail"}
	application := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "alphaworkspace"}
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.page.create", build.PageType, "LIST1", map[string]any{
		"name": list.Name, "title": "Alpha list", "object": object.Name, "list": []string{"note"}, "detail": []string{"note"},
	})
	detailDraft := map[string]any{
		"name": detail.Name, "title": "Alpha detail", "object": object.Name,
		"sections": []build.Section{{ID: "details", Widget: "detail", Fields: []string{"note"}, RecordVariable: "currentObject"}},
		"document": platform.PageDocument{
			FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root",
			Nodes:     map[string]platform.PageLayoutNode{"root": {Kind: "section", Section: "details"}},
			Variables: map[string]platform.PageVariable{"currentObject": {Scope: "page", Type: "record", Mode: "input"}},
			Interface: &platform.PageInterface{Version: 1, Inputs: map[string]platform.PagePort{
				"currentObject": {Variable: "currentObject", Type: "record", Object: &object, Required: true},
			}},
		},
	}
	live.accept(t, "build.page.create", build.PageType, "DETAIL1", detailDraft)
	live.accept(t, "build.app.create", build.AppType, "APP1", map[string]any{
		"name": application.Name, "title": "Alpha workspace", "pages": []string{list.Name, detail.Name}, "resources": []platform.AssetRef{object},
	})
	drafts := []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"}, {Kind: platform.AssetPage, ID: "LIST1"},
		{Kind: platform.AssetPage, ID: "DETAIL1"}, {Kind: platform.AssetApp, ID: "APP1"},
	}
	publish := func(key string) string {
		t.Helper()
		preview, err := live.PreviewReleaseDrafts(live.member(t), drafts, "", invariantClock)
		if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
			t.Fatalf("%s preview: %+v: %v", key, preview, err)
		}
		for _, ref := range []platform.AssetRef{object, list, detail, application} {
			if !slices.Contains(preview.Included, ref) {
				t.Fatalf("%s preview omits %s", key, ref)
			}
		}
		id, err := live.SaveReleaseCandidates(live.member(t), drafts, preview.CandidateID, key, invariantClock)
		if err != nil || id != preview.CandidateID {
			t.Fatalf("%s save: id=%s: %v", key, id, err)
		}
		if active, err := live.ActivateRelease(live.member(t), id, key, invariantClock); err != nil || active != id {
			t.Fatalf("%s activate: id=%s: %v", key, active, err)
		}
		return id
	}
	pagePublication := func(tenant *invariantTenant) build.Page {
		t.Helper()
		view, refusal := tenant.RecordOf(tenant.member(t), build.PageType, "DETAIL1", invariantClock)
		if refusal != nil {
			t.Fatalf("read detail publication: %v", refusal)
		}
		raw, err := json.Marshal(view.Record)
		if err != nil {
			t.Fatal(err)
		}
		var page build.Page
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	oldID := publish("before-object-view")
	oldBytes := bytes.Clone(live.releases.Raw(oldID))
	oldPage := pagePublication(live)
	if oldPage.Published == "" || !slices.Contains(oldPage.Versions, oldPage.Published) {
		t.Fatal("the first joint release did not retain its page publication")
	}
	oldCandidate, err := platform.ReadCandidate(oldID, oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range oldCandidate.Assets {
		if asset.Ref == object {
			var saved build.Object
			if err := json.Unmarshal(asset.Body, &saved); err != nil || saved.ObjectView != nil {
				t.Fatalf("the old candidate already contains an object view: %+v: %v", saved.ObjectView, err)
			}
		}
	}
	wantView := &platform.ObjectView{Application: application, Page: detail}
	objectDraft := invariantObject("alpha", "Alpha")
	objectDraft["objectView"] = wantView
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", objectDraft)
	detailDraft["title"] = "Alpha object view"
	live.accept(t, "build.page.edit", build.PageType, "DETAIL1", detailDraft)
	activeID := publish("with-object-view")
	if activeID == oldID || !bytes.Equal(oldBytes, live.releases.Raw(oldID)) {
		t.Fatal("adding the object view reused or rewrote the old candidate")
	}
	currentPage := pagePublication(live)
	if currentPage.Published == oldPage.Published || !slices.Contains(currentPage.Versions, oldPage.Published) {
		t.Fatal("the changed object page lost its original publication bytes")
	}
	for _, raw := range oldPage.Versions {
		if !slices.Contains(currentPage.Versions, raw) {
			t.Fatal("the changed object page rewrote a historical publication")
		}
	}
	live.accept(t, "build.alpha.create", object.Name, "A1", map[string]any{"note": "saved under the object view release"})
	want := live.recordImage(t)
	if len(want[object.Name]) == 0 {
		t.Fatal("the joint release did not accept a business object record")
	}
	activeBytes := bytes.Clone(live.releases.Raw(activeID))
	var liveInfo platform.EntityInfo
	for _, info := range live.Entities(live.member(t)) {
		if info.Type == object.Name {
			liveInfo = info
		}
	}
	if !reflect.DeepEqual(liveInfo.ObjectView, wantView) {
		t.Fatalf("live entity object view: %+v; want %+v", liveInfo.ObjectView, wantView)
	}
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs from the live record store in %v", replay, differing)
		}
		if restored.ActiveRelease() != activeID || !bytes.Equal(oldBytes, restored.releases.Raw(oldID)) || !bytes.Equal(activeBytes, restored.releases.Raw(activeID)) {
			t.Fatalf("replay %d changed a saved candidate or active release", replay)
		}
		page := pagePublication(restored)
		if page.Published != currentPage.Published || !reflect.DeepEqual(page.Versions, currentPage.Versions) {
			t.Fatalf("replay %d changed the page publication or history bytes", replay)
		}
		var restoredInfo platform.EntityInfo
		for _, info := range restored.Entities(restored.member(t)) {
			if info.Type == object.Name {
				restoredInfo = info
			}
		}
		if !reflect.DeepEqual(restoredInfo.ObjectView, liveInfo.ObjectView) {
			t.Fatalf("replay %d changed EntityInfo.ObjectView: %+v", replay, restoredInfo.ObjectView)
		}
	}
}

func TestInvariantK2SameKeyReturnsSameAnswer(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		tn := newInvariantTenant(t, "invariant-k2-accepted")
		request := tn.submission("build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
		again := proto.Clone(request).(*pb.Submission)
		first, refusal := tn.Submit(tn.member(t), request, invariantClock)
		if refusal != nil {
			t.Fatalf("first submission: %s: %s", refusal.Code, refusal.Message)
		}
		if len(tn.entries) != 1 {
			t.Fatalf("first submission sealed %d results; want 1", len(tn.entries))
		}
		second, refusal := tn.Submit(tn.member(t), again, invariantClock.Add(time.Hour))
		if refusal != nil {
			t.Fatalf("repeated submission: %s: %s", refusal.Code, refusal.Message)
		}
		if !proto.Equal(first, second) {
			t.Fatalf("repeated submission answered differently:\nfirst:  %v\nsecond: %v", first, second)
		}
		if len(tn.entries) != 1 {
			t.Fatalf("the journal holds %d accepted results after the repeat; want 1", len(tn.entries))
		}
	})
	t.Run("refused", func(t *testing.T) {
		tn := newInvariantTenant(t, "invariant-k2-refused")
		tn.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
		request := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, stale"))
		zero := uint32(0)
		request.ExpectedRevision = &zero
		again := proto.Clone(request).(*pb.Submission)
		_, first := tn.Submit(tn.member(t), request, invariantClock)
		if first == nil {
			t.Fatal("the stale submission was accepted")
		}
		if len(tn.entries) != 2 {
			t.Fatalf("the journal holds %d results after the refusal; want 2", len(tn.entries))
		}
		_, second := tn.Submit(tn.member(t), again, invariantClock.Add(time.Hour))
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("repeated refusal answered differently:\nfirst:  %+v\nsecond: %+v", first, second)
		}
		if len(tn.entries) != 2 {
			t.Fatalf("the journal holds %d results after the repeat; want 2", len(tn.entries))
		}
	})
}

func TestInvariantK3StaleRevisionIsRefusedAndSealed(t *testing.T) {
	tn := newInvariantTenant(t, "invariant-k3")
	tn.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	tn.accept(t, "build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, revised"))
	sealedBefore := len(tn.entries)
	imageBefore := tn.recordImage(t)
	stale := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, stale"))
	read := uint32(1)
	stale.ExpectedRevision = &read
	record, refusal := tn.Submit(tn.member(t), stale, invariantClock)
	if record != nil || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("stale expected revision: record=%v refusal=%v; want a CONFLICT refusal", record, refusal)
	}
	if len(tn.entries) != sealedBefore+1 {
		t.Fatalf("the refusal sealed %d journal entries; want 1", len(tn.entries)-sealedBefore)
	}
	saved, sealed := tn.sealedRefusal(t, sealedBefore)
	if saved.Error.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || sealed.GetIdempotencyKey() != stale.GetIdempotencyKey() || sealed.GetExpectedRevision() != read {
		t.Fatalf("sealed refusal does not carry the refused submission: code=%s key=%q expected=%d", saved.Error.Code, sealed.GetIdempotencyKey(), sealed.GetExpectedRevision())
	}
	if differing := differingTypes(imageBefore, tn.recordImage(t)); len(differing) != 0 {
		t.Fatalf("the refused submission changed the record store in %v", differing)
	}
	current := uint32(2)
	fresh := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, current"))
	fresh.ExpectedRevision = &current
	accepted, refusal := tn.Submit(tn.member(t), fresh, invariantClock)
	if refusal != nil || accepted.GetRevision() != 3 {
		t.Fatalf("the current expected revision: record=%v refusal=%v; want revision 3", accepted, refusal)
	}
}

func TestInvariantK4RefusalsCarryReasonAndMarkingsGate(t *testing.T) {
	tn := newInvariantTenant(t, "invariant-k4",
		platform.Seat{Subjects: []string{"cleared-builder"}, Member: platform.Member{ID: "cleared-builder", Roles: map[string]string{build.ID: build.Builder, "platform": "confidential"}}},
		platform.Seat{Subjects: []string{"unprivileged"}, Member: platform.Member{ID: "unprivileged"}},
	)
	uncleared := tn.member(t)
	cleared, ok := tn.Member("cleared-builder")
	if !ok {
		t.Fatal("the cleared builder is not seated")
	}
	unprivileged, ok := tn.Member("unprivileged")
	if !ok {
		t.Fatal("the unprivileged member is not seated")
	}
	if !uncleared.Holds(build.ID, build.Builder) || !cleared.Holds(build.ID, build.Builder) || platform.MarkingCleared(uncleared, "confidential") || !platform.MarkingCleared(cleared, "confidential") {
		t.Fatal("the builders must have the same build role and different confidential clearances")
	}

	t.Run("write refusal is explained and sealed", func(t *testing.T) {
		sealedBefore := len(tn.entries)
		imageBefore := tn.recordImage(t)
		request := tn.submission("build.object.create", build.ObjectType, "DENIED", invariantObject("denied", "Denied"))
		request.PrincipalId = unprivileged.ID
		record, refusal := tn.Submit(unprivileged, request, invariantClock)
		want := "Authorization denied at verb: No role or policy grants this operation"
		if record != nil || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Message != want {
			t.Fatalf("unauthorized write: record=%v refusal=%+v; want POLICY_DENIED with %q", record, refusal, want)
		}
		if len(tn.entries) != sealedBefore+1 {
			t.Fatalf("the write refusal sealed %d journal entries; want 1", len(tn.entries)-sealedBefore)
		}
		saved, sealed := tn.sealedRefusal(t, sealedBefore)
		if !reflect.DeepEqual(saved.Error, *refusal) || !proto.Equal(sealed, request) {
			t.Fatalf("the sealed refusal changed its reason or submission: error=%+v submission=%v", saved.Error, sealed)
		}
		if differing := differingTypes(imageBefore, tn.recordImage(t)); len(differing) != 0 {
			t.Fatalf("the refused write changed the record store in %v", differing)
		}
	})

	for _, dataset := range []struct{ id, name, marking string }{
		{"OPEN", "open", "internal"}, {"GUARDED", "guarded", "confidential"},
	} {
		tn.accept(t, "build.dataset.create", build.DatasetType, dataset.id, map[string]any{"name": dataset.name, "title": dataset.name})
		tn.accept(t, build.SchemaDatasetLoad, build.DatasetType, dataset.id, map[string]any{
			"rows": []map[string]any{{"note": dataset.name}}, "marking": dataset.marking,
		})
	}
	openID, guardedID := build.VersionID("OPEN", 1), build.VersionID("GUARDED", 1)

	t.Run("read refusal carries its reason", func(t *testing.T) {
		want := "Authorization denied at verb: No role or policy grants this operation"
		page, refusal := tn.Records(unprivileged, build.DatasetVersionType, platform.Query{}, invariantClock)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Message != want || len(page.Records) != 0 {
			t.Fatalf("unauthorized collection read: page=%+v refusal=%+v; want POLICY_DENIED with %q", page, refusal, want)
		}
		view, refusal := tn.RecordOf(unprivileged, build.DatasetVersionType, openID, invariantClock)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Message != want || view.Record != nil {
			t.Fatalf("unauthorized single read: view=%+v refusal=%+v; want POLICY_DENIED with %q", view, refusal, want)
		}
	})

	t.Run("collections filter uncleared markings", func(t *testing.T) {
		for _, check := range []struct {
			member platform.Member
			want   []string
		}{{cleared, []string{guardedID, openID}}, {uncleared, []string{openID}}} {
			page, refusal := tn.Records(check.member, build.DatasetVersionType, platform.Query{Sort: []string{"id"}}, invariantClock)
			if refusal != nil {
				t.Fatalf("%s collection read: %+v", check.member.ID, refusal)
			}
			var ids []string
			for _, record := range page.Records {
				version, ok := record.(build.DatasetVersion)
				if !ok {
					t.Fatalf("dataset version read returned %T", record)
				}
				ids = append(ids, version.ID)
			}
			if !slices.Equal(ids, check.want) || page.Total != len(check.want) {
				t.Fatalf("%s sees ids=%v total=%d; want ids=%v total=%d", check.member.ID, ids, page.Total, check.want, len(check.want))
			}
		}
	})

	t.Run("single reads hide uncleared markings", func(t *testing.T) {
		for _, check := range []struct {
			member platform.Member
			id     string
		}{{cleared, guardedID}, {uncleared, openID}} {
			view, refusal := tn.RecordOf(check.member, build.DatasetVersionType, check.id, invariantClock)
			if refusal != nil || view.Record == nil {
				t.Fatalf("%s cannot read visible version %s: view=%+v refusal=%+v", check.member.ID, check.id, view, refusal)
			}
		}
		hidden, refusal := tn.RecordOf(uncleared, build.DatasetVersionType, guardedID, invariantClock)
		missing, absent := tn.RecordOf(uncleared, build.DatasetVersionType, "MISSING@1", invariantClock)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND || hidden.Record != nil || !reflect.DeepEqual(refusal, absent) || !reflect.DeepEqual(hidden, missing) {
			t.Fatalf("uncleared read differs from a missing version: hidden=%+v refusal=%+v missing=%+v refusal=%+v", hidden, refusal, missing, absent)
		}
	})
	t.Run("discovery exposes resource identity without granting definition access", func(t *testing.T) {
		sub := tn.submission(platform.SchemaProjectSave, platform.ProjectType, build.WorkspaceProject, map[string]any{
			"title": "Workspace", "members": []platform.ProjectMember{{Member: uncleared.ID, Role: platform.ProjectOwnerRole}, {Member: unprivileged.ID, Role: platform.ProjectDiscovererRole}},
		})
		sub.Authority = "platform"
		if _, refusal := tn.Submit(uncleared, sub, invariantClock); refusal != nil {
			t.Fatal(refusal)
		}
		for _, reader := range []platform.Member{uncleared, unprivileged} {
			hits := tn.Search(&reader, "guarded", invariantClock)
			if len(hits) != 1 || hits[0].Resource == nil {
				t.Fatalf("%s resource discovery: %+v", reader.ID, hits)
			}
			resource := hits[0].Resource
			if resource.ID != platform.ResourceID(tn.ID, platform.AssetRef{App: build.ID, Kind: platform.AssetDataset, Name: "guarded"}) || resource.Project != build.WorkspaceProject || resource.CanOpen != (reader.ID == uncleared.ID) {
				t.Fatalf("%s discovered wrong identity or permission: %+v", reader.ID, resource)
			}
		}
		if _, refusal := tn.RecordOf(unprivileged, build.DatasetType, "GUARDED", invariantClock); refusal == nil {
			t.Fatal("discovery granted access to a resource definition")
		}
	})

}

func TestInvariantK5MarkingsPropagateThroughSourcesAndPipelines(t *testing.T) {
	clearance := platform.Seat{Subjects: []string{"inspector"}, Member: platform.Member{ID: "inspector", Roles: map[string]string{build.ID: build.Builder, "platform": "restricted"}}}
	live := newInvariantTenant(t, "invariant-k5", clearance)
	inspector, ok := live.Member("inspector")
	if !ok {
		t.Fatal("the lineage inspector is not seated")
	}
	for _, dataset := range []struct{ id, name string }{{"INPUT", "input"}, {"OTHER", "other"}, {"OUTPUT", "output"}} {
		live.accept(t, "build.dataset.create", build.DatasetType, dataset.id, map[string]any{"name": dataset.name, "title": dataset.name})
		live.accept(t, build.SchemaDatasetLoad, build.DatasetType, dataset.id, map[string]any{
			"rows": []map[string]any{{"id": "A1", "note": dataset.name}}, "marking": "internal",
		})
	}
	live.accept(t, "build.connection.create", build.ConnectionType, "CONNECTION", map[string]any{
		"name": "upstream", "title": "Upstream", "kind": "http", "address": "https://example.test", "marking": "confidential",
	})
	live.accept(t, "build.source.create", build.SourceType, "SOURCE", map[string]any{
		"name": "feed", "title": "Feed", "connection": "CONNECTION", "dataset": "INPUT",
	})
	live.accept(t, "build.pipeline.create", build.PipelineType, "PIPELINE", map[string]any{
		"name": "combine", "title": "Combine", "input": "INPUT", "outputDataset": "OUTPUT",
		"steps": []platform.TransformStep{{Kind: "join", Dataset: "OTHER", Column: "id", Match: "id"}},
	})
	object := invariantObject("alpha", "Alpha")
	object["datasources"] = []platform.DataSourceBinding{{Kind: "dataset", Source: "OUTPUT", Key: "id", Columns: []platform.DataSourceColumn{{Column: "note", Attribute: "note"}}}}
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", object)
	for _, phase := range []struct{ other, downstream string }{{"internal", "confidential"}, {"restricted", "restricted"}} {
		t.Run(phase.downstream, func(t *testing.T) {
			if phase.other == "restricted" {
				live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "OTHER", map[string]any{
					"rows": []map[string]any{{"id": "A1", "note": "higher clearance"}}, "marking": phase.other,
				})
			}
			graph, refusal := live.lineage(inspector)
			if refusal != nil {
				t.Fatalf("read lineage: %+v", refusal)
			}
			nodes := map[string]LineageNode{}
			for _, node := range graph.Nodes {
				nodes[node.ID] = node
			}
			for id, want := range map[string]string{
				"connection:CONNECTION": "confidential", "source:SOURCE": "confidential", "dataset:INPUT": "confidential",
				"dataset:OTHER": phase.other, "pipeline:PIPELINE": phase.downstream, "dataset:OUTPUT": phase.downstream, "object:build.alpha": phase.downstream,
			} {
				if node, ok := nodes[id]; !ok || node.Effective != want {
					t.Fatalf("lineage node %s: %+v; want effective marking %s", id, node, want)
				}
			}
			for _, id := range []string{"dataset:INPUT", "dataset:OUTPUT"} {
				if nodes[id].Marking != "internal" {
					t.Fatalf("%s has declared marking %q; want internal so propagation is required", id, nodes[id].Marking)
				}
			}
			if nodes["object:build.alpha"].Marking != "" || nodes["pipeline:PIPELINE"].Marking != "" {
				t.Fatal("the object and pipeline must inherit their effective markings without declaring one")
			}
			index := live.markingsIn(live.records, false)
			if got := index.types["build.alpha"]; got != phase.downstream {
				t.Fatalf("object marking index: %q; want %q", got, phase.downstream)
			}
			for id, want := range map[string]string{"INPUT": "confidential", "OTHER": phase.other, "OUTPUT": phase.downstream} {
				if got := index.datasets[id]; got != want {
					t.Fatalf("dataset %s marking index: %q; want %q", id, got, want)
				}
				row := live.records.Type(build.DatasetVersionType).Rows().Get(build.VersionID(id, 1))
				if row == nil {
					t.Fatalf("dataset %s has no first version", id)
				}
				if got := index.row(build.DatasetVersionType, row); got != want {
					t.Fatalf("dataset version %s marking: %q; want %q", build.VersionID(id, 1), got, want)
				}
			}
		})
	}
}

func TestInvariantK6BranchSavesIsolateMainAndMergeAsOneResult(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-k6", nil, platform.Seat{Subjects: []string{"reviewer"}, Member: platform.Member{ID: "reviewer", Roles: map[string]string{build.ID: build.Builder}}})
	member := live.member(t)
	objects := []struct{ id, name, title string }{{"OBJ1", "alpha", "Alpha"}, {"OBJ2", "beta", "Beta"}}
	for _, object := range objects {
		live.accept(t, "build.object.create", build.ObjectType, object.id, invariantObject(object.name, object.title))
	}
	mainBefore := bytes.Clone(live.recordImage(t)[build.ObjectType])
	if len(mainBefore) == 0 {
		t.Fatal("main has no object drafts")
	}
	branch, refusal := live.CreateBranch(member, BranchCreateRequest{Name: "k6", Title: "K6 branch"}, invariantClock)
	if refusal != nil {
		t.Fatalf("create branch: %v", refusal)
	}
	for _, object := range objects {
		saved, refusal := live.SaveBranchResource(member, branch.ID, build.ObjectType, object.id, BranchSaveRequest{
			Spec: platform.Raw(invariantObject(object.name, object.title+", branched")),
		}, invariantClock)
		if refusal != nil {
			t.Fatalf("save %s into branch: %v", object.id, refusal)
		}
		if !slices.ContainsFunc(saved.Resources, func(resource BranchResourceStatus) bool {
			return resource.Type == build.ObjectType && resource.ID == object.id && resource.Op == "update"
		}) {
			t.Fatalf("branch save omitted %s: %+v", object.id, saved.Resources)
		}
		if !bytes.Equal(mainBefore, live.recordImage(t)[build.ObjectType]) {
			t.Fatalf("saving %s into the branch changed main's object drafts", object.id)
		}
	}
	if _, refusal := live.ProposeBranch(member, branch.ID, BranchProposeRequest{Reviewers: []string{"reviewer"}}, invariantClock); refusal != nil {
		t.Fatalf("propose branch: %v", refusal)
	}
	if !bytes.Equal(mainBefore, live.recordImage(t)[build.ObjectType]) {
		t.Fatal("proposing the branch changed main's object drafts before merge")
	}
	reviewer, _ := live.Member("reviewer")
	if _, refusal := live.ReviewBranch(reviewer, branch.ID, BranchReviewRequest{Decision: "approve"}, invariantClock); refusal != nil {
		t.Fatalf("review branch: %v", refusal)
	}
	sealedBefore := len(live.entries)
	merged, refusal := live.MergeBranch(member, branch.ID, BranchMergeRequest{}, invariantClock)
	if refusal != nil || merged.Branch.State != build.BranchMerged || merged.Candidate == "" || merged.CandidateError != "" {
		t.Fatalf("merge branch: result=%+v refusal=%v", merged, refusal)
	}
	mergeResults, candidateResults := 0, 0
	for _, entry := range live.entries[sealedBefore:] {
		if entry.Kind != "accepted-result" {
			t.Fatalf("merge journal entry has kind %q; want accepted-result", entry.Kind)
		}
		var envelope struct{ Kind string }
		if err := json.Unmarshal(entry.Body, &envelope); err != nil {
			t.Fatal(err)
		}
		switch envelope.Kind {
		case "record-batch":
			batch, receipt, err := acceptedformat.DecodeBatch(entry.Body)
			if err != nil {
				t.Fatalf("decode merge result: %v", err)
			}
			submission := receipt.GetSubmission()
			if submission.GetSchema().GetName() != build.BranchType+".merge" || submission.GetTarget().GetType() != build.BranchType || submission.GetTarget().GetId() != branch.ID {
				t.Fatalf("merge emitted a separate resource result: %v", submission)
			}
			mergeResults++
			children := map[string]int{}
			for _, decision := range batch.Decisions {
				child := new(pb.ChangeRecord)
				if err := protojson.Unmarshal(decision.Receipt, child); err != nil {
					t.Fatal(err)
				}
				if child.GetSubmission().GetSchema().GetName() == "build.object.edit" {
					children[child.GetSubmission().GetTarget().GetId()]++
				}
			}
			if len(children) != len(objects) {
				t.Fatalf("the merge result contains object edits %v; want %d objects", children, len(objects))
			}
			for _, object := range objects {
				if children[object.id] != 1 || !slices.ContainsFunc(batch.Rows, func(row acceptedformat.BatchRow) bool {
					return row.Type == build.ObjectType && row.ID == object.id
				}) {
					t.Fatalf("the single merge result omits an edit or row for %s: children=%v", object.id, children)
				}
			}
		case "release-result":
			saved, err := acceptedformat.DecodeRelease(entry.Body)
			if err != nil || saved.CandidateID != merged.Candidate || saved.Key != "release:branch:"+branch.ID+":merge" || saved.Active {
				t.Fatalf("merge candidate save: result=%+v error=%v", saved, err)
			}
			candidateResults++
		default:
			t.Fatalf("merge emitted unexpected accepted result %q", envelope.Kind)
		}
	}
	if mergeResults != 1 || candidateResults != 1 {
		t.Fatalf("merge sealed %d merge results and %d candidate saves; want one of each", mergeResults, candidateResults)
	}
	for _, object := range objects {
		view, refusal := live.RecordOf(member, build.ObjectType, object.id, invariantClock)
		if refusal != nil {
			t.Fatalf("read merged object %s: %v", object.id, refusal)
		}
		current, ok := view.Record.(build.Object)
		if !ok || current.Title != object.title+", branched" || current.Revision != 2 {
			t.Fatalf("main object %s after merge: %+v; want the branch title at revision 2", object.id, view.Record)
		}
	}
}
