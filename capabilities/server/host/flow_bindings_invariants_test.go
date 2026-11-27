package host

import (
	"encoding/json"
	"testing"
	"time"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	automationauthoring "platformserver/services/automate/authoring"
	automation "platformserver/services/automate/runtime"
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
