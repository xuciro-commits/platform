package host

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	trialowner "platformserver/services/devops/trials"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK40ConnectorImplementationsMatchTheirContract(t *testing.T) {
	const contract = "telephony.call/1"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: "invariant-k40-connector", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, "invariant-k40-connector", admin)
	try := func(schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, id, payload), invariantClock)
		return refusal
	}
	listed := func(tenant *Tenant) actions.ProtocolInfo {
		for _, info := range tenant.router().Protocols() {
			if info.ID == contract {
				return info
			}
		}
		return actions.ProtocolInfo{}
	}
	connector := func(change map[string]any) map[string]any {
		out := map[string]any{"name": "cloudtalk", "title": "CloudTalk", "contract": contract, "kind": "connector",
			"origins": []string{"https://api.cloudtalk.example"}, "credential": "bearer",
			"requests": []runtimeprotocol.ImplementationRequest{{Action: "dial", Method: "POST", Path: "/calls/{number}"}}}
		maps.Copy(out, change)
		return out
	}
	refusedFor := func(why string, want string) {
		t.Helper()
		refused := try("build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})
		if refused == nil || !strings.Contains(refused.Message, want) {
			t.Fatalf("%s must be refused by name (%q): %+v", why, want, refused)
		}
	}

	live.accept(t, "build.implementation.create", build.ImplementationType, "CLOUDTALK", connector(nil))
	refusedFor("a connector for a contract that is not installed", "not an installed capability contract")

	number := []platform.Field{{Name: "number", Label: "Number", Type: "text", Required: true}}
	live.accept(t, "build.contract.create", build.ContractType, "CALL", map[string]any{"name": "telephony.call", "major": 1, "title": "Phone calls",
		"actions": []runtimeprotocol.ContractAction{{Name: "dial", Title: "Dial", Payload: number}}})
	live.accept(t, "build.contract.publish", build.ContractType, "CALL", map[string]any{})

	for _, bad := range []struct {
		why    string
		change map[string]any
		want   string
		field  bool
	}{
		{"a connector without a request for a contract action", map[string]any{"requests": []runtimeprotocol.ImplementationRequest{{Action: "hangup", Method: "POST", Path: "/hangup"}}}, "no request for action dial", false},
		{"a path naming something that is not a parameter", map[string]any{"requests": []runtimeprotocol.ImplementationRequest{{Action: "dial", Method: "POST", Path: "/calls/{who}"}}}, "not a parameter of action dial", false},
		{"an unclosed path placeholder", map[string]any{"requests": []runtimeprotocol.ImplementationRequest{{Action: "dial", Method: "POST", Path: "/calls/{number"}}}, "malformed parameter placeholder", false},
		{"an origin with a path", map[string]any{"origins": []string{"https://api.cloudtalk.example/v1"}}, "not an exact https origin", false},
		{"a plain http origin", map[string]any{"origins": []string{"http://api.cloudtalk.example"}}, "not an exact https origin", false},
		{"a kind other than connector", map[string]any{"kind": "wasm"}, "must be a connector", true},
		{"a password credential", map[string]any{"credential": "password"}, "api-key or a bearer credential", true},
	} {
		if refused := try("build.implementation.edit", build.ImplementationType, "CLOUDTALK", connector(bad.change)); refused != nil {
			if !bad.field {
				t.Fatalf("%s is refused at publish, not when the draft is saved: %+v", bad.why, refused)
			}
			continue
		}
		refusedFor(bad.why, bad.want)
	}

	live.accept(t, "build.implementation.edit", build.ImplementationType, "CLOUDTALK", connector(nil))
	live.accept(t, "build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})
	info := listed(live.Tenant)
	if len(info.Implementations) != 1 || info.Implementations[0].ID != build.ID+":cloudtalk" || info.Implementations[0].Kind != "connector" ||
		len(info.Implementations[0].Origins) != 1 || len(info.Providers) != 0 {
		t.Fatalf("the contract lists the connector under its own identity, apart from app providers: %+v", info)
	}

	bind := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": build.ID + ":cloudtalk"})
	bind.PrincipalId, bind.Authority = "admin", controlowner.PlatformApp
	member, _ := live.Member("admin")
	if _, refusal := live.Submit(member, bind, invariantClock); refusal == nil || !strings.Contains(refusal.Message, "conformance tests") {
		t.Fatalf("a connector that has not passed the conformance tests must not be bound: %+v", refusal)
	}
	if _, bound := live.router().Resolve(contract); bound {
		t.Fatal("a refused binding leaves the contract unbound")
	}

	live.accept(t, "build.implementation.edit", build.ImplementationType, "CLOUDTALK", connector(map[string]any{"contract": "telephony.fax/1"}))
	if try("build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{}) == nil {
		t.Fatal("a published implementation keeps the contract it implements")
	}
	live.accept(t, "build.implementation.edit", build.ImplementationType, "CLOUDTALK", connector(map[string]any{"title": "CloudTalk voice", "origins": []string{"https://api.cloudtalk.example", "https://eu.cloudtalk.example"}}))
	live.accept(t, "build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})
	if got := listed(live.Tenant).Implementations; len(got) != 1 || got[0].Title != "CloudTalk voice" || len(got[0].Origins) != 2 {
		t.Fatalf("a published implementation may change its words and origins: %+v", got)
	}

	before, listedBefore := live.recordImage(t), listed(live.Tenant)
	replayed := live.restore(t, live.entries, admin)
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the implementation records")
	}
	if listedAfter := listed(replayed.Tenant); !reflect.DeepEqual(listedBefore, listedAfter) {
		t.Fatalf("replay must install the same implementations: %+v != %+v", listedBefore, listedAfter)
	}

	root := platform.AssetRef{App: build.ID, Kind: runtimeprotocol.AssetImplementation, Name: "cloudtalk"}
	contractRef := platform.AssetRef{App: build.ID, Kind: runtimeprotocol.AssetContract, Name: "telephony.call"}
	assets, err := live.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	assets = slices.DeleteFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref != root && asset.Ref != contractRef })
	candidate, err := PublicationResolver().Candidate([]platform.AssetRef{root}, assets)
	if err != nil || !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == contractRef }) {
		t.Fatalf("the connector closes over its contract: %v", err)
	}
	environment, err := trialowner.PrepareEnvironment(live.apps, live.simulations, actions.NewAuthority, candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := candidateTestTenantWithEnvironment(candidate, live.member(t), simulationEnvironment{Environment: environment, Services: live.services})
	if err != nil {
		t.Fatal(err)
	}
	if got := listed(sandbox).Implementations; len(got) != 1 || got[0].ID != runtimeprotocol.ImplementationIdentity(root.App, root.Name) {
		t.Fatalf("the candidate sandbox must load the connector: %+v", got)
	}
}
