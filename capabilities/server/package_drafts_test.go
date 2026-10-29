package platformserver

import agentowner "platformserver/engine/agents"

import "platformserver/studio/build"

import (
	"os"
	"path/filepath"
	controlowner "platformserver/kernel/control"
	"platformserver/studio/packages"
	"slices"
	"strings"
	"testing"

	"platformserver/apps/ai"

	"platformserver/apps/core"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
)

func TestManufacturingApplicationDraftClosureActivates(t *testing.T) {
	const id = "manufacturing-application-release"
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "platform:admin", "core:steward"), seatOf("user")}, build.New(id), core.New(id), ai.New(id), automation.New(id), work.New(id), agentowner.New(id))
	index, err := packages.LoadPackageIndex("../../deploy/packages")
	if err != nil {
		t.Fatal(err)
	}
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "builder", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, "application.manufacturing", map[string]any{"id": "application.manufacturing", "version": "0.1.1"}, txnAt)
	member := memberOf(t, tn, "builder")
	if _, err := tn.CreatePackageDrafts(member, "application.manufacturing", "0.1.1", "drafts", txnAt); err != nil {
		t.Fatal(err)
	}
	closure, err := tn.ReferencedDrafts(member, platform.AssetApp, "mesapp")
	if err != nil {
		t.Fatal(err)
	}
	selection := append([]build.JointDraftRef{{Kind: platform.AssetApp, ID: "mesapp"}}, closure.Drafts...)
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %s %v", preview.Diagnostic, err)
	}
	if _, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", txnAt); err != nil {
		t.Fatal(err)
	}
	review, err := tn.ReviewSavedRelease(member, preview.CandidateID)
	if err != nil || !review.CanActivate {
		t.Fatalf("saved candidate: activation=%s running=%s %v", review.ActivationDiagnostic, review.RunningDiagnostic, err)
	}
	if review.RunningDiagnostic != "" || len(review.Preview.Added) == 0 {
		t.Fatalf("first activation must show additions, not a comparison failure: %s", review.RunningDiagnostic)
	}
	if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate", txnAt); err != nil {
		t.Fatal(err)
	}
	review, err = tn.ReviewSavedRelease(member, preview.CandidateID)
	if err != nil || !review.Active || !review.RunningMatches {
		t.Fatalf("activated candidate: active=%v matches=%v activation=%s running=%s %v", review.Active, review.RunningMatches, review.ActivationDiagnostic, review.RunningDiagnostic, err)
	}
	for _, app := range tn.Applications(memberOf(t, tn, "user")) {
		if app.Name == "mesapp" {
			t.Fatal("activation granted application access")
		}
	}
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaGrant, controlowner.MemberType, "user", map[string]any{"app": "build", "role": "supervisor"}, txnAt)
	apps := tn.Applications(memberOf(t, tn, "user"))
	if !slices.ContainsFunc(apps, func(app platform.ApplicationView) bool { return app.Name == "mesapp" && len(app.Pages) > 0 }) {
		t.Fatal("explicitly authorized user cannot discover the activated application")
	}
	decide(t, tn, "user", "build", "build.mesrouting.create", "build.mesrouting", "routing", map[string]any{"code": "ROUTE-1", "name": "Local routing", "version": 1, "from": "2026-10-26"}, txnAt)
	decide(t, tn, "user", "build", "build.mesrouting.edit", "build.mesrouting", "routing", map[string]any{"name": "Updated local routing"}, txnAt)
}

func TestInstalledManufacturingPackageCreatesAllDraftsWithoutPublishingOrGranting(t *testing.T) {
	const id = "package-declarations"
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "platform:admin", "core:steward")}, build.New(id), core.New(id), ai.New(id), agentowner.New(id))
	index, err := packages.LoadPackageIndex("../../deploy/packages")
	if err != nil {
		t.Fatal(err)
	}
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "builder", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, "application.manufacturing", map[string]any{"id": "application.manufacturing", "version": "0.1.1"}, txnAt)
	var accepted []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		accepted = append(accepted, entry)
		return entry.Body, nil
	}
	member := memberOf(t, tn, "builder")
	answer, err := tn.CreatePackageDrafts(member, "application.manufacturing", "0.1.1", "create-original", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Drafts) != 104 || tn.ActiveRelease() != "" || memberOf(t, tn, "builder").Holds("build", "accountant") {
		t.Fatalf("incomplete drafts or unexpected activation/grant: %d drafts, release %s", len(answer.Drafts), tn.ActiveRelease())
	}
	move, ok := platform.Get[build.Object](tn.automation(build.ID, false), "erpmove")
	if !ok || len(move.Constraints) != 12 || move.Published != "" {
		t.Fatal("the original constrained draft was lost or published")
	}
	before := len(accepted)
	retried, err := tn.CreatePackageDrafts(member, "application.manufacturing", "0.1.1", "create-original", txnAt)
	if err != nil || !slices.Equal(retried.Drafts, answer.Drafts) || len(accepted) != before {
		t.Fatalf("same-key retry changed the draft task: %v", err)
	}
}

func TestPackageBusinessRoleBecomesGrantableOnlyAfterDefinitionsActivate(t *testing.T) {
	const id = "package-role-order"
	object := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.roleentry"}
	application, err := platform.ApplicationReleaseAsset("build", "1", platform.Application{Name: "roledesk", Title: "Role desk", Pages: []string{"roleentry"}, Resources: []platform.AssetRef{object}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := platform.PackageDescriptor{ID: "example.role-order", Version: "1.0.0", Namespace: "example.role-order", Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: []platform.ReleaseAsset{
		{Ref: object, ContractVersion: 1, SourceVersion: "1", Body: platform.Raw(map[string]any{"type": object.Name, "name": "roleentry", "title": "Role entry", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}, "access": []build.Access{{Role: "accountant", Read: "all", Create: true}}})}, application,
	}}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "role-order.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := packages.LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New(id))
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "builder", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	member := memberOf(t, tn, "builder")
	check := func(available, held bool) {
		t.Helper()
		plan, refused := tn.PackagePreparation(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, txnAt)
		if refused != nil {
			t.Fatal(refused)
		}
		role := slices.IndexFunc(plan.Roles, func(role packages.PackageRoleRequirement) bool {
			return role.App == "build" && role.Role == "accountant"
		})
		if role < 0 || plan.Roles[role].Available != available || plan.Roles[role].Held != held {
			t.Fatalf("incorrect preparation role state: %+v", plan.Roles)
		}
	}
	check(false, false)
	if reason := refuse(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaGrant, controlowner.MemberType, "builder", map[string]any{"app": "build", "role": "accountant"}, txnAt); !strings.Contains(reason, "not declared") {
		t.Fatalf("undeclared role was not explained: %s", reason)
	}
	answer, err := tn.CreatePackageDrafts(member, descriptor.ID, descriptor.Version, "create", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	check(false, false)
	selection := make([]build.JointDraftRef, 0, len(answer.Drafts))
	for _, draft := range answer.Drafts {
		selection = append(selection, build.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID})
	}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if _, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", txnAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate", txnAt); err != nil {
		t.Fatal(err)
	}
	check(true, false)
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaGrant, controlowner.MemberType, "builder", map[string]any{"app": "build", "role": "accountant"}, txnAt)
	check(true, true)
	if member := memberOf(t, tn, "builder"); member.Roles["build"] != "builder" || !member.Holds("build", "accountant") {
		t.Fatal("adding business access removed the original builder task", member.Roles)
	}
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaRevoke, controlowner.MemberType, "builder", map[string]any{"app": "build", "role": "builder"}, txnAt)
	if member := memberOf(t, tn, "builder"); member.Roles["build"] != "accountant" || member.Holds("build", "builder") {
		t.Fatal("revoking the primary role retained it", member.Roles)
	}
}

func TestPackageBlueprintAgentQueryDraftsFormApplicationCandidate(t *testing.T) {
	at := txnAt
	source := builderTenant(t, "agent-blueprint-source", ai.New("agent-blueprint-source"), agentowner.New("agent-blueprint-source"))
	decide(t, source, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "OBJECT", map[string]any{"name": "opportunity", "title": "Opportunity", "fields": []build.Field{{Name: "title", Title: "Title", Type: "text"}}}, at)
	decide(t, source, "builder", build.ID, build.QueryType+".create", build.QueryType, "QUERY", map[string]any{"name": "crmopenopportunities", "title": "Open opportunities", "description": "Read opportunities", "object": "build.opportunity", "limit": 20}, at)
	decide(t, source, "builder", build.ID, build.AgentType+".create", build.AgentType, "AGENT", map[string]any{"name": "salesassistant", "title": "Sales assistant", "description": "Review opportunities", "instructions": "Read the opportunities query", "tools": []string{"query:crmopenopportunities"}}, at)
	root := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "crmapp"}
	// Only the agent is explicitly held; its query and the query's source must
	// be discovered transitively, regardless of record IDs or publication state.
	decide(t, source, "builder", build.ID, build.AppType+".create", build.AppType, "APP", map[string]any{"name": root.Name, "title": "CRM", "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetAgent, Name: "salesassistant"}}}, at)
	source.mu.Lock()
	preview, candidate, err := source.previewReleaseLocked([]build.JointDraftRef{{Kind: platform.AssetAgent, ID: "AGENT"}, {Kind: platform.AssetApp, ID: "APP"}, {Kind: platform.AssetQuery, ID: "QUERY"}, {Kind: platform.AssetObject, ID: "OBJECT"}}, memberOf(t, source, "builder"))
	source.mu.Unlock()
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("source candidate: %+v %v", preview, err)
	}
	descriptor := platform.PackageDescriptor{ID: "example.crm", Version: "1.0.0", Namespace: "example.crm", Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{root}, Assets: candidate.Assets}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "crm.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := packages.LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	const targetID = "agent-blueprint-target"
	compose := func() *Tenant {
		tn := composeTenant(t, targetID, []platform.Seat{seatOf("builder", "build:builder"), seatOf("admin", "platform:admin")}, build.New(targetID), ai.New(targetID), agentowner.New(targetID))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	target := compose()
	var entries []Entry
	target.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, target, "admin", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
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
	selection := append([]build.JointDraftRef{{Kind: platform.AssetApp, ID: created.ApplicationID}}, closure.Drafts...)
	if len(selection) != 4 {
		t.Fatalf("incomplete application closure: %+v", selection)
	}
	for _, draft := range created.Drafts {
		if !slices.Contains(selection, build.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID}) {
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
	query, _ := platform.Get[build.Query](target.automation(build.ID, false), "crmopenopportunities")
	agent, _ := platform.Get[build.Agent](target.automation(build.ID, false), "salesassistant")
	if query.Published != "" || agent.Published != "" || query.Version != 0 || agent.Version != 0 {
		t.Fatal("dependency discovery published query or agent")
	}
	if _, err := target.SaveReleaseCandidates(member, selection, preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, target, entries, compose)
	decide(t, target, "builder", build.ID, build.AppType+".archive", build.AppType, created.ApplicationID, map[string]any{}, at)
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
	decide(t, target, "builder", build.ID, build.AgentType+".archive", build.AgentType, "salesassistant", map[string]any{}, at)
	CheckReplay(t, target, entries, compose)
}
func TestPackageBlueprintExtendsInstalledCodeWithoutNativeDrafts(t *testing.T) {
	base := platform.AssetRef{App: core.ID, Kind: platform.AssetApp, Name: core.ID}
	page, err := platform.PageReleaseAsset(build.ID, "1", platform.Page{Name: "materialdesk", Title: "Materials", Object: platform.AssetRef{App: core.ID, Kind: platform.AssetObject, Name: core.MaterialType}, Sections: []platform.Section{{ID: "table", Widget: "table", Fields: []string{"name"}}}})
	if err != nil {
		t.Fatal(err)
	}
	application, err := platform.ApplicationReleaseAsset(build.ID, "1", platform.Application{Name: "materialworkspace", Title: "Material workspace", Pages: []string{"materialdesk"}, Extends: &base, Entry: core.ID,
		Replacements: []platform.ApplicationPageReplacement{{View: "materials", Page: "materialdesk"}}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := platform.PackageDescriptor{ID: "example.material-workspace", Version: "1.0.0", Namespace: "example.material-workspace", Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: []platform.ReleaseAsset{page, application}}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "materials.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := packages.LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "native-blueprint", []platform.Seat{seatOf("admin", "platform:admin"), seatOf("builder", "build:builder", "core:steward")}, build.New("native-blueprint"), core.New("native-blueprint"))
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "admin", controlowner.PlatformApp, packages.SchemaPackageInstall, platform.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	original, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "native-drafts", txnAt)
	if err != nil || len(answer.Drafts) != 2 || answer.Application != "materialworkspace" {
		t.Fatalf("native dependency prevented ordinary Build drafts: %+v %v", answer, err)
	}
	for _, draft := range answer.Drafts {
		if draft.Ref.App != build.ID || draft.Type != build.AppType && draft.Type != build.PageType {
			t.Fatalf("native prerequisite became a draft: %+v", draft)
		}
	}
	current, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil || string(original) != string(current) || tn.ActiveRelease() != "" {
		t.Fatal("draft creation changed the native owner or activated the application")
	}
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), []build.JointDraftRef{{Kind: platform.AssetPage, ID: "materialdesk"}, {Kind: platform.AssetApp, ID: "materialworkspace"}})
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("native extension drafts do not form an ordinary candidate: %+v %v", preview, err)
	}
}
