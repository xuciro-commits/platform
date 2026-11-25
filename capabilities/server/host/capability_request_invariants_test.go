package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK47ActionRequestsTheBoundCapabilityOnce(t *testing.T) {
	const tenant, contract, sender = "invariant-k47", "telephony.fax/1", "invariantproject.sender"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	try := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, id, payload), invariantClock)
		return refusal
	}
	faxes := func(tenant *invariantTenant) int {
		raw, ok := tenant.recordImage(t)["invariantproject.fax"]
		if !ok {
			return 0
		}
		var records []json.RawMessage
		if err := json.Unmarshal(raw, &records); err != nil {
			t.Fatalf("fax records: %v: %s", err, raw)
		}
		return len(records)
	}

	fax := invariantObject("fax", "Fax")
	fax["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.fax", Version: 1, Actions: map[string]string{"send": "create"}}}
	live.accept(t, "build.object.create", build.ObjectType, "FAX", fax)
	live.accept(t, "build.object.publish", build.ObjectType, "FAX", map[string]any{})
	providers := live.router().Providers(contract)
	if len(providers) != 1 || len(providers[0].Provision.Protocol.Actions) != 1 {
		t.Fatalf("the fax object provides the protocol: %+v", providers)
	}
	live.accept(t, "build.contract.create", build.ContractType, "FAX", map[string]any{"name": "telephony.fax", "major": 1, "title": "Fax",
		"actions": []runtimeprotocol.ContractAction{{Name: "send", Title: "Send", Payload: providers[0].Provision.Protocol.Actions[0].Payload}}})
	live.accept(t, "build.contract.publish", build.ContractType, "FAX", map[string]any{})

	object := invariantObject("sender", "Sender")
	object["states"] = []ontology.State{{Name: "open", Title: "Open"}}
	live.accept(t, "build.object.create", build.ObjectType, "SENDER", object)
	live.accept(t, "build.object.publish", build.ObjectType, "SENDER", map[string]any{})
	action := func(uses ...string) ontology.Action {
		return ontology.Action{Name: "fax", Title: "Fax it", From: []string{"open"}, Roles: []string{ontology.BuilderRole}, Uses: uses,
			Transaction: &platform.BusinessTxn{Subject: sender, Steps: []platform.TxnStep{{Name: "send", Kind: "request",
				Request: &platform.TxnRequest{Use: contract + "#send", Payload: []platform.TxnSet{{Field: "note", Expr: &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("sent by the sender")}}}}}}}}}
	}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "FAXIT", map[string]any{"name": sender + ".fax", "object": sender, "definition": action()})
	refused := try("build.actiontype.publish", build.ActionTypeType, "FAXIT", map[string]any{})
	if refused == nil {
		refused = try("build.object.publish", build.ObjectType, "SENDER", map[string]any{})
	}
	if refused == nil || !strings.Contains(refused.Message, "does not declare in uses") {
		t.Fatalf("an action must declare the capability it requests: %+v", refused)
	}
	live.accept(t, "build.actiontype.edit", build.ActionTypeType, "FAXIT", map[string]any{"definition": action(contract + "#send")})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "FAXIT", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "SENDER", map[string]any{})

	bind := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": build.ID})
	bind.PrincipalId, bind.Authority = "admin", controlowner.PlatformApp
	member, _ := live.Member("admin")
	if _, refusal := live.Submit(member, bind, invariantClock); refusal != nil {
		t.Fatalf("binding: %s", refusal.Message)
	}

	live.accept(t, sender+".create", sender, "S1", map[string]any{"note": "first"})
	if faxes(live) != 0 {
		t.Fatal("creating the sender must not request anything")
	}
	run := live.submission(sender+".fax", sender, "S1", map[string]any{})
	if _, refusal := live.Submit(live.member(t), run, invariantClock); refusal != nil {
		t.Fatalf("the action requests the bound capability: %s", refusal.Message)
	}
	if got := faxes(live); got != 1 {
		t.Fatalf("one action makes one request at the implementation, got %d", got)
	}
	if _, refusal := live.Submit(live.member(t), run, invariantClock); refusal != nil {
		t.Fatalf("resubmitting the same key is answered: %s", refusal.Message)
	}
	if got := faxes(live); got != 1 {
		t.Fatalf("the same idempotency key makes the same request once, got %d", got)
	}
	replayed := live.restore(t, live.entries, admin)
	if faxes(replayed) != 1 || !reflect.DeepEqual(replayed.recordImage(t), live.recordImage(t)) {
		t.Fatal("replay restores the request's result without requesting again")
	}

	unbind := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": ""})
	unbind.PrincipalId, unbind.Authority = "admin", controlowner.PlatformApp
	if _, refusal := live.Submit(member, unbind, invariantClock); refusal != nil {
		t.Fatalf("unbinding: %s", refusal.Message)
	}
	live.accept(t, sender+".create", sender, "S2", map[string]any{"note": "second"})
	if refusal := try(sender+".fax", sender, "S2", map[string]any{}); refusal == nil {
		t.Fatal("an action whose capability is unbound must be refused")
	}
	if got := faxes(live); got != 1 {
		t.Fatalf("a refused action requests nothing, got %d", got)
	}
}
