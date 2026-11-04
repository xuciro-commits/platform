package host

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func invariantReadObject(t *testing.T, live *invariantTenant, id string) build.Object {
	t.Helper()
	view, refusal := live.RecordOf(live.member(t), build.ObjectType, id, invariantClock)
	if refusal != nil {
		t.Fatalf("read object %s: %v", id, refusal)
	}
	object, ok := view.Record.(build.Object)
	if !ok {
		t.Fatalf("object %s has type %T", id, view.Record)
	}
	return object
}

func invariantReadActionType(t *testing.T, live *invariantTenant, id string) build.ActionType {
	t.Helper()
	view, refusal := live.RecordOf(live.member(t), build.ActionTypeType, id, invariantClock)
	if refusal != nil {
		t.Fatalf("read action type %s: %v", id, refusal)
	}
	action, ok := view.Record.(build.ActionType)
	if !ok {
		t.Fatalf("action type %s has type %T", id, view.Record)
	}
	return action
}

func invariantCandidateAction(t *testing.T, live *invariantTenant, id string) (build.Action, string) {
	t.Helper()
	candidate, err := platform.ReadCandidate(id, live.releases.Raw(id))
	if err != nil {
		t.Fatalf("read release candidate: %v", err)
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.alpha.approve"}
	for _, asset := range candidate.Assets {
		if asset.Ref != ref {
			continue
		}
		var body struct {
			Action build.Action `json:"action"`
		}
		if err := json.Unmarshal(asset.Body, &body); err != nil {
			t.Fatalf("decode action asset: %v", err)
		}
		return body.Action, asset.SourceVersion
	}
	t.Fatalf("release candidate has no %s asset", ref)
	return build.Action{}, ""
}

func invariantReleaseAction(t *testing.T, live *invariantTenant, objectID, key string) (string, build.Action, string) {
	t.Helper()
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: objectID}}
	preview, err := live.PreviewReleaseDrafts(live.member(t), drafts, "", invariantClock)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("release preview: %+v: %v", preview, err)
	}
	id, err := live.SaveReleaseCandidates(live.member(t), drafts, preview.CandidateID, key, invariantClock)
	if err != nil || id != preview.CandidateID {
		t.Fatalf("save release candidate: id=%s: %v", id, err)
	}
	action, source := invariantCandidateAction(t, live, id)
	return id, action, source
}

func TestInvariantActionTypeSingleWriter(t *testing.T) {
	live := newInvariantTenant(t, "invariant-action-type-owner")
	object := invariantObject("alpha", "Alpha")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", object)
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", map[string]any{"states": []build.State{{Name: "open", Title: "Open"}}})
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "ACTION1", map[string]any{
		"name": "build.alpha.approve", "object": "build.alpha",
		"definition": map[string]any{"name": "approve", "title": "Approve", "from": []string{"open"}},
	})

	current := invariantReadObject(t, live, "OBJ1")
	if len(current.ActionTypes) != 1 || current.ActionTypes[0].Ref.Name != "build.alpha.approve" {
		t.Fatalf("object action references: %+v", current.ActionTypes)
	}
	action := invariantReadActionType(t, live, "ACTION1")
	if action.Version != 1 || len(action.Versions) != 1 {
		t.Fatalf("new action type version: %+v", action)
	}

	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", map[string]any{"title": "Alpha, revised"})
	if next := invariantReadActionType(t, live, "ACTION1"); next.Version != action.Version {
		t.Fatalf("object save changed the action version from %d to %d", action.Version, next.Version)
	}
	changed := []build.Action{{Name: "approve", Title: "Approve through the object", From: []string{"open"}}}
	if _, refusal := live.Submit(live.member(t), live.submission("build.object.edit", build.ObjectType, "OBJ1", map[string]any{"actions": changed}), invariantClock); refusal == nil {
		t.Fatal("an object save carrying changed actions was accepted")
	}

	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "ACTION1", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	current = invariantReadObject(t, live, "OBJ1")
	published, ok := build.ReadPublication[build.Object](current.Published)
	if !ok {
		t.Fatal("published object has no retained image")
	}
	if len(published.ActionTypes) != 1 || published.ActionTypes[0].SourceVersion != "1" || len(published.Actions) != 1 || published.Actions[0].Title != "Approve" {
		t.Fatalf("published object did not pin action version 1: %+v", published)
	}
	pinned := published.Actions[0]
	revision, publishedBytes := current.Revision, current.Published
	candidate, released, source := invariantReleaseAction(t, live, "OBJ1", "action-owner-v1")
	if released.Title != "Approve" || released.Title != pinned.Title || !strings.HasSuffix(source, ".action-1") {
		t.Fatalf("release did not keep the published action version: source=%s action=%+v", source, released)
	}

	live.accept(t, "build.actiontype.edit", build.ActionTypeType, "ACTION1", map[string]any{
		"name": "build.alpha.approve", "object": "build.alpha",
		"definition": map[string]any{"name": "approve", "title": "Approve, revised", "from": []string{"open"}},
	})
	edited := invariantReadActionType(t, live, "ACTION1")
	if edited.Version != 2 || len(edited.Versions) != 2 || edited.Definition.Title != "Approve, revised" {
		t.Fatalf("action edit version: %+v", edited)
	}
	if after := invariantReadObject(t, live, "OBJ1"); after.Revision != revision || after.Published != publishedBytes {
		t.Fatalf("action edit changed the object record or its publication: revision %d to %d", revision, after.Revision)
	}
	if kept := invariantReadActionType(t, live, "ACTION1"); kept.Versions[0].Definition.Title != "Approve" {
		t.Fatalf("action edit rewrote version 1: %+v", kept.Versions[0])
	}

	kept, keptSource := invariantCandidateAction(t, live, candidate)
	if kept.Title != "Approve" || !strings.HasSuffix(keptSource, ".action-1") {
		t.Fatalf("action edit rewrote a saved release closure: source=%s action=%+v", keptSource, kept)
	}

	_, released, source = invariantReleaseAction(t, live, "OBJ1", "action-owner-v2")
	if released.Title != "Approve, revised" || !strings.HasSuffix(source, ".action-2") {
		t.Fatalf("new release did not pin action version 2: source=%s action=%+v", source, released)
	}
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	current = invariantReadObject(t, live, "OBJ1")
	republished, ok := build.ReadPublication[build.Object](current.Published)
	if !ok {
		t.Fatal("republished object has no retained image")
	}
	if len(republished.ActionTypes) != 1 || republished.ActionTypes[0].SourceVersion != "2" || len(republished.Actions) != 1 || republished.Actions[0].Title != "Approve, revised" {
		t.Fatalf("republished object did not pin action version 2: %+v", republished)
	}
}

func TestInvariantActionTypeResourceSaveEditsExistingResource(t *testing.T) {
	live := newInvariantTenant(t, "invariant-action-type-save")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	created := live.accept(t, "build.actiontype.create", build.ActionTypeType, "ACTION1", map[string]any{
		"name": "build.alpha.approve", "object": "build.alpha",
		"definition": map[string]any{"name": "approve", "title": "Approve", "from": []string{"open"}},
	})
	if stored := invariantReadActionType(t, live, "ACTION1").Record.Revision; stored != created.GetRevision() {
		t.Fatalf("created actiontype record revision=%d stored revision=%d", created.GetRevision(), stored)
	}

	save := live.submission("build.actiontype.save", build.ActionTypeType, "ACTION1", map[string]any{
		"spec": map[string]any{
			"name": "build.alpha.approve", "object": "build.alpha",
			"definition": map[string]any{"name": "approve", "title": "Approve, revised", "from": []string{"open"}},
		},
		"project": invariantProject,
	})
	revision := invariantReadActionType(t, live, "ACTION1").Record.Revision
	save.ExpectedRevision = &revision
	if record, refusal := live.Submit(live.member(t), save, invariantClock); refusal != nil {
		t.Fatalf("resource save with expected revision 0: %s: %s", refusal.Code, refusal.Message)
	} else if record.GetRevision() == 0 {
		t.Fatal("resource save recorded revision 0")
	}

	action := invariantReadActionType(t, live, "ACTION1")
	if action.Version != 2 || action.Definition.Title != "Approve, revised" {
		t.Fatalf("resource save did not edit the action type: %+v", action)
	}
}
