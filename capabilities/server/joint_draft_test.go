package platformserver

import (
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// M3 (ADR-0047 §10.2, ADR-0048 D1/D2): a new object, a new page over it and an
// application holding that page are composed and delivered as one joint
// candidate, without publishing any dependency first. The same call proves the
// selection installs before it can be saved, and refuses a selection whose
// dependency is neither installed nor part of it.
func TestJointDraftsDeliverNewObjectPageAndApplication(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "joint-drafts", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:user", "flow:member")},
			work.New("joint-drafts"), flow.New("joint-drafts"), build.New("joint-drafts"))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member := memberOf(t, tn, "builder")

	// Four drafts, none published: the application holds both a page over the
	// draft object and a new native flow. Preview/save must not publish the flow.
	decide(t, tn, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "O", map[string]any{
		"name": "jointvisit", "title": "Joint visit", "plural": "Joint visits",
		"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", build.ID, build.PageType+".create", build.PageType, "PAGE", map[string]any{
		"name": "jointvisits", "title": "Joint visits", "object": "build.jointvisit",
		"sections": []build.Section{{Widget: "table", Fields: []string{"note"}}}}, at)
	decide(t, tn, "builder", build.ID, build.ProcessType+".create", build.ProcessType, "FLOW", map[string]any{
		"name": "jointarrival", "title": "Joint arrival", "manual": true, "steps": []build.ProcessStep{{Name: "done", Kind: "end"}}}, at)
	decide(t, tn, "builder", build.ID, build.AppType+".create", build.AppType, "APP", map[string]any{
		"name": "jointdesk", "title": "Joint desk", "pages": []string{"jointvisits"},
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetFlow, Name: "build.jointarrival"}}}, at)

	// The page alone still cannot be delivered: its object was never published,
	// so the review must say what is missing rather than save a candidate that
	// would not install (ADR-0048 D2).
	alone, aloneErr := tn.PreviewRelease(member, platform.AssetPage, "PAGE")
	if aloneErr == nil && alone.Diagnostic == "" && alone.CandidateID != "" {
		t.Fatalf("a page over an unpublished object was deliverable on its own: %+v", alone)
	}

	// The builder does not have to know the graph: the closure names the drafts
	// the application draft needs, deepest last, and never repeats an installed
	// dependency.
	closure, err := tn.ReferencedDrafts(member, platform.AssetApp, "APP")
	if err != nil || len(closure.Drafts) != 3 {
		t.Fatalf("application closure: %+v %v", closure, err)
	}
	for _, ref := range []build.JointDraftRef{{Kind: platform.AssetPage, ID: "PAGE"}, {Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetFlow, ID: "FLOW"}} {
		if !slices.Contains(closure.Drafts, ref) {
			t.Fatalf("application closure omitted %v: %+v", ref, closure.Drafts)
		}
	}

	selection := []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"},
		{Kind: platform.AssetPage, ID: "PAGE"},
		{Kind: platform.AssetFlow, ID: "FLOW"},
		{Kind: platform.AssetApp, ID: "APP"},
	}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("joint preview: %+v %v", preview, err)
	}
	if len(preview.Drafts) != 4 || preview.Drafts[0] != (build.JointDraftRef{Kind: platform.AssetObject, ID: "O"}) {
		t.Fatalf("joint review concealed its draft provenance: %+v", preview.Drafts)
	}
	kinds := map[platform.AssetKind]int{}
	for _, ref := range preview.Included {
		kinds[ref.Kind]++
	}
	if kinds[platform.AssetObject] == 0 || kinds[platform.AssetPage] == 0 || kinds[platform.AssetApp] == 0 || kinds[platform.AssetFlow] == 0 {
		t.Fatalf("joint candidate omitted a selected draft: %+v", preview.Included)
	}
	// Saving recomputes the same bytes; a selection whose dependency set shrinks
	// changes the candidate ID and cannot be saved under the previewed one.
	selection = selection[:2]
	narrower, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || narrower.CandidateID == preview.CandidateID {
		t.Fatalf("dropping a selected draft kept the candidate: %+v %v", narrower, err)
	}
	if _, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "stale", at); err == nil {
		t.Fatal("a stale joint selection was saved under the previewed candidate ID")
	}
	missingFlow, err := tn.PreviewReleaseDrafts(member, []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "PAGE"}, {Kind: platform.AssetApp, ID: "APP"},
	})
	if err == nil && !strings.Contains(missingFlow.Diagnostic, "build.jointarrival") {
		t.Fatalf("an omitted flow did not prevent delivery: %+v", missingFlow)
	}
	if tn.procs.HasPublishedFlow("build.jointarrival") {
		t.Fatal("candidate validation published the new flow in the live runtime")
	}
	saved, err := tn.SaveReleaseCandidates(member, []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"},
		{Kind: platform.AssetPage, ID: "PAGE"},
		{Kind: platform.AssetFlow, ID: "FLOW"},
		{Kind: platform.AssetApp, ID: "APP"},
	}, preview.CandidateID, "save-joint", at)
	if err != nil || saved != preview.CandidateID {
		t.Fatalf("save joint candidate: %s %v", saved, err)
	}
	if tn.procs.HasPublishedFlow("build.jointarrival") {
		t.Fatal("saving the candidate published the new flow")
	}
	if active, err := tn.ActivateRelease(member, saved, "activate-joint", at); err != nil || active != saved {
		t.Fatalf("activate joint candidate: %s %v", active, err)
	}
	if !tn.procs.HasPublishedFlow("build.jointarrival") {
		t.Fatal("activation did not publish the selected flow")
	}
	// The delivered drafts are what the operator now reads: the object is in
	// their definitions and the page is served under its application.
	reader := platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User, flow.ID: "member"}}
	sawObject, sawPage, sawApplication := false, false, false
	for _, definition := range tn.Definitions(reader) {
		switch {
		case definition.Ref.Kind == platform.AssetObject && definition.Ref.Name == "build.jointvisit":
			sawObject = true
		case definition.Ref.Kind == platform.AssetPage && definition.Ref.Name == "jointvisits":
			sawPage = true
		case definition.Ref.Kind == platform.AssetApp && definition.Ref.Name == "jointdesk":
			sawApplication = true
		}
	}
	if !sawObject || !sawPage || !sawApplication {
		t.Fatalf("joint delivery did not install its drafts: object=%v page=%v application=%v", sawObject, sawPage, sawApplication)
	}
	CheckReplay(t, tn, entries, compose)
}
