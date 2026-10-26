package platformserver

import "platformserver/internal/construction"

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestPackageBlueprintAgentQueryDraftsFormApplicationCandidate(t *testing.T) {
	at := txnAt
	source := builderTenant(t, "agent-blueprint-source", ai.New("agent-blueprint-source"), NewAgents("agent-blueprint-source"))
	decide(t, source, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "OBJECT", map[string]any{"name": "opportunity", "title": "Opportunity", "fields": []construction.Field{{Name: "title", Title: "Title", Type: "text"}}}, at)
	decide(t, source, "builder", construction.ID, construction.QueryType+".create", construction.QueryType, "QUERY", map[string]any{"name": "crmopenopportunities", "title": "Open opportunities", "description": "Read opportunities", "object": "build.opportunity", "limit": 20}, at)
	decide(t, source, "builder", construction.ID, build.AgentType+".create", build.AgentType, "AGENT", map[string]any{"name": "salesassistant", "title": "Sales assistant", "description": "Review opportunities", "instructions": "Read the opportunities query", "tools": []string{"query:crmopenopportunities"}}, at)
	root := platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "crmapp"}
	// Only the agent is explicitly held; its query and the query's source must
	// be discovered transitively, regardless of record IDs or publication state.
	decide(t, source, "builder", construction.ID, construction.AppType+".create", construction.AppType, "APP", map[string]any{"name": root.Name, "title": "CRM", "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetAgent, Name: "salesassistant"}}}, at)
	source.mu.Lock()
	preview, candidate, err := source.previewReleaseLocked([]construction.JointDraftRef{{Kind: platform.AssetAgent, ID: "AGENT"}, {Kind: platform.AssetApp, ID: "APP"}, {Kind: platform.AssetQuery, ID: "QUERY"}, {Kind: platform.AssetObject, ID: "OBJECT"}}, memberOf(t, source, "builder"))
	source.mu.Unlock()
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("source candidate: %+v %v", preview, err)
	}
	descriptor := PackageDescriptor{ID: "example.crm", Version: "1.0.0", Namespace: "example.crm", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{root}, Assets: candidate.Assets}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "crm.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	const targetID = "agent-blueprint-target"
	compose := func() *Tenant {
		tn := composeTenant(t, targetID, []Seat{seatOf("builder", "build:builder"), seatOf("admin", "platform:admin")}, build.New(targetID), ai.New(targetID), NewAgents(targetID))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	target := compose()
	var entries []Entry
	target.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, target, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	member := memberOf(t, target, "builder")
	created, err := target.CreatePackageDrafts(member, descriptor.ID, descriptor.Version, "create", at)
	if err != nil {
		t.Fatal(err)
	}
	before := len(entries)
	closure, err := target.ReferencedDrafts(member, platform.AssetApp, created.ApplicationID)
	if err != nil {
		t.Fatalf("package application dependency discovery: %v", err)
	}
	selection := append([]construction.JointDraftRef{{Kind: platform.AssetApp, ID: created.ApplicationID}}, closure.Drafts...)
	if len(selection) != 4 {
		t.Fatalf("incomplete application closure: %+v", selection)
	}
	for _, draft := range created.Drafts {
		if !slices.Contains(selection, construction.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID}) {
			t.Fatalf("package draft omitted: %+v", draft)
		}
	}
	preview, err = target.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("package application preview: %+v %v", preview, err)
	}
	if len(entries) != before || target.ActiveRelease() != "" {
		t.Fatal("discovery or preview wrote formal state")
	}
	query, _ := platform.Get[build.Query](target.automation(construction.ID, false), "crmopenopportunities")
	agent, _ := platform.Get[build.Agent](target.automation(construction.ID, false), "salesassistant")
	if query.Published != "" || agent.Published != "" || query.Version != 0 || agent.Version != 0 {
		t.Fatal("dependency discovery published query or agent")
	}
	if _, err := target.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, target, entries, compose)
	decide(t, target, "builder", construction.ID, construction.AppType+".archive", construction.AppType, created.ApplicationID, map[string]any{}, at)
	recreated, err := target.CreatePackageDrafts(member, descriptor.ID, descriptor.Version, "recreate", at)
	if err != nil || recreated.ApplicationID == created.ApplicationID {
		t.Fatalf("archived application did not get a new target identity: %+v %v", recreated, err)
	}
	for _, draft := range recreated.Drafts {
		if draft.Ref.Kind != platform.AssetApp && !slices.Contains(created.Drafts, draft) {
			t.Fatalf("recreating the application replaced a reusable draft: %+v", draft)
		}
	}
	original, err := target.CreatePackageDrafts(member, descriptor.ID, descriptor.Version, "create", at)
	if err != nil || original.ApplicationID != created.ApplicationID {
		t.Fatalf("retry revived or replaced the old creation: %+v %v", original, err)
	}
	// Unpublished tools can be discarded even though their query never went live.
	decide(t, target, "builder", construction.ID, build.AgentType+".archive", build.AgentType, "salesassistant", map[string]any{}, at)
	CheckReplay(t, target, entries, compose)
}
func TestPackageBlueprintExtendsInstalledCodeWithoutNativeDrafts(t *testing.T) {
	base := platform.AssetRef{App: core.ID, Kind: platform.AssetApp, Name: core.ID}
	page, err := platform.PageReleaseAsset(construction.ID, "1", platform.Page{Name: "materialdesk", Title: "Materials", Object: platform.AssetRef{App: core.ID, Kind: platform.AssetObject, Name: core.MaterialType}, Sections: []platform.Section{{ID: "table", Widget: "table", Fields: []string{"name"}}}})
	if err != nil {
		t.Fatal(err)
	}
	application, err := platform.ApplicationReleaseAsset(construction.ID, "1", platform.Application{Name: "materialworkspace", Title: "Material workspace", Pages: []string{"materialdesk"}, Extends: &base, Entry: core.ID,
		Replacements: []platform.ApplicationPageReplacement{{View: "materials", Page: "materialdesk"}}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := PackageDescriptor{ID: "example.material-workspace", Version: "1.0.0", Namespace: "example.material-workspace", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: []platform.ReleaseAsset{page, application}}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "materials.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "native-blueprint", []Seat{seatOf("admin", "platform:admin"), seatOf("builder", "build:builder", "core:steward")}, build.New("native-blueprint"), core.New("native-blueprint"))
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	original, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "native-drafts", txnAt)
	if err != nil || len(answer.Drafts) != 2 || answer.Application != "materialworkspace" {
		t.Fatalf("native dependency prevented ordinary Build drafts: %+v %v", answer, err)
	}
	for _, draft := range answer.Drafts {
		if draft.Ref.App != construction.ID || draft.Type != construction.AppType && draft.Type != construction.PageType {
			t.Fatalf("native prerequisite became a draft: %+v", draft)
		}
	}
	current, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil || string(original) != string(current) || tn.ActiveRelease() != "" {
		t.Fatal("draft creation changed the native owner or activated the application")
	}
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), []construction.JointDraftRef{{Kind: platform.AssetPage, ID: "materialdesk"}, {Kind: platform.AssetApp, ID: "materialworkspace"}})
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("native extension drafts do not form an ordinary candidate: %+v %v", preview, err)
	}
}
