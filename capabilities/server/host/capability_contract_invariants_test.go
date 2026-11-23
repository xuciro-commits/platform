package host

import (
	"reflect"
	"slices"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK40CapabilityContractsStandAloneAndBindImplementations(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k40")
	try := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, id, payload), invariantClock)
		return refusal
	}
	protocols := func(tenant *Tenant) map[string]actions.ProtocolInfo {
		out := map[string]actions.ProtocolInfo{}
		for _, info := range tenant.router().Protocols() {
			out[info.ID] = info
		}
		return out
	}
	number := []platform.Field{{Name: "number", Label: "Number", Type: "text", Required: true}}
	live.accept(t, "build.contract.create", build.ContractType, "CALL", map[string]any{
		"name": "telephony.call", "major": 1, "title": "Phone calls", "description": "Place calls and read the call log",
		"actions": []runtimeprotocol.ContractAction{{Name: "dial", Title: "Dial", Payload: number}},
		"events":  []platform.ProtocolEvent{{Name: "answered", Title: "Answered"}},
	})
	live.accept(t, "build.contract.publish", build.ContractType, "CALL", map[string]any{})
	call, listed := protocols(live.Tenant)["telephony.call/1"]
	if !listed || !call.Declared || call.Title != "Phone calls" || len(call.Providers) != 0 || !slices.Equal(call.Actions, []string{"dial"}) || call.Contract == nil || len(call.Contract.Events) != 1 {
		t.Fatalf("a contract installs and lists without any implementation: %+v", call)
	}
	if _, bound := live.router().Resolve("telephony.call/1"); bound {
		t.Fatal("a contract without an implementation has nothing to resolve")
	}

	object := invariantObject("phone", "Phone")
	object["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.call", Version: 1, Actions: map[string]string{"dial": "edit"}}}
	refused := try("build.object.create", build.ObjectType, "PHONE", object)
	if refused == nil {
		refused = try("build.object.publish", build.ObjectType, "PHONE", map[string]any{})
	}
	if refused == nil {
		t.Fatal("an implementation whose action parameters differ from the contract must be refused")
	}
	if len(protocols(live.Tenant)["telephony.call/1"].Providers) != 0 {
		t.Fatal("a refused implementation must not provide the contract")
	}

	fax := invariantObject("fax", "Fax")
	fax["protocols"] = []runtimeprotocol.ProtocolBinding{{Name: "telephony.fax", Version: 1, Actions: map[string]string{"send": "edit"}}}
	live.accept(t, "build.object.create", build.ObjectType, "FAX", fax)
	live.accept(t, "build.object.publish", build.ObjectType, "FAX", map[string]any{})
	provider, ok := live.router().Resolve("telephony.fax/1")
	if !ok || len(provider.Provision.Protocol.Actions) != 1 {
		t.Fatalf("the fax object provides its protocol: %+v", provider)
	}
	implemented := provider.Provision.Protocol.Actions[0].Payload
	contract := func(payload []platform.Field) map[string]any {
		return map[string]any{"name": "telephony.fax", "major": 1, "title": "Fax", "actions": []runtimeprotocol.ContractAction{{Name: "send", Title: "Send", Payload: payload}}}
	}
	live.accept(t, "build.contract.create", build.ContractType, "FAX", contract(append(slices.Clone(implemented), number...)))
	if try("build.contract.publish", build.ContractType, "FAX", map[string]any{}) == nil {
		t.Fatal("a contract that an installed implementation does not match must be refused")
	}
	live.accept(t, "build.contract.edit", build.ContractType, "FAX", contract(implemented))
	live.accept(t, "build.contract.publish", build.ContractType, "FAX", map[string]any{})
	info := protocols(live.Tenant)["telephony.fax/1"]
	if !info.Declared || info.Title != "Fax" || !slices.Equal(info.Providers, []string{build.ID}) {
		t.Fatalf("the contract lists its implementation: %+v", info)
	}

	live.accept(t, "build.contract.edit", build.ContractType, "FAX", map[string]any{"title": "Fax machines"})
	live.accept(t, "build.contract.publish", build.ContractType, "FAX", map[string]any{})
	for name, change := range map[string]map[string]any{
		"changing the parameters": contract(append(slices.Clone(implemented), number...)),
		"changing the major":      {"major": 2},
		"adding a read":           {"reads": []string{"log"}},
	} {
		if try("build.contract.edit", build.ContractType, "FAX", change) == nil && try("build.contract.publish", build.ContractType, "FAX", map[string]any{}) == nil {
			t.Fatalf("%s of a published contract must be refused", name)
		}
		live.accept(t, "build.contract.edit", build.ContractType, "FAX", contract(implemented))
	}
	if protocols(live.Tenant)["telephony.fax/1"].Title != "Fax machines" {
		t.Fatal("a published contract may change its words")
	}

	before, listedBefore := live.recordImage(t), protocols(live.Tenant)
	replayed := live.restore(t, live.entries)
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the contract records")
	}
	if listedAfter := protocols(replayed.Tenant); !reflect.DeepEqual(listedBefore, listedAfter) {
		t.Fatalf("replay must install the same contracts: %+v != %+v", listedBefore, listedAfter)
	}
}
