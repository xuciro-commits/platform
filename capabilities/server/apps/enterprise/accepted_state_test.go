package enterprise

import (
	"bytes"
	"testing"

	"platformserver/platform"
)

func TestAcceptedModelOwnsNestedValuesAndAppliesWithoutDecisions(t *testing.T) {
	live := New("tenant", platform.OrgSeed{})
	live.model.Elements = []Element{{ID: "unit", Stereotype: Organization, Name: "Unit", Properties: map[string]any{"nested": map[string]any{"owner": "old"}}}}
	live.model.Views = []View{{ID: "view", Name: "View", Elements: []string{"unit"}, Layout: map[string][2]float64{"unit": {1, 2}}, Pins: []Pin{{Ref: "record:work.task/task", Label: "Old"}}}}
	before, _ := live.AcceptedState()
	app, err := live.ForkAcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	private := app.(*Enterprise)
	private.model.Elements[0].Properties["nested"].(map[string]any)["owner"] = "new"
	private.model.Views[0].Elements[0] = "next"
	private.model.Views[0].Layout["unit"] = [2]float64{3, 4}
	private.model.Views[0].Pins[0].Label = "New"
	unchanged, _ := live.AcceptedState()
	if !bytes.Equal(before, unchanged) {
		t.Fatal("private enterprise model aliases live nested values")
	}
	image, _ := private.AcceptedState()
	if err := live.ApplyAcceptedState(image); err != nil {
		t.Fatal(err)
	}
	after, _ := live.AcceptedState()
	if !bytes.Equal(image, after) || live.AcceptedLedger() != private.AcceptedLedger() || len(live.ledger.RecordsFor("tenant")) != 0 {
		t.Fatal("pure model application changed its ledger or omitted nested values")
	}
	if err := live.ApplyAcceptedState([]byte(`{"elements":[{"id":"same"},{"id":"same"}]}`)); err == nil {
		t.Fatal("duplicate enterprise element accepted")
	}
	still, _ := live.AcceptedState()
	if !bytes.Equal(after, still) {
		t.Fatal("invalid model image changed live state")
	}
}
