package host

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	automationauthoring "platformserver/services/automate/authoring"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK9FlowBindingsTreatEmptyAndAbsentActionsAlike(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k9-flow")
	owner, ok := live.App(build.ID).(interface {
		ProcessFromAsset(platform.ReleaseAsset) (protocol.Process, error)
	})
	if !ok {
		t.Fatal("the build app does not read flow release assets")
	}
	asset := func(p protocol.Process, actions any, keep bool) platform.ReleaseAsset {
		t.Helper()
		compiled, err := automationauthoring.FlowReleaseAsset(build.ID, p, "invariant")
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(compiled.Body, &body); err != nil {
			t.Fatal(err)
		}
		if keep {
			body["actions"] = actions
		} else {
			delete(body, "actions")
		}
		compiled.Body = platform.Raw(body)
		withRequires, err := PublicationResolver().WithDependencies(compiled)
		if err != nil {
			t.Fatal(err)
		}
		return withRequires
	}
	notify := protocol.Process{Name: "hello", Title: "Hello", Kind: "flow", Steps: []protocol.ProcessStep{{Name: "tell", Kind: "notify", Notify: &protocol.NotificationEffect{Title: "Hello"}}}}
	for _, variant := range []struct {
		name    string
		actions any
		keep    bool
	}{{"absent", nil, false}, {"null", nil, true}, {"empty", []any{}, true}} {
		if _, err := owner.ProcessFromAsset(asset(notify, variant.actions, variant.keep)); err != nil {
			t.Fatalf("a flow without action steps whose actions are %s must read back: %v", variant.name, err)
		}
	}
	extra := []any{map[string]any{"app": build.ID, "kind": "action", "name": "invariantproject.note.edit"}}
	if _, err := owner.ProcessFromAsset(asset(notify, extra, true)); err == nil {
		t.Fatal("an action the steps do not use must still be refused")
	}
	acting := protocol.Process{Name: "mark", Title: "Mark", Kind: "flow", Object: "invariantproject.note", Steps: []protocol.ProcessStep{{Name: "mark", Kind: "action", Act: "edit"}}}
	if _, err := owner.ProcessFromAsset(asset(acting, []any{}, true)); err == nil {
		t.Fatal("empty actions on a flow whose steps act must still be refused")
	}
}

func TestInvariantK9DateWaitUntilAndArchivedSubjectCancelFlow(t *testing.T) {
	const tenant = "invariant-k9-date-wait"
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{
		work.New(tenant, actions.NewAuthority),
		automation.New(tenant, actions.NewAuthority, PublicationResolver()),
	})
	deal := invariantObject("deal", "Deal")
	deal["fields"] = []ontology.Field{
		{Name: "note", Type: "text", Title: "Note"},
		{Name: "nextstepdue", Type: "date", Title: "Next step due"},
		{Name: "owner", Type: "member", Title: "Owner"},
	}
	deal["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "won", Title: "Won"}}
	live.accept(t, "build.object.create", build.ObjectType, "DEAL", deal)
	live.accept(t, "build.object.publish", build.ObjectType, "DEAL", map[string]any{})
	live.accept(t, "build.process.create", protocol.ProcessType, "NEXTSTEP", map[string]any{
		"name": "nextstep", "title": "Next step reminder", "kind": "flow", "object": "invariantproject.deal", "when": "open",
		"steps": []protocol.ProcessStep{
			{Name: "due", Kind: "wait", WaitUntil: &platform.Binding{Source: "subject", Path: []string{"nextstepdue"}}, Next: "tell"},
			{Name: "tell", Kind: "notify", Notify: &protocol.NotificationEffect{Title: "Next step overdue", Body: "Follow up.", Field: "owner"}, Next: "done"},
			{Name: "done", Kind: "end"},
		},
	})
	live.accept(t, protocol.SchemaProcess, protocol.ProcessType, "NEXTSTEP", map[string]any{})
	dueDay := invariantClock.AddDate(0, 0, 2).Format(time.DateOnly)
	live.accept(t, "invariantproject.deal.create", "invariantproject.deal", "DEAL-LIVE", map[string]any{"note": "Live", "nextstepdue": dueDay, "owner": "builder"})
	live.accept(t, "invariantproject.deal.create", "invariantproject.deal", "DEAL-DEMO", map[string]any{"note": "Demo", "nextstepdue": dueDay, "owner": "builder"})
	live.Work(invariantClock)
	flowState := func(id string) string {
		t.Helper()
		inst, ok := platform.Get[protocol.FlowInstance](live.automation(automation.ID, false), "build.nextstep:"+id)
		if !ok {
			t.Fatalf("flow instance for %s is missing", id)
		}
		return inst.State
	}
	if got := flowState("DEAL-LIVE"); got != "waiting" {
		t.Fatalf("date waitUntil did not park live deal flow: state=%s", got)
	}
	if got := flowState("DEAL-DEMO"); got != "waiting" {
		t.Fatalf("date waitUntil did not park demo deal flow: state=%s", got)
	}
	live.accept(t, "invariantproject.deal.archive", "invariantproject.deal", "DEAL-DEMO", map[string]any{})
	live.Work(invariantClock)
	if got := flowState("DEAL-DEMO"); got != "canceled" {
		t.Fatalf("archiving subject did not cancel waiting flow: state=%s", got)
	}
	after := invariantClock.AddDate(0, 0, 3)
	live.Work(after)
	if got := flowState("DEAL-LIVE"); got != "done" {
		t.Fatalf("live deal flow did not complete after date deadline: state=%s", got)
	}
	if got := flowState("DEAL-DEMO"); got != "canceled" {
		t.Fatalf("canceled demo flow changed state after deadline: state=%s", got)
	}
	if tasks := live.Projection.Type(protocol.WorkTaskType).Rows().Len(); tasks != 0 {
		t.Fatalf("expected 0 failure tasks after date wait and archived subject cancel, got %d", tasks)
	}
	notes := live.Notices.ForMember("builder")
	if len(notes) != 1 || notes[0].Ref != "invariantproject.deal/DEAL-LIVE" {
		t.Fatalf("expected exactly 1 notification for live deal only, got %+v", notes)
	}
}

func TestInvariantK9UnboundFlowSurvivesCompatibleUpgradeAndRestart(t *testing.T) {
	flowApps := func(id string) []platform.Service {
		return []platform.Service{work.New(id, actions.NewAuthority), automation.New(id, actions.NewAuthority, PublicationResolver())}
	}
	publishPkg := func(version string, extra bool) platform.PackageDescriptor {
		t.Helper()
		src := newInvariantTenantWithApps(t, "flow-pkg-src-"+version, flowApps("flow-pkg-src-"+version))
		fields := []ontology.Field{{Name: "note", Title: "Note", Type: "text"}}
		if extra {
			fields = append(fields, ontology.Field{Name: "externalref", Title: "External ref", Type: "text"})
		}
		obj := invariantObject("lead", "Lead")
		obj["fields"] = fields
		obj["states"] = []ontology.State{{Name: "open", Title: "Open"}}
		src.accept(t, "build.object.create", build.ObjectType, "OBJ1", obj)
		src.accept(t, build.SchemaPublish, build.ObjectType, "OBJ1", map[string]any{})
		src.accept(t, "build.process.create", protocol.ProcessType, "PROC1", map[string]any{
			"name": "leadsla", "title": "Lead SLA", "object": "invariantproject.lead", "when": "open",
			"steps": []protocol.ProcessStep{
				{Name: "ask", Title: "Review", Kind: "ask", Ask: build.User, Answers: []string{"ok"}, Next: "mark"},
				{Name: "mark", Kind: "action", Act: "edit", Inputs: map[string]platform.Binding{"note": {Source: "literal", Value: platform.Raw("Reviewed")}}},
			},
		})
		src.accept(t, protocol.SchemaProcess, protocol.ProcessType, "PROC1", map[string]any{})
		src.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
			"name": "crmapp", "title": "CRM",
			"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.lead"}, {App: build.ID, Kind: platform.AssetFlow, Name: "build.leadsla"}},
		})
		candidateID := invariantPublishData(t, src, src.member(t), []build.JointDraftRef{
			{Kind: platform.AssetObject, ID: "OBJ1"}, {Kind: platform.AssetFlow, ID: "PROC1"}, {Kind: platform.AssetApp, ID: "APP1"},
		}, "flow-pkg-"+version)
		candidate, err := PublicationResolver().ReadCandidate(candidateID, src.releases.Raw(candidateID))
		if err != nil {
			t.Fatal(err)
		}
		desc, err := packages.Rebind(platform.PackageDescriptor{
			ID: "crm", Version: version, Namespace: "crm", Title: "CRM",
			Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "crmapp"}}, Assets: candidate.Assets},
		}, "crm")
		if err != nil {
			t.Fatal(err)
		}
		return desc
	}

	v1, v2 := publishPkg("0.3.1", false), publishPkg("0.3.2", true)
	seat := protocol.Seat{Subjects: []string{"package-admin"}, Member: platform.Member{ID: "package-admin", Roles: map[string]string{"platform": "admin", "crm": "user", build.ID: build.User}}}
	target := newInvariantTenantWithApps(t, "flow-pkg-target", flowApps("flow-pkg-target"), seat)
	admin, _ := target.Member("package-admin")
	submitPkg := func(schema string, payload any) {
		t.Helper()
		sub := target.submission(schema, protocol.PackageType, "crm", payload)
		sub.Authority, sub.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, sub, invariantClock); refusal != nil {
			t.Fatalf("%s: %#v", schema, refusal)
		}
	}
	for _, desc := range []platform.PackageDescriptor{v1, v2} {
		offer, err := target.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
		if err != nil {
			t.Fatal(err)
		}
		submitPkg(packages.SchemaPackageImport, map[string]any{"id": desc.ID, "digest": offer.Digest})
	}
	submitPkg(packages.SchemaPackageInstall, map[string]any{"id": "crm", "version": "0.3.1", "namespace": "crm"})

	createLead := target.submission("crm.lead.create", "crm.lead", "L1", map[string]any{"note": "Open lead"})
	createLead.Authority, createLead.PrincipalId = build.ID, admin.ID
	if _, refusal := target.Submit(admin, createLead, invariantClock); refusal != nil {
		t.Fatalf("create lead: %#v", refusal)
	}
	target.Work(invariantClock.Add(2 * time.Second))
	if err := target.Processes.Check(); err != nil {
		t.Fatalf("before upgrade: %v", err)
	}

	submitPkg(packages.SchemaPackageUpgrade, map[string]any{"id": "crm", "version": "0.3.2"})
	if err := target.Processes.Check(); err != nil {
		t.Fatalf("after upgrade: %v", err)
	}

	raw, _, err := target.Snapshot(func() int64 { return int64(len(target.entries)) })
	if err != nil {
		t.Fatal(err)
	}
	snapRestored := newInvariantTenantReplayWithApps(t, target.ID, flowApps(target.ID), seat)
	if err := snapRestored.Restore(raw); err != nil {
		t.Fatalf("snapshot restore: %v", err)
	}
	if err := snapRestored.Processes.Check(); err != nil {
		t.Fatalf("snapshot process check: %v", err)
	}

	replayed := newInvariantTenantReplayWithApps(t, target.ID, flowApps(target.ID), seat)
	if err := replayed.Replay(target.entries); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := replayed.Processes.Check(); err != nil {
		t.Fatalf("replay process check: %v", err)
	}
	// Completing the waiting task after package upgrade must step and finish the unbound flow.
	replayedRow := replayed.Projection.Type(protocol.FlowInstanceType).Rows().Get("build.leadsla:L1")
	if replayedRow == nil {
		t.Fatal("missing replayed flow instance")
	}
	taskID := replayedRow.Value.Interface().(protocol.FlowInstance).Tokens[0].Task
	replayedAdmin, _ := replayed.Member("package-admin")
	completeSub := replayed.submission("work.task.complete", "work.task", taskID, map[string]any{"answer": "ok"})
	completeSub.Authority, completeSub.PrincipalId = "work", replayedAdmin.ID
	if _, refusal := replayed.Submit(replayedAdmin, completeSub, invariantClock.Add(4*time.Second)); refusal != nil {
		t.Fatalf("complete waiting task after upgrade: %#v", refusal)
	}
	replayed.Work(invariantClock.Add(6 * time.Second))
	finished := replayed.Projection.Type(protocol.FlowInstanceType).Rows().Get("build.leadsla:L1").Value.Interface().(protocol.FlowInstance)
	if finished.State != "done" {
		t.Fatalf("expected unbound flow to finish after upgrade, got state=%q", finished.State)
	}

	// Forged dependency digest on the unbound running instance must still be rejected.
	row := snapRestored.Projection.Type(protocol.FlowInstanceType).Rows().Get("build.leadsla:L1")
	if row == nil {
		t.Fatal("missing waiting flow instance")
	}
	forged := row.Value.Interface().(protocol.FlowInstance)
	forged.Dependencies = "sha256-v1:0000000000000000000000000000000000000000000000000000000000000000"
	forgedBytes, _ := json.Marshal(forged)
	if _, err := snapRestored.restoreRecords(map[string][]records.StoredState{protocol.FlowInstanceType: {{Value: forgedBytes}}}); err != nil {
		t.Fatal(err)
	}
	if err := snapRestored.Processes.Check(); err == nil {
		t.Fatal("forged dependency digest on unbound instance was accepted")
	}

	// Builder compatible object expansion across multiple publishes + restart.
	dev := newInvariantTenantWithApps(t, "flow-dev-target", flowApps("flow-dev-target"))
	obj := invariantObject("visit", "Visit")
	obj["fields"] = []ontology.Field{{Name: "guest", Title: "Guest", Type: "text"}}
	obj["states"] = []ontology.State{{Name: "open", Title: "Open"}}
	dev.accept(t, "build.object.create", build.ObjectType, "O1", obj)
	dev.accept(t, build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	dev.accept(t, "build.process.create", protocol.ProcessType, "P1", map[string]any{
		"name": "visitcheck", "title": "Visit check", "object": "invariantproject.visit", "when": "open",
		"steps": []protocol.ProcessStep{{Name: "ask", Title: "Check", Kind: "ask", Ask: build.User, Answers: []string{"yes"}}},
	})
	dev.accept(t, protocol.SchemaProcess, protocol.ProcessType, "P1", map[string]any{})
	dev.accept(t, "invariantproject.visit.create", "invariantproject.visit", "V1", map[string]any{"guest": "Ada"})
	dev.Work(invariantClock.Add(2 * time.Second))
	dev.accept(t, "build.object.edit", build.ObjectType, "O1", map[string]any{
		"fields": []ontology.Field{{Name: "guest", Title: "Guest", Type: "text"}, {Name: "room", Title: "Room", Type: "text"}},
	})
	dev.accept(t, build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	dev.accept(t, "build.object.edit", build.ObjectType, "O1", map[string]any{
		"fields": []ontology.Field{{Name: "guest", Title: "Guest", Type: "text"}, {Name: "room", Title: "Room", Type: "text"}, {Name: "floor", Title: "Floor", Type: "integer"}},
	})
	dev.accept(t, build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	if err := dev.Processes.Check(); err != nil {
		t.Fatalf("dev after two compatible publishes: %v", err)
	}
	devReplayed := newInvariantTenantReplayWithApps(t, dev.ID, flowApps(dev.ID))
	if err := devReplayed.Replay(dev.entries); err != nil {
		t.Fatalf("dev replay: %v", err)
	}
	if err := devReplayed.Processes.Check(); err != nil {
		t.Fatalf("dev replay check: %v", err)
	}
}
