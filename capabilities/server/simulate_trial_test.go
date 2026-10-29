package platformserver

import trialowner "platformserver/studio/trials"

import "platformserver/studio/build"

import (
	"encoding/json"
	"testing"
	"time"

	enterprise "platformserver/engine/organization"
	"platformserver/platform"
)

func TestApplicationTrialUsesExplicitOrganizationWithoutCopyingLiveMemberships(t *testing.T) {
	const id = "organization-trial"
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	seats := []platform.Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("other", "build:operator")}
	live := platform.OrgSeed{Units: []platform.Unit{{ID: "secret", Name: "Live private organization", Kind: "department"}}, Memberships: []platform.Membership{{Party: "member:other", Unit: "secret", Role: "employee"}}}
	tn := composeTenant(t, id, seats, build.New(id), enterprise.New(id, live))
	decide(t, tn, "builder", "build", "build.object.create", "build.object", "ASSIGNMENT", map[string]any{
		"name": "assignment", "title": "Assignment", "plural": "Assignments",
		"fields": []build.Field{{Name: "unit", Title: "Unit", Type: "reference", Ref: "enterprise.element", Stereotype: "ActualOrganization", Required: true}, {Name: "note", Title: "Note", Type: "text"}},
		"scope":  build.ObjectScope{Unit: "unit", Structure: "site"}, "access": []build.Access{{Role: "operator", Read: "unit", Create: true, Edit: true}},
	}, at)
	organization := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}}, Units: []platform.Unit{{ID: "synthetic", Name: "Synthetic department", Kind: "department"}}, Memberships: []platform.Membership{{Party: "member:operator", Unit: "synthetic", Role: "employee"}}}
	request := trialowner.CandidateSimulationRequest{ObjectID: "ASSIGNMENT", Actors: []string{"operator", "other"}, At: at, Organization: &organization,
		Samples: []trialowner.SimulationSample{{Type: "build.assignment", Records: []json.RawMessage{platform.Raw(map[string]any{"id": "A", "unit": "synthetic", "note": "Synthetic task"})}}},
		Steps:   []trialowner.SimulationStep{{Type: "build.assignment", ID: "A", Read: true, Payload: platform.Raw(map[string]any{})}, {Type: "build.assignment", ID: "A", Action: "build.assignment.edit", Payload: platform.Raw(map[string]any{"note": "Completed synthetic task"})}, {Type: "build.assignment", ID: "LEAK", Action: "build.assignment.create", Payload: platform.Raw(map[string]any{"unit": "secret", "note": "Must not resolve live organizations"}), Expect: "refused"}},
	}
	before := snapshot(tn)
	result, failure := tn.SimulateCandidate(memberOf(t, tn, "builder"), request)
	if failure != nil || len(result.Actors) != 2 {
		t.Fatalf("isolated organization: %+v %s", failure, platform.Raw(result))
	}
	if !result.Actors[0].Steps[0].Accepted || !result.Actors[0].Steps[1].Accepted || result.Actors[1].Steps[0].Accepted || result.Actors[1].Steps[1].Accepted || !result.Actors[0].Recovered || !result.Actors[1].Recovered {
		t.Fatalf("same-role unit isolation or recovery: %s", platform.Raw(result))
	}
	for _, actor := range result.Actors {
		if actor.Steps[2].Accepted {
			t.Fatal("a live organization or membership entered the sandbox")
		}
	}
	if snapshot(tn) != before {
		t.Fatal("the synthetic organization changed live state or grants")
	}
}
