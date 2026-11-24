package ontology

import "testing"

func TestAMemberInputAsksForOneMemberOfTheTenant(t *testing.T) {
	field := Input{Name: "holder", Title: "Check out to", Type: "member", Required: true}.Field()
	if field.Type != "member" || !field.Required || field.Label != "Check out to" {
		t.Fatalf("a member input becomes a required member parameter, got %+v", field)
	}
	if ValueInputTypes["member[]"] != "" {
		t.Fatal("an action input names one member, not a list")
	}
}
