package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"platformserver/apps/ai"
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
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "O", map[string]any{
		"name": "jointvisit", "title": "Joint visit", "plural": "Joint visits",
		"states":  []construction.State{{Name: "open", Title: "Open"}, {Name: "closed", Title: "Closed"}},
		"actions": []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "closed", Roles: []string{construction.User}}},
		"fields":  []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.PageType+".create", construction.PageType, "PAGE", map[string]any{
		"name": "jointvisits", "title": "Joint visits", "object": "build.jointvisit",
		"list": []string{"note"}, "detail": []string{"note"}, "actions": []string{"build.jointvisit.create", "build.jointvisit.close"}}, at)
	decide(t, tn, "builder", construction.ID, construction.ProcessType+".create", construction.ProcessType, "FLOW", map[string]any{
		"name": "jointarrival", "title": "Joint arrival", "manual": true, "steps": []construction.ProcessStep{{Name: "done", Kind: "end"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{
		"name": "jointdesk", "title": "Joint desk", "pages": []string{"jointvisits"},
		"resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetFlow, Name: "build.jointarrival"}}}, at)

	// The page alone still cannot be delivered: its object was never published,
	// so the review must say what is missing rather than save a candidate that
	// would not install.
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
	for _, ref := range []construction.JointDraftRef{{Kind: platform.AssetPage, ID: "PAGE"}, {Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetFlow, ID: "FLOW"}} {
		if !slices.Contains(closure.Drafts, ref) {
			t.Fatalf("application closure omitted %v: %+v", ref, closure.Drafts)
		}
	}

	selection := []construction.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"},
		{Kind: platform.AssetPage, ID: "PAGE"},
		{Kind: platform.AssetFlow, ID: "FLOW"},
		{Kind: platform.AssetApp, ID: "APP"},
	}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("joint preview: %+v %v", preview, err)
	}
	if len(preview.Drafts) != 4 || preview.Drafts[0] != (construction.JointDraftRef{Kind: platform.AssetObject, ID: "O"}) {
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
	missingFlow, err := tn.PreviewReleaseDrafts(member, []construction.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "PAGE"}, {Kind: platform.AssetApp, ID: "APP"},
	})
	if err == nil && !strings.Contains(missingFlow.Diagnostic, "build.jointarrival") {
		t.Fatalf("an omitted flow did not prevent delivery: %+v", missingFlow)
	}
	if tn.procs.HasPublishedFlow("build.jointarrival") {
		t.Fatal("candidate validation published the new flow in the live runtime")
	}
	saved, err := tn.SaveReleaseCandidates(member, []construction.JointDraftRef{
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
	reader := platform.Member{ID: "operator", Roles: map[string]string{construction.ID: construction.User, flow.ID: "member"}}
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
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "O", map[string]any{"actions": []construction.Action{}}, at)
	removedAction, err := tn.PreviewReleaseDrafts(member, []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "PAGE"}})
	if err == nil && !strings.Contains(removedAction.Diagnostic, "build.jointvisit.close") {
		t.Fatalf("the page reused an installed action removed from its selected draft: %+v", removedAction)
	}
	CheckReplay(t, tn, entries, compose)
}

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
	if err != nil || !strings.Contains(preview.Diagnostic, "source/SOURCE") || !strings.Contains(preview.Diagnostic, "Connection CONNECTION is not ready") || len(preview.Drafts) != 5 {
		t.Fatalf("review did not locate the actual blocker in its complete selection: %+v %v", preview, err)
	}
	if requests.Load() != 0 || !bytes.Equal(before, platform.Raw(tn.Definitions(member))) {
		t.Fatal("discovery or refused preview performed external I/O or changed installed definitions")
	}
	connection, _ := platform.Get[construction.Connection](tn.automation(construction.ID, false), "CONNECTION")
	if connection.State != "draft" || connection.Published != "" {
		t.Fatal("discovery published or checked the connection")
	}
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "CONNECTION", map[string]any{}, at)
	tn.CheckConnections(at)
	preview, err = tn.PreviewApplicationDrafts(member, "APP")
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" || len(preview.Drafts) != 5 {
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
func TestApplicationArtifactAgentDeliveryAndRuntime(t *testing.T) {
	const tenant = "application-agents"
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	compose := func(id string) *Tenant {
		return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "agent:admin", "flow:member", "platform:admin", "ai:admin")}, work.New(id), flow.New(id), build.New(id), NewAgents(id), ai.New(id))
	}
	source := compose(tenant)
	var entries []Entry
	source.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	decide(t, source, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "O", map[string]any{"name": "case", "title": "Case", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, source, "builder", construction.ID, build.AgentType+".create", build.AgentType, "AG", map[string]any{"name": "assist", "title": "Assist", "description": "Prepare case updates", "instructions": "Prepare a note update", "tools": []string{"build.case.edit"}, "cases": []build.AgentCase{{Name: "note", Goal: "Prepare a note", Expect: "Ready"}}}, at)
	decide(t, source, "builder", construction.ID, construction.ProcessType+".create", construction.ProcessType, "FLOW", map[string]any{"name": "closecase", "title": "Close case", "manual": true, "steps": []construction.ProcessStep{{Name: "done", Kind: "end"}}}, at)
	resources := []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: "build.case"}, {App: construction.ID, Kind: platform.AssetAction, Name: "build.case.edit"}, {App: construction.ID, Kind: platform.AssetAgent, Name: "assist"}, {App: construction.ID, Kind: platform.AssetFlow, Name: "build.closecase"}}
	decide(t, source, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": "cases", "title": "Cases", "resources": resources, "profile": platform.ApplicationProfile{ExpectedRecords: map[string]int64{"build.case": 1}, ExpectedRuns: 1, Retention: map[string]string{"runs": "24h"}}}, at)
	member := memberOf(t, source, "builder")
	selection := []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetAgent, ID: "AG"}, {Kind: platform.AssetFlow, ID: "FLOW"}, {Kind: platform.AssetApp, ID: "APP"}}
	preview, err := source.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("agent/application preview: %+v %v", preview, err)
	}
	saved, err := source.SaveReleaseCandidates(member, selection, preview.CandidateID, "save-app", at)
	if err != nil {
		t.Fatal(err)
	}
	configureModel := func(tn *Tenant) {
		decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/probe"}, at)
		decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "wire": "openai"}, at)
		decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]any{"access": "users"}, at)
		tn.AIClient = func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"content":"Ready"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))}, nil
		}
	}
	configureModel(source)
	if _, err := source.EvaluateRelease(member, ReleaseEvaluationRequest{CandidateID: saved, Agent: "build.assist", Key: "agent"}, at); err != nil {
		t.Fatal(err)
	}
	source.Evaluate(at)
	if _, err := source.ActivateRelease(member, saved, "activate-app", at); err != nil {
		t.Fatal(err)
	}
	root := platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "cases"}
	runtime, refusal := source.ApplicationRuntimeOf(member, root, at)
	if refusal != nil || len(runtime.Artifact.AI) != 1 || runtime.Artifact.AI[0].Ref.Kind != platform.AssetAgent || len(runtime.Artifact.Flows) != 1 || len(runtime.Artifact.Roles) == 0 {
		t.Fatalf("complete application projection: %+v %v", runtime, refusal)
	}
	target := compose("application-target")
	target.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	if _, err := PromoteCandidate(source, target, saved, "promote-app", false, memberOf(t, target, "builder"), at); err != nil {
		t.Fatal(err)
	}
	configureModel(target)
	if _, err := target.EvaluateRelease(memberOf(t, target, "builder"), ReleaseEvaluationRequest{CandidateID: saved, Agent: "build.assist", Key: "agent"}, at); err != nil {
		t.Fatal(err)
	}
	target.Evaluate(at)
	if _, err := target.ActivateRelease(memberOf(t, target, "builder"), saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	installed, refusal := target.ApplicationRuntimeOf(memberOf(t, target, "builder"), root, at)
	if refusal != nil || installed.Artifact.Version != runtime.Artifact.Version {
		t.Fatalf("target changed immutable artifact: %+v %v", installed, refusal)
	}
	CheckReplay(t, source, entries, func() *Tenant { return compose(tenant) })
	decide(t, source, "builder", construction.ID, construction.SchemaProcessRun, construction.ProcessType, "FLOW", map[string]any{"key": "run-one"}, at)
	source.Work(at)
	runs, refusal := source.ApplicationRuns(member, root, 0, 50, at, ApplicationRunFilter{Kind: "flow", Since: at, Until: at})
	if refusal != nil || runs.Total != 1 || runs.Runs[0].ResourceName != "build.closecase" || len(runs.Runs[0].Applications) == 0 || runs.Runs[0].Applications[0].Ref != root {
		t.Fatalf("filtered original flow: %+v %v", runs, refusal)
	}
	filtered, refusal := source.ApplicationRuns(member, root, 0, 50, at, ApplicationRunFilter{Kind: "flow", Since: at.Add(time.Second)})
	if refusal != nil || filtered.Total != 0 {
		t.Fatalf("time filter: %+v %v", filtered, refusal)
	}
	// Recorded application membership survives removal and must not be assigned
	// to an unrelated application which begins sharing the flow later.
	decide(t, source, "builder", construction.ID, construction.AppType+".edit", construction.AppType, "APP", map[string]any{"resources": resources[:len(resources)-1]}, at.Add(time.Second))
	decide(t, source, "builder", construction.ID, construction.SchemaHandOver, construction.AppType, "APP", map[string]any{}, at.Add(time.Second))
	decide(t, source, "builder", construction.ID, construction.AppType+".create", construction.AppType, "OTHER", map[string]any{"name": "later", "title": "Later application", "resources": resources[len(resources)-1:]}, at.Add(time.Second))
	decide(t, source, "builder", construction.ID, construction.SchemaHandOver, construction.AppType, "OTHER", map[string]any{}, at.Add(time.Second))
	runs, refusal = source.ApplicationRuns(member, root, 0, 50, at.Add(time.Second), ApplicationRunFilter{Kind: "flow"})
	if refusal != nil || runs.Total != 1 || runs.Runs[0].Association != "application" || len(runs.Runs[0].Shared) != 0 {
		t.Fatalf("historical application membership changed: %+v %v", runs, refusal)
	}
	other, refusal := source.ApplicationRuns(member, platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "later"}, 0, 50, at.Add(time.Second), ApplicationRunFilter{Kind: "flow"})
	if refusal != nil || other.Total != 0 {
		t.Fatalf("later membership claimed an existing run: %+v %v", other, refusal)
	}
	CheckReplay(t, source, entries, func() *Tenant { return compose(tenant) })
	// Exercise the former 10,000-record read boundary without introducing a
	// timing assertion or pretending synthetic inventory is capacity proof.
	instance, ok := platform.Get[platform.FlowInstance](source.automation(flow.ID, false), runs.Runs[0].ID)
	if !ok {
		t.Fatal("original flow instance is unavailable")
	}
	beforeScale, refusal := source.ApplicationRuns(member, root, 0, 50, at.Add(time.Second))
	if refusal != nil || beforeScale.Total < 1 {
		t.Fatalf("original application run inventory is unavailable: %+v %v", beforeScale, refusal)
	}
	expectedRuns := beforeScale.Total + 10001
	source.records.mu.Lock()
	for i := 0; i < 10001; i++ {
		copy := instance
		copy.ID = fmt.Sprintf("inventory-%05d", i)
		value := reflect.New(reflect.TypeOf(copy)).Elem()
		value.Set(reflect.ValueOf(copy))
		source.records.types[platform.FlowInstanceType].setRow(copy.ID, &records.Row{Value: value})
	}
	source.records.mu.Unlock()
	scale, refusal := source.ApplicationRuntimeOf(member, root, at.Add(time.Second))
	if refusal != nil || scale.Runs != expectedRuns || !slices.ContainsFunc(scale.Warnings, func(warning ApplicationWarning) bool {
		return warning.Kind == "runs" && warning.Actual == int64(expectedRuns) && warning.Expected == 1
	}) {
		t.Fatalf("run inventory silently lost its scale warning: %+v %v", scale, refusal)
	}
}
