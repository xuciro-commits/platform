package host

import (
	"encoding/json"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	build "platformserver/services/resources/authoring"
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
