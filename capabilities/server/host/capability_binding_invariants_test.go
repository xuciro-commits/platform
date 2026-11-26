package host

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
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

func TestInvariantStatelessObjectActionsRespectCapabilityBindings(t *testing.T) {
	const tenant, contract, contact = "invariant-stateless-action", "telephony.fax/1", "invariantproject.contact"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	try := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, id, payload), invariantClock)
		return refusal
	}
	bind := func(provider string) {
		t.Helper()
		sub := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": provider})
		sub.PrincipalId, sub.Authority = "admin", controlowner.PlatformApp
		member, _ := live.Member("admin")
		if _, refusal := live.Submit(member, sub, invariantClock); refusal != nil {
			t.Fatalf("binding %q: %s", provider, refusal.Message)
		}
	}
	offered := func(tenant *invariantTenant) bool {
		return slices.ContainsFunc(tenant.Catalog(tenant.member(t)), func(a platform.Action) bool { return a.Schema == contact+".dial" })
	}

	fax := invariantObject("fax", "Fax")
	fax["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.fax", Version: 1, Actions: map[string]string{"send": "edit"}}}
	live.accept(t, "build.object.create", build.ObjectType, "FAX", fax)
	live.accept(t, "build.object.publish", build.ObjectType, "FAX", map[string]any{})
	providers := live.router().Providers(contract)
	live.accept(t, "build.contract.create", build.ContractType, "FAX", map[string]any{"name": "telephony.fax", "major": 1, "title": "Fax",
		"actions": []runtimeprotocol.ContractAction{{Name: "send", Title: "Send", Payload: providers[0].Provision.Protocol.Actions[0].Payload}}})
	live.accept(t, "build.contract.publish", build.ContractType, "FAX", map[string]any{})

	live.accept(t, "build.object.create", build.ObjectType, "CONTACT", invariantObject("contact", "Contact"))
	action := func(from []string) ontology.Action {
		return ontology.Action{Name: "dial", Title: "Call", From: from, Roles: []string{ontology.BuilderRole}, Uses: []string{contract + "#send"},
			Transaction: &platform.BusinessTxn{Subject: contact, Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: contact, ID: "$subject"},
				Changes: []platform.TxnSet{{Field: "note", Expr: &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("dialed")}}}}}}}}
	}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "DIAL", map[string]any{"name": contact + ".dial", "object": contact, "definition": action([]string{"open"})})
	refused := try("build.actiontype.publish", build.ActionTypeType, "DIAL", map[string]any{})
	if refused == nil {
		refused = try("build.object.publish", build.ObjectType, "CONTACT", map[string]any{})
	}
	if refused == nil || !strings.Contains(refused.Message, "which the object does not have") {
		t.Fatalf("naming states on a stateless object must be refused: %+v", refused)
	}
	live.accept(t, "build.actiontype.edit", build.ActionTypeType, "DIAL", map[string]any{"definition": action(nil)})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "DIAL", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "CONTACT", map[string]any{})

	defs := live.Definitions(live.member(t))
	index := slices.IndexFunc(defs, func(d platform.Definition) bool {
		return d.Ref.Kind == platform.AssetObject && d.Ref.Name == contact
	})
	if index < 0 || defs[index].Entity == nil || defs[index].Entity.Lifecycle != nil {
		t.Fatalf("a stateless object with actions must keep a nil lifecycle: index=%d", index)
	}
	if offered(live) {
		t.Fatal("a stateless object action using an unbound capability must not be offered")
	}

	bind(build.ID)
	if !offered(live) {
		t.Fatal("binding the capability offers the stateless object action")
	}
	live.accept(t, contact+".create", contact, "C1", map[string]any{"note": "initial"})
	live.accept(t, contact+".dial", contact, "C1", map[string]any{})
	view, readErr := live.RecordOf(t.Context(), live.member(t), contact, "C1", invariantClock.Add(2))
	if readErr != nil || !strings.Contains(string(platform.Raw(view.Record)), `"note":"dialed"`) {
		t.Fatalf("stateless object action transaction must run: err=%+v raw=%s", readErr, string(platform.Raw(view.Record)))
	}
	listed := actions.RecordActions(live.App(build.ID), live.objectCaller(live.member(t), build.ID), contact, "C1", invariantClock.Add(2), live.Entity, func(reason string) string { return reason }, nil)
	if !slices.ContainsFunc(listed, func(a objects.RecordAction) bool { return a.Schema == contact+".dial" && a.Allowed }) {
		t.Fatalf("record actions must include allowed stateless custom action: %+v", listed)
	}
	live.accept(t, contact+".archive", contact, "C1", map[string]any{})
	archivedListed := actions.RecordActions(live.App(build.ID), live.objectCaller(live.member(t), build.ID), contact, "C1", invariantClock.Add(2), live.Entity, func(reason string) string { return reason }, nil)
	if !slices.ContainsFunc(archivedListed, func(a objects.RecordAction) bool {
		return a.Schema == contact+".dial" && !a.Allowed && strings.Contains(a.Reason, "is archived")
	}) {
		t.Fatalf("archived stateless records must mark custom actions disallowed: %+v", archivedListed)
	}
	if archivedErr := try(contact+".dial", contact, "C1", map[string]any{}); archivedErr == nil || !strings.Contains(archivedErr.Message, "is archived") {
		t.Fatalf("archived stateless records must refuse custom actions: %+v", archivedErr)
	}
	replayed := live.restore(t, live.entries, admin)
	if !offered(replayed) || !reflect.DeepEqual(replayed.recordImage(t), live.recordImage(t)) {
		t.Fatal("replay must restore stateless object actions and records")
	}
}

func TestInvariantTwoDeclarativeImplementationsInOneAppAreToldApart(t *testing.T) {
	const tenant, contract = "invariant-two-impls", "telephony.fax/1"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	bind := func(provider string) *platform.Error {
		t.Helper()
		sub := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": provider})
		sub.PrincipalId, sub.Authority = "admin", controlowner.PlatformApp
		member, _ := live.Member("admin")
		_, refusal := live.Submit(member, sub, invariantClock)
		return refusal
	}
	for _, name := range []string{"fax", "efax"} {
		object := invariantObject(name, strings.ToUpper(name))
		object["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.fax", Version: 1, Actions: map[string]string{"send": "edit"}}}
		live.accept(t, "build.object.create", build.ObjectType, strings.ToUpper(name), object)
		live.accept(t, "build.object.publish", build.ObjectType, strings.ToUpper(name), map[string]any{})
	}
	providers := live.router().Providers(contract)
	ids := []string{}
	for _, p := range providers {
		ids = append(ids, p.ID())
	}
	slices.Sort(ids)
	want := []string{build.ID + "@invariantproject.efax", build.ID + "@invariantproject.fax"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("two record types implementing one contract in one app must be two implementations: %v", ids)
	}
	listed := slices.IndexFunc(live.router().Protocols(), func(p actions.ProtocolInfo) bool { return p.ID == contract })
	if listed < 0 || !reflect.DeepEqual(slices.Sorted(slices.Values(live.router().Protocols()[listed].Providers)), want) {
		t.Fatalf("the protocol listing names both implementations: %+v", live.router().Protocols())
	}
	if refusal := bind(build.ID + "@invariantproject.missing"); refusal == nil {
		t.Fatal("binding an implementation that does not exist must be refused")
	}
	for _, chosen := range want {
		if refusal := bind(chosen); refusal != nil {
			t.Fatalf("binding %s: %s", chosen, refusal.Message)
		}
		bound, ok := live.router().Bound(contract)
		if !ok || bound.ID() != chosen || build.ID+"@"+bound.EntityType() != chosen || bound.App() != build.ID {
			t.Fatalf("binding %s chose %s (%s)", chosen, bound.ID(), bound.EntityType())
		}
		if info := live.router().Protocols()[listed]; info.Bound != chosen {
			t.Fatalf("the listing shows %q bound, want %q", info.Bound, chosen)
		}
		replayed := live.restore(t, live.entries, admin)
		if again, ok := replayed.router().Bound(contract); !ok || again.ID() != chosen {
			t.Fatalf("replay must restore the chosen implementation %s", chosen)
		}
	}
}
