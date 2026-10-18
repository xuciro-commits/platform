package mes

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformkernel/kernel"
	"platformserver"
	"platformserver/platform"
	"production"
)

// schemas are the plant's and the platform's actions of a catalog; every member
// also has the work app's (tasks, approvals, saved views) and may ask agents.
func schemas(actions []platform.Action) []string {
	var out []string
	for _, a := range actions {
		if !strings.HasPrefix(a.Schema, "work.") && !strings.HasPrefix(a.Schema, "agent.") {
			out = append(out, a.Schema)
		}
	}
	return out
}

// #90: one declaration serves every caller; each receives only what its role may call.
func TestCatalogPerCaller(t *testing.T) {
	p := newPlant(t)
	assistant := member("agent-l1", Assistant, "L1")
	read, lang := platformserver.SchemaNotificationRead, platformserver.SchemaProfileUpdate
	// Every member may call the platform's declared operations (ADR-0016 §13).
	call := platformserver.SchemaOperationCall
	delegate, issue, revoke := platformserver.SchemaDelegate, platformserver.SchemaTokenIssue, platformserver.SchemaTokenRevoke
	for _, c := range []struct {
		who  platform.Caller
		want []string
	}{
		{sup, []string{lang, delegate, issue, revoke, read, call, SchemaAdvice, SchemaRelease, SchemaReason, SchemaConfirm, SchemaAnswer, SchemaResend}},
		{op1, []string{lang, delegate, issue, revoke, read, call, SchemaStart, SchemaComplete, SchemaNC, SchemaReason}},
		{qa1, []string{lang, delegate, issue, revoke, read, call, SchemaNC, SchemaSign}},
		{assistant, []string{lang, delegate, issue, revoke, read, call, SchemaReason, SchemaResend}},
		{gw, []string{lang, delegate, issue, revoke, read, call}}, // every member chooses its language and marks its own notifications
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

// Releasing can place the order in the enterprise model: the ref payload field
// is checked by the kernel — an element the model does not name never reaches
// the plant — and the decision keeps the place on the record (ADR-0034).
func TestReleasePlacesOrderInModel(t *testing.T) {
	p := newPlant(t)
	// DemoOrganization's units — the plant and its lines — are model elements.
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-70", releasePayload{Product: "P-100", Quantity: 2, SFCs: 1, Place: "L1"}), "ok")
	found := false
	for _, o := range p.Orders() {
		if o.ID == "SO-70" {
			found = true
			expect(t, o.Place, "L1")
		}
	}
	if !found {
		t.Fatal("released order is not among the records")
	}
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-71", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1, Place: "ghost"}), "ERROR_CODE_INVALID_ARGUMENT")
}

func TestReleaseFormUsesActualMasterDefaultsAndOriginalChoices(t *testing.T) {
	master := DemoMaster()
	slices.Reverse(master.Products)
	plant := New(tenant, master)
	action, ok := plant.Manifest().Actions.Action(SchemaRelease)
	if !ok || action.IDPrefix != "SO" {
		t.Fatalf("release form identity: %+v", action)
	}
	fields := map[string]platform.Field{}
	for _, field := range action.Payload {
		fields[field.Name] = field
	}
	if string(fields["product"].Default) != string(platform.Raw(master.Products[0].ID)) || string(fields["quantity"].Default) != "1" || string(fields["sfcs"].Default) != "1" {
		t.Fatalf("release defaults did not follow the plant: %+v", action.Payload)
	}
	if fields["product"].From != "products" || fields["planned"].From != "available-planned-orders" || fields["planned"].Key != "erpId" || !slices.Equal(fields["place"].Stereotypes, []string{"ActualLocation", "ActualOrganization"}) {
		t.Fatalf("release form lost an original choice: %+v", action.Payload)
	}
}

func TestReleaseRejectsInvalidLotsAndExistingOrderWithoutChangingRecords(t *testing.T) {
	p := newPlant(t)
	for _, lots := range []int{0, -1, 3} {
		expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-invalid", releasePayload{Product: "P-100", Quantity: 2, SFCs: lots}), "ERROR_CODE_INVALID_ARGUMENT")
	}
	if len(p.Orders()) != 0 || len(p.SFCs()) != 0 {
		t.Fatal("invalid lot counts created records")
	}
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-valid", releasePayload{Product: "P-100", Quantity: 2, SFCs: 1}), "ok")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-valid", releasePayload{Product: "P-100", Quantity: 7, SFCs: 2}), "ERROR_CODE_CONFLICT")
	if len(p.Orders()) != 1 || len(p.SFCs()) != 1 || p.Orders()[0].Quantity != 2 {
		t.Fatal("an existing order was overwritten or split again")
	}
}

type shopOrderChoiceRuntime struct {
	platform.Runtime
	plans  []production.Order
	orders []Order
	err    *kernel.Error
}

func (r *shopOrderChoiceRuntime) Query(platform.Caller, string, string, ...string) ([]platform.ProviderResult, *kernel.Error) {
	return []platform.ProviderResult{{Result: r.plans}}, nil
}

func (r *shopOrderChoiceRuntime) Find(_ platform.Caller, typ reflect.Type, query platform.Query) ([]any, int, *kernel.Error) {
	if typ != reflect.TypeFor[Order]() || !query.Archived {
		panic("the shop-order chooser must inspect its original orders including archived ones")
	}
	rows := make([]any, len(r.orders))
	for i, order := range r.orders {
		rows[i] = order
	}
	return rows, len(rows), r.err
}

func TestAvailablePlannedOrdersKeepOpenUnclaimedOptionsAndReadFailures(t *testing.T) {
	runtime := &shopOrderChoiceRuntime{
		plans:  []production.Order{{ID: "open", State: "released"}, {ID: "sent", State: "sent"}, {ID: "confirmed", State: "confirmed"}, {ID: "claimed", State: "released"}, {ID: "retry", State: "refused"}},
		orders: []Order{{Planned: "claimed"}},
	}
	caller := platform.NewCaller(runtime, sup.Member, ID, false, false)
	options, err := New(tenant, DemoMaster()).Read(caller, "available-planned-orders")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, plan := range options.([]PlannedOrder) {
		ids = append(ids, plan.ERPID)
	}
	if !slices.Equal(ids, []string{"open", "retry"}) {
		t.Fatalf("unavailable plans were offered: %v", ids)
	}
	runtime.err = denied
	if _, err := availablePlanned(caller); err != denied {
		t.Fatal("an unavailable order read was shown as an empty option list")
	}
}

// Deactivating a capability removes its actions and refuses new ones; what it
// recorded keeps replaying and resolving.
func TestDeactivatedCapabilityKeepsItsHistory(t *testing.T) {
	p := newPlant(t)
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0, "rddr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Tool change"}), "ok")

	if New(tenant, DemoMaster()).Disable("no-such-capability") {
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
