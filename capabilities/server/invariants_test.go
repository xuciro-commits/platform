package platformserver

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/studio/build"
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
