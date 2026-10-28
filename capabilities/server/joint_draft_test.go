package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"bytes"
	"net/http"
	"net/http/httptest"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
	"sync/atomic"
)

// M3 (/D2): a new object, a new page over it and an
// application holding that page are composed and delivered as one joint
// candidate, without publishing any dependency first. The same call proves the
// selection installs before it can be saved, and refuses a selection whose
// dependency is neither installed nor part of it.
func TestApplicationDraftClosureIncludesUnreadyIntegration(t *testing.T) {
	at := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	const tenant = "draft-closure"
	tn := builderTenant(t, tenant, work.New(tenant))
	member := memberOf(t, tn, "builder")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "OBJECT", map[string]any{"name": "item", "title": "Item", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.PageType+".create", construction.PageType, "PAGE", map[string]any{"name": "items", "title": "Items", "object": "build.item", "list": []string{"note"}, "detail": []string{"note"}}, at)
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".create", construction.ConnectionType, "CONNECTION", map[string]any{"name": "upstream", "title": "Upstream", "kind": "http", "address": server.URL + "/", "allowPrivate": true}, at)
	decide(t, tn, "builder", construction.ID, construction.SourceType+".create", construction.SourceType, "SOURCE", map[string]any{"name": "feed", "title": "Feed", "connection": "CONNECTION", "url": "items", "object": "build.item", "key": "id", "mapping": []construction.SourceField{{From: "note", To: "note"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": "items", "title": "Items", "pages": []string{"items"}, "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetSource, Name: "feed"}}}, at)
	before := platform.Raw(tn.Definitions(member))
	closure, err := tn.ReferencedDrafts(member, platform.AssetApp, "APP")
	if err != nil || len(closure.Drafts) != 4 {
		t.Fatalf("unready connection prevented dependency discovery: %+v %v", closure, err)
	}
	for _, draft := range []construction.JointDraftRef{{Kind: platform.AssetPage, ID: "PAGE"}, {Kind: platform.AssetObject, ID: "OBJECT"}, {Kind: platform.AssetSource, ID: "SOURCE"}, {Kind: platform.AssetConnection, ID: "CONNECTION"}} {
		if !slices.Contains(closure.Drafts, draft) {
			t.Fatalf("closure omitted %v: %+v", draft, closure.Drafts)
		}
	}
	selection := append([]construction.JointDraftRef{{Kind: platform.AssetApp, ID: "APP"}}, closure.Drafts...)
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" || len(preview.Drafts) != 5 {
		t.Fatalf("an unready external connection blocked the complete logical candidate: %+v %v", preview, err)
	}
	logicalCandidate := preview.CandidateID
	if requests.Load() != 0 || !bytes.Equal(before, platform.Raw(tn.Definitions(member))) {
		t.Fatal("discovery or preview performed external I/O or changed installed definitions")
	}
	connection, _ := platform.Get[construction.Connection](tn.automation(construction.ID, false), "CONNECTION")
	if connection.State != "draft" || connection.Published != "" {
		t.Fatal("discovery published or checked the connection")
	}
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "CONNECTION", map[string]any{}, at)
	tn.CheckConnections(at)
	preview, err = tn.PreviewApplicationDrafts(member, "APP")
	if err != nil || preview.Diagnostic != "" || preview.CandidateID != logicalCandidate || len(preview.Drafts) != 5 {
		t.Fatalf("first application preview after checking connection: %+v %v", preview, err)
	}
}

// Automatic starts must retain the candidate reviewer, rather than requiring a
// separate direct publication before a first joint release can be previewed.
func TestJointDraftFirstScheduledFlowRetainsPublisher(t *testing.T) {
	const tenant = "joint-scheduled"
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, tenant, []Seat{seatOf("author", "build:builder"), seatOf("publisher", "build:builder")},
			work.New(tenant), flow.New(tenant), build.New(tenant))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	decide(t, tn, "author", construction.ID, construction.ProcessType+".create", construction.ProcessType, "FLOW", map[string]any{
		"name": "scheduled", "title": "Scheduled", "every": "1h", "steps": []construction.ProcessStep{{Name: "done", Kind: "end"}}}, at)
	member := memberOf(t, tn, "publisher")
	selection := []construction.JointDraftRef{{Kind: platform.AssetFlow, ID: "FLOW"}}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("first scheduled preview: %+v %v", preview, err)
	}
	if tn.procs.HasPublishedFlow("build.scheduled") {
		t.Fatal("preview installed the automatic flow")
	}
	saved, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	p, ok := platform.Get[construction.Process](tn.automation(construction.ID, false), "FLOW")
	var accepted construction.Process
	if !ok || json.Unmarshal([]byte(p.Published), &accepted) != nil || accepted.Version != 1 || accepted.Scheduler != member.ID {
		t.Fatalf("candidate lost its publisher: %+v", accepted)
	}
	CheckReplay(t, tn, entries, compose)
}

// the same owner-produced agent bytes travel with an application,
// while the original target owners compile/install its tools and evaluations.
