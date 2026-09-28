package platformserver

import (
	"bytes"
	"encoding/json"
	"testing"

	"platformserver/platform"
)

func TestCodeReleaseDescriptorsKeepHiddenExecutionRules(t *testing.T) {
	entity := platform.Entity{
		Type: "sample.request",
		Scope: platform.Scope{
			Levels: map[string]string{"owner": platform.ScopeOwn},
			Owner:  "requester",
		},
		Lifecycle: &platform.Lifecycle{
			Field: "state", Initial: "draft",
			Transitions: []platform.Transition{{
				Name: "approve", Roles: []string{"reviewer"}, From: []string{"draft"},
				To: []string{"approved"}, Approval: &platform.Approval{
					Levels: []platform.ApprovalLevel{{AppRole: "manager", All: true}},
				},
			}},
		},
	}
	action := platform.Action{
		Schema: "sample.request.approve", Target: entity.Type, Roles: []string{"reviewer"},
		Approval: &platform.Approval{Levels: []platform.ApprovalLevel{{AppRole: "manager", All: true}}},
	}
	object, err := json.Marshal(codeObjectDescriptor(entity, platform.EntityInfo{Type: entity.Type}))
	if err != nil {
		t.Fatal(err)
	}
	encodedAction, err := json.Marshal(codeActionDescriptor(action))
	if err != nil {
		t.Fatal(err)
	}
	for label, raw := range map[string][]byte{"object": object, "action": encodedAction} {
		for _, required := range []string{`"reviewer"`, `"manager"`, `"All":true`} {
			if !bytes.Contains(raw, []byte(required)) {
				t.Errorf("%s descriptor omitted %s: %s", label, required, raw)
			}
		}
	}
	if !bytes.Contains(object, []byte(`"own"`)) || !bytes.Contains(object, []byte(`"requester"`)) {
		t.Fatalf("object omitted unfiltered data scope: %s", object)
	}
}
