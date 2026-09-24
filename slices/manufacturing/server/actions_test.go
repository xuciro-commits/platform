package mes

import (
	"slices"
	"testing"

	"platformserver"
)

func schemas(actions []platformserver.Action) []string {
	var out []string
	for _, a := range actions {
		out = append(out, a.Schema)
	}
	return out
}

// #90: one declaration serves every caller; each receives only what its role may call.
func TestCatalogPerCaller(t *testing.T) {
	p := newPlant(t)
	assistant := Principal{ID: "agent-l1", Tenant: tenant, Role: Assistant, Lines: []string{"L1"}}
	for _, c := range []struct {
		who  Principal
		want []string
	}{
		{sup, []string{SchemaRelease, SchemaReason}},
		{op1, []string{SchemaStart, SchemaComplete, SchemaNC, SchemaReason}},
		{qa1, []string{SchemaNC, SchemaSign}},
		{assistant, []string{SchemaReason}},
		{gw, nil},
	} {
		if got := schemas(p.Catalog(c.who)); !slices.Equal(got, c.want) {
			t.Errorf("%s: catalog %v, want %v", c.who.ID, got, c.want)
		}
	}
	for _, a := range p.Catalog(sup) {
		if a.Description == "" || a.Title == "" || a.Payload == nil {
			t.Errorf("%s is not described for its callers", a.Schema)
		}
	}
}

// An AI agent is a principal like any other: its role and lines bound what it may do,
// whatever it asks for.
func TestAssistantActsWithinItsGrant(t *testing.T) {
	p := newPlant(t)
	assistant := Principal{ID: "agent-l1", Tenant: tenant, Role: Assistant, Lines: []string{"L1"}}
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0, "rddr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, assistant, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Setup"}), "ok")
	expect(t, p.Downtime()[0].Reason, "Setup")
	elsewhere := Principal{ID: "agent-l2", Tenant: tenant, Role: Assistant, Lines: []string{"L2"}}
	expect(t, submit(p, elsewhere, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Breakdown"}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, assistant, SchemaRelease, OrderType, "SO-9", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ERROR_CODE_POLICY_DENIED")
}

// Deactivating a capability removes its actions and refuses new ones; what it
// recorded keeps replaying and resolving.
func TestDeactivatedCapabilityKeepsItsHistory(t *testing.T) {
	var journal []platformserver.Entry
	p := newPlant(t)
	recorded := p.Record // newPlant's own replay check keeps running
	p.Record = func(e platformserver.Entry) { recorded(e); journal = append(journal, e) }
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0, "rddr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Tool change"}), "ok")

	again := NewPlant(tenant, DemoMaster())
	for _, d := range DemoConnectors(tenant) {
		again.RegisterConnector(d)
	}
	if !again.Disable("downtime-reasons") || again.Disable("no-such-capability") {
		t.Fatal("Disable must accept exactly the declared capabilities")
	}
	if err := again.Replay(journal); err != nil {
		t.Fatalf("history of a deactivated capability must replay: %v", err)
	}
	expect(t, again.Downtime()[0].Reason, "Tool change")
	if slices.Contains(schemas(again.Catalog(op1)), SchemaReason) {
		t.Fatal("a deactivated action stays in the catalog")
	}
	expect(t, submit(again, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Setup"}), "ERROR_CODE_UNKNOWN_SCHEMA")
	expect(t, submit(again, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
}
