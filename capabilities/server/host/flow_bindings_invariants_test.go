package host

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"platformserver/kernel/protocol"
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

func TestInvariantK9ManagedFlowStartingReleaseSurvivesPackageUpgrade(t *testing.T) {
	apps := func(id string) []platform.Service {
		return []platform.Service{work.New(id, actions.NewAuthority), automation.New(id, actions.NewAuthority, PublicationResolver())}
	}
	descriptor := func(version, title string, extra bool) platform.PackageDescriptor {
		id := "flow-source-" + version
		source := newInvariantTenantWithApps(t, id, apps(id))
		object := invariantObject("deal", "Deal")
		fields := []ontology.Field{{Name: "note", Type: "text", Title: "Note"}, {Name: "due", Type: "date", Title: "Due"}, {Name: "owner", Type: "member", Title: "Owner"}}
		if extra {
			fields = append(fields, ontology.Field{Name: "extra", Type: "text", Title: "Extra"})
		}
		object["fields"] = fields
		object["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "won", Title: "Won"}}
		source.accept(t, "build.object.create", build.ObjectType, "DEAL", object)
		source.accept(t, "build.object.publish", build.ObjectType, "DEAL", map[string]any{})
		source.accept(t, "build.process.create", protocol.ProcessType, "REMINDER", map[string]any{
			"name": "reminder", "title": "Reminder", "kind": "flow", "object": "invariantproject.deal", "when": "open",
			"steps": []protocol.ProcessStep{
				{Name: "due", Kind: "wait", WaitUntil: &platform.Binding{Source: "subject", Path: []string{"due"}}, Next: "tell"},
				{Name: "tell", Kind: "notify", Notify: &protocol.NotificationEffect{Title: title, Body: "Follow up", Field: "owner"}, Next: "done"},
				{Name: "done", Kind: "end"},
			},
		})
		source.accept(t, protocol.SchemaProcess, protocol.ProcessType, "REMINDER", map[string]any{})
		ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFlow, Name: "build.reminder"}
		root := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "reminderapp"}
		source.accept(t, "build.app.create", protocol.AppType, "APP", map[string]any{
			"name": root.Name, "title": "Reminder app", "resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.deal"}, ref},
		})
		candidate := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
			{Kind: platform.AssetObject, ID: "DEAL"}, {Kind: platform.AssetFlow, ID: "REMINDER"}, {Kind: platform.AssetApp, ID: "APP"},
		}, "flow-"+version)
		saved, err := source.releases.Candidate(candidate)
		if err != nil {
			t.Fatal(err)
		}
		return platform.PackageDescriptor{ID: "reminderpkg", Version: version, Namespace: "reminderpkg", Title: "Reminder package",
			Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{root}, Assets: saved.Assets}}
	}
	const id = "managed-flow-upgrade"
	seat := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin"}}}
	live := newInvariantTenantWithApps(t, id, apps(id), seat)
	admin, _ := live.Member("admin")
	install := func(desc platform.PackageDescriptor, action string) {
		t.Helper()
		preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			schema  string
			payload map[string]any
		}{
			{packages.SchemaPackageImport, map[string]any{"id": desc.ID, "digest": preview.Digest}},
			{action, map[string]any{"id": desc.ID, "version": desc.Version, "namespace": "reminderpkg"}},
		} {
			req := live.submission(step.schema, protocol.PackageType, desc.ID, step.payload)
			req.Authority, req.PrincipalId = "platform", admin.ID
			if _, refused := live.Submit(admin, req, invariantClock); refused != nil {
				t.Fatalf("%s %s: %v", step.schema, desc.Version, refused)
			}
		}
	}
	install(descriptor("1.0.0", "Original reminder", false), packages.SchemaPackageInstall)
	due := invariantClock.AddDate(0, 0, 2).Format(time.DateOnly)
	live.accept(t, "reminderpkg.deal.create", "reminderpkg.deal", "D1", map[string]any{"note": "Original", "due": due, "owner": "builder"})
	live.Work(invariantClock)
	flow := func(tenant *invariantTenant, key string) protocol.FlowInstance {
		t.Helper()
		x, ok := platform.Get[protocol.FlowInstance](tenant.automation(automation.ID, false), "build.reminder:"+key)
		if !ok {
			t.Fatalf("missing flow %s", key)
		}
		return x
	}
	first := flow(live, "D1")
	if first.State != "waiting" || first.Release == "" || first.Release != first.Dependencies || live.releases.Active() != "" {
		t.Fatalf("managed flow must pin a starting closure without activating a tenant release: %+v", first)
	}
	if err := live.Processes.Check(); err != nil {
		t.Fatal(err)
	}
	install(descriptor("1.1.0", "New reminder", true), packages.SchemaPackageUpgrade)
	if err := live.Processes.Check(); err != nil {
		t.Fatalf("upgrade broke the old instance: %v", err)
	}
	live.accept(t, "reminderpkg.deal.create", "reminderpkg.deal", "D2", map[string]any{"note": "New", "due": due, "owner": "builder"})
	live.Work(invariantClock)
	next := flow(live, "D2")
	if next.Release == "" || next.Release == first.Release || next.State != "waiting" {
		t.Fatalf("new instance must bind the new closure: %+v", next)
	}
	restored := newInvariantTenantReplayWithApps(t, id, apps(id), seat)
	restored.Files = live.files()
	if err := restored.recoverEntries(live.entries); err != nil {
		t.Fatal(err)
	}
	if err := restored.Processes.Check(); err != nil {
		t.Fatalf("replay cannot recover pinned flows: %v", err)
	}
	if recovered := flow(restored, "D1"); recovered.Release != first.Release || recovered.Dependencies != first.Dependencies {
		t.Fatalf("recovery changed the original binding: %+v", recovered)
	}
	restored.Work(invariantClock.AddDate(0, 0, 3))
	if old, newer := flow(restored, "D1"), flow(restored, "D2"); old.State != "done" || newer.State != "done" {
		t.Fatalf("retained and current flows must both complete: %s / %s", old.State, newer.State)
	}
	notes := platform.Raw(restored.Notices.ForMember("builder"))
	for _, title := range []string{"Original reminder", "New reminder"} {
		if !strings.Contains(string(notes), title) {
			t.Fatalf("missing notification from starting flow version %s: %s", title, notes)
		}
	}
}
