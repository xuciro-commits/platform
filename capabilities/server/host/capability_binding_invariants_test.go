package host

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK41OptionalCapabilityUseDegradesUntilBound(t *testing.T) {
	const tenant, contract, sender = "invariant-k41", "telephony.fax/1", "invariantproject.sender"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	agent := runtimeprotocol.Seat{Subjects: []string{"agent"}, Member: platform.Member{ID: "agent", Tenant: tenant, Roles: map[string]string{"invariantproject": ontology.BuilderRole}}}
	live := newInvariantTenant(t, tenant, admin, agent)
	try := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, id, payload), invariantClock)
		return refusal
	}
	bind := func(tenant *invariantTenant, provider string) {
		t.Helper()
		sub := tenant.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": provider})
		sub.PrincipalId, sub.Authority = "admin", controlowner.PlatformApp
		member, _ := tenant.Member("admin")
		if _, refusal := tenant.Submit(member, sub, invariantClock); refusal != nil {
			t.Fatalf("binding %q: %s", provider, refusal.Message)
		}
	}
	offered := func(tenant *invariantTenant) bool {
		return slices.ContainsFunc(tenant.Catalog(tenant.member(t)), func(a platform.Action) bool { return a.Schema == sender+".fax" })
	}

	fax := invariantObject("fax", "Fax")
	fax["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.fax", Version: 1, Actions: map[string]string{"send": "edit"}}}
	live.accept(t, "build.object.create", build.ObjectType, "FAX", fax)
	live.accept(t, "build.object.publish", build.ObjectType, "FAX", map[string]any{})
	providers := live.router().Providers(contract)
	if len(providers) != 1 || len(providers[0].Provision.Protocol.Actions) != 1 {
		t.Fatalf("the fax object provides the protocol: %+v", providers)
	}
	live.accept(t, "build.contract.create", build.ContractType, "FAX", map[string]any{"name": "telephony.fax", "major": 1, "title": "Fax",
		"actions": []runtimeprotocol.ContractAction{{Name: "send", Title: "Send", Payload: providers[0].Provision.Protocol.Actions[0].Payload}}})
	live.accept(t, "build.contract.publish", build.ContractType, "FAX", map[string]any{})
	if _, bound := live.router().Resolve(contract); bound {
		t.Fatal("installing a contract and an implementation must not bind them")
	}

	object := invariantObject("sender", "Sender")
	object["states"] = []ontology.State{{Name: "open", Title: "Open"}}
	live.accept(t, "build.object.create", build.ObjectType, "SENDER", object)
	action := func(uses ...string) ontology.Action {
		return ontology.Action{Name: "fax", Title: "Fax it", From: []string{"open"}, Roles: []string{ontology.BuilderRole}, Uses: uses,
			Transaction: &platform.BusinessTxn{Subject: sender, Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: sender, ID: "$subject"},
				Changes: []platform.TxnSet{{Field: "note", Expr: &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("faxed")}}}}}}}}
	}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "FAXIT", map[string]any{"name": sender + ".fax", "object": sender, "definition": action(contract + "#print")})
	refused := try("build.actiontype.publish", build.ActionTypeType, "FAXIT", map[string]any{})
	if refused == nil {
		refused = try("build.object.publish", build.ObjectType, "SENDER", map[string]any{})
	}
	if refused == nil || !strings.Contains(refused.Message, "not an action of an installed capability contract") {
		t.Fatalf("using an action the contract does not declare must be refused by name: %+v", refused)
	}
	live.accept(t, "build.actiontype.edit", build.ActionTypeType, "FAXIT", map[string]any{"definition": action(contract + "#send")})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "FAXIT", map[string]any{})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "FAXIT", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "SENDER", map[string]any{})
	if !slices.ContainsFunc(live.App(build.ID).Manifest().Consumes, func(c platform.Consumption) bool { return c.Protocol == contract && c.Optional }) {
		t.Fatalf("the builder consumes the used contract optionally: %+v", live.App(build.ID).Manifest().Consumes)
	}
	if offered(live) {
		t.Fatal("an action using an unbound capability must not be offered")
	}
	_, _, refusal := live.Invoke(live.member(t), contract, "send", "F1", platform.Raw(map[string]any{}), "k41-unbound", invariantClock)
	if refusal == nil || !strings.Contains(refusal.Message, "no bound implementation") {
		t.Fatalf("calling an unbound capability answers that it is unbound: %+v", refusal)
	}

	bind(live, build.ID)
	if bound, ok := live.router().Resolve(contract); !ok || bound.ID() != build.ID {
		t.Fatal("binding chooses the implementation")
	}
	if !offered(live) {
		t.Fatal("an action using a bound capability is offered")
	}
	if holder, _ := live.Member("agent"); !slices.ContainsFunc(live.Catalog(holder), func(a platform.Action) bool { return a.Schema == sender+".fax" }) {
		t.Fatal("a member whose role is in the implementing app is offered the action, as the record itself allows it")
	}
	replayed := live.restore(t, live.entries, admin)
	if !offered(replayed) || !reflect.DeepEqual(replayed.recordImage(t), live.recordImage(t)) {
		t.Fatal("replay must restore the binding and the records")
	}

	bind(live, "")
	if _, ok := live.router().Resolve(contract); ok || offered(live) {
		t.Fatal("unbinding takes the capability and its entries away again")
	}
	if offered(live.restore(t, live.entries, admin)) {
		t.Fatal("replay must restore the unbinding")
	}
}
