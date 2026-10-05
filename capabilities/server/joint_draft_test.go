package platformserver

import (
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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
		tn, err := NewTenant("joint-drafts", NewConsole("joint-drafts",
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User, flow.ID: "member"}}}),
			work.New("joint-drafts"), flow.New("joint-drafts"), build.New("joint-drafts"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member, _ := tn.Member("builder")
	keys := 0
	must := func(schema, typ, id string, payload any) {
		t.Helper()
		keys++
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprint(keys), Schema: &pb.SchemaRef{Name: schema, Version: 1},
			Target: &pb.EntityRef{Type: typ, Id: id}, Payload: platform.Raw(payload)}, at); err != nil {
			t.Fatalf("%s: %v: %s", schema, err, err.Message)
		}
	}

	// Three drafts, none of them published: the page names an object that is
	// only a draft, and the application holds that page.
	must(build.ObjectType+".create", build.ObjectType, "O", map[string]any{
		"name": "jointvisit", "title": "Joint visit", "plural": "Joint visits",
		"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	must(build.PageType+".create", build.PageType, "PAGE", map[string]any{
		"name": "jointvisits", "title": "Joint visits", "object": "build.jointvisit",
		"sections": []build.Section{{Widget: "table", Fields: []string{"note"}}}})
	must(build.AppType+".create", build.AppType, "APP", map[string]any{
		"name": "jointdesk", "title": "Joint desk", "pages": []string{"jointvisits"}})

	// The page alone still cannot be delivered: its object was never published,
	// so the review must say what is missing rather than save a candidate that
	// would not install (ADR-0048 D2).
	alone, aloneErr := tn.PreviewRelease(member, platform.AssetPage, "PAGE")
	if aloneErr == nil && alone.Diagnostic == "" && alone.CandidateID != "" {
		t.Fatalf("a page over an unpublished object was deliverable on its own: %+v", alone)
	}

	selection := []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"},
		{Kind: platform.AssetPage, ID: "PAGE"},
		{Kind: platform.AssetApp, ID: "APP"},
	}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("joint preview: %+v %v", preview, err)
	}
	kinds := map[platform.AssetKind]int{}
	for _, ref := range preview.Included {
		kinds[ref.Kind]++
	}
	if kinds[platform.AssetObject] == 0 || kinds[platform.AssetPage] == 0 || kinds[platform.AssetApp] == 0 {
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
	keys++
	saved, err := tn.SaveReleaseCandidates(member, []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"},
		{Kind: platform.AssetPage, ID: "PAGE"},
		{Kind: platform.AssetApp, ID: "APP"},
	}, preview.CandidateID, fmt.Sprint(keys), at)
	if err != nil || saved != preview.CandidateID {
		t.Fatalf("save joint candidate: %s %v", saved, err)
	}
	keys++
	if active, err := tn.ActivateRelease(member, saved, fmt.Sprint(keys), at); err != nil || active != saved {
		t.Fatalf("activate joint candidate: %s %v", active, err)
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
