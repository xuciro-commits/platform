package mes

import (
	"slices"
	"strings"
	"testing"

	"platformserver"
	"platformserver/platform"
)

// schemas are the plant's and the platform's actions of a catalog; every member
// also has the work app's (tasks, approvals, saved views).
func schemas(actions []platform.Action) []string {
	var out []string
	for _, a := range actions {
		if !strings.HasPrefix(a.Schema, "work.") {
			out = append(out, a.Schema)
		}
	}
	return out
}

// #90: one declaration serves every caller; each receives only what its role may call.
func TestCatalogPerCaller(t *testing.T) {
	p := newPlant(t)
	assistant := member("agent-l1", Assistant, "L1")
	read := platformserver.SchemaNotificationRead
	for _, c := range []struct {
		who  platform.Caller
		want []string
	}{
		{sup, []string{read, SchemaRelease, SchemaReason, SchemaConfirm, SchemaResend}},
		{op1, []string{read, SchemaStart, SchemaComplete, SchemaNC, SchemaReason}},
		{qa1, []string{read, SchemaNC, SchemaSign}},
		{assistant, []string{read, SchemaReason, SchemaResend}},
		{gw, []string{read}}, // every member marks its own notifications
	} {
		if got := schemas(p.tenant.Catalog(c.who.Member)); !slices.Equal(got, c.want) {
			t.Errorf("%s: catalog %v, want %v", c.who.ID, got, c.want)
		}
	}
	for _, a := range p.tenant.Catalog(sup.Member) {
		if a.Description == "" || a.Title == "" || a.Payload == nil {
			t.Errorf("%s is not described for its callers", a.Schema)
		}
	}
}

// An AI agent is a principal like any other: its role and lines bound what it may do,
// whatever it asks for.
func TestAssistantActsWithinItsGrant(t *testing.T) {
	p := newPlant(t)
	assistant := member("agent-l1", Assistant, "L1")
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0, "rddr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, assistant, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Setup"}), "ok")
	expect(t, p.Downtime()[0].Reason, "Setup")
	elsewhere := member("agent-l2", Assistant, "L2")
	expect(t, submit(p, elsewhere, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Breakdown"}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, assistant, SchemaRelease, OrderType, "SO-9", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ERROR_CODE_POLICY_DENIED")
}

// Deactivating a capability removes its actions and refuses new ones; what it
// recorded keeps replaying and resolving.
func TestDeactivatedCapabilityKeepsItsHistory(t *testing.T) {
	p := newPlant(t)
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0, "rddr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Tool change"}), "ok")

	if NewPlant(tenant, DemoMaster()).Disable("no-such-capability") {
		t.Fatal("Disable must accept only declared capabilities")
	}
	plant, tn := plantTenant(t, "downtime-reasons")
	again := &testPlant{Plant: plant, tenant: tn}
	if err := tn.Replay(p.journal); err != nil {
		t.Fatalf("history of a deactivated capability must replay: %v", err)
	}
	expect(t, again.Downtime()[0].Reason, "Tool change")
	if slices.Contains(schemas(tn.Catalog(op1.Member)), SchemaReason) {
		t.Fatal("a deactivated action stays in the catalog")
	}
	expect(t, submit(again, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Setup"}), "ERROR_CODE_UNKNOWN_SCHEMA")
	expect(t, submit(again, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
}
