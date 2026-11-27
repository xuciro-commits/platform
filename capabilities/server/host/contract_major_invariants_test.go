package host

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK51ContractMajorVersionsDoNotCrossRoute(t *testing.T) {
	const tenant = "invariant-k51"
	admin := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	bind := func(major int, provider string) *platform.Error {
		t.Helper()
		contract := fmt.Sprintf("sample.post/%d", major)
		sub := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": provider})
		sub.PrincipalId, sub.Authority = "admin", controlowner.PlatformApp
		member, _ := live.Member("admin")
		_, refusal := live.Submit(member, sub, invariantClock)
		return refusal
	}
	for _, major := range []int{1, 2} {
		name := fmt.Sprintf("provider%d", major)
		id := fmt.Sprintf("PROVIDER%d", major)
		object := invariantObject(name, name)
		object["protocols"] = []protocol.ProtocolBinding{{Name: "sample.post", Version: major, Actions: map[string]string{"post": "edit"}}}
		live.accept(t, "build.object.create", build.ObjectType, id, object)
		live.accept(t, "build.object.publish", build.ObjectType, id, map[string]any{})
		contract := fmt.Sprintf("sample.post/%d", major)
		providers := live.router().Providers(contract)
		if len(providers) != 1 || len(providers[0].Provision.Protocol.Actions) != 1 {
			t.Fatalf("provider %s: %+v", contract, providers)
		}
		live.accept(t, "build.contract.create", build.ContractType, id, map[string]any{"name": "sample.post", "major": major, "title": name, "actions": []protocol.ContractAction{{Name: "post", Title: "Post", Payload: providers[0].Provision.Protocol.Actions[0].Payload}}})
		live.accept(t, "build.contract.publish", build.ContractType, id, map[string]any{})
	}
	for _, major := range []int{1, 2} {
		contract := fmt.Sprintf("sample.post/%d", major)
		providers := live.router().Providers(contract)
		if len(providers) != 1 || providers[0].EntityType() != fmt.Sprintf("invariantproject.provider%d", major) {
			t.Fatalf("cross-major discovery %s: %+v", contract, providers)
		}
		wrong := build.ID + "@" + live.router().Providers(fmt.Sprintf("sample.post/%d", 3-major))[0].EntityType()
		if refusal := bind(major, wrong); refusal == nil {
			t.Fatalf("cross-major binding %s to %s was accepted", contract, wrong)
		}
		if refusal := bind(major, providers[0].ID()); refusal != nil {
			t.Fatalf("binding %s: %s", contract, refusal.Message)
		}
	}
	for _, major := range []int{1, 2} {
		contract := fmt.Sprintf("sample.post/%d", major)
		sub, ok := live.router().Submission("builder", contract, "post", "R1", []byte("{}"), contract)
		if !ok || sub.GetTarget().GetType() != fmt.Sprintf("invariantproject.provider%d", major) {
			t.Fatalf("incorrect route %s: %+v", contract, sub)
		}
	}

	assets, err := live.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	roots := []platform.AssetRef{
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.post"},
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.post@2"},
		{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.provider1"},
		{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.provider2"},
	}
	for _, ref := range roots {
		if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
			t.Fatalf("missing release asset %s", ref)
		}
	}
	candidate, err := PublicationResolver().Candidate(roots, assets)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := platform.PackageDescriptor{ID: "versioned", Version: "1.0.0", Namespace: "versioned", Title: "Versioned contracts", Blueprint: &platform.PackageBlueprint{Roots: roots, Assets: candidate.Assets}}
	target := newInvariantTenant(t, "invariant-k51-target", admin)
	member, _ := target.Member("admin")
	offer, err := target.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(descriptor)})
	if err != nil || !offer.Precheck.OK {
		t.Fatalf("inspect two-major package: %+v %v", offer.Precheck, err)
	}
	for _, entry := range []struct {
		schema  string
		payload map[string]any
	}{
		{packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": offer.Digest}},
		{packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version, "namespace": descriptor.Namespace}},
	} {
		sub := target.submission(entry.schema, protocol.PackageType, descriptor.ID, entry.payload)
		sub.PrincipalId, sub.Authority = member.ID, "platform"
		if _, refusal := target.Submit(member, sub, invariantClock); refusal != nil {
			t.Fatalf("%s: %s", entry.schema, refusal.Message)
		}
	}
	for _, major := range []int{1, 2} {
		id := fmt.Sprintf("sample.post/%d", major)
		if !target.router().Declared(id) || len(target.router().Providers(id)) != 1 {
			t.Fatalf("package lost contract %s", id)
		}
	}

	for _, major := range []int{1, 2} {
		typ := fmt.Sprintf("invariantproject.provider%d", major)
		live.accept(t, typ+".create", typ, "ROW1", map[string]any{"note": "initial"})
	}
	for _, major := range []int{1, 2} {
		contract := fmt.Sprintf("sample.post/%d", major)
		typ := fmt.Sprintf("invariantproject.provider%d", major)
		target, _, refusal := live.Invoke(live.member(t), contract, "post", "ROW1", platform.Raw(map[string]any{"note": contract}), "k51-call-"+contract, invariantClock)
		if refusal != nil || target.GetType() != typ {
			t.Fatalf("invoke %s: %v %v", contract, target, refusal)
		}
	}
	for _, major := range []int{1, 2} {
		contract := fmt.Sprintf("sample.post/%d", major)
		typ := fmt.Sprintf("invariantproject.provider%d", major)
		view, refusal := live.RecordOf(t.Context(), live.member(t), typ, "ROW1", invariantClock)
		if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), contract) {
			t.Fatalf("wrong accepted record for %s: %v %v", contract, view, refusal)
		}
	}
	before := live.router().Protocols()
	replayed := live.restore(t, live.entries, admin)
	if !reflect.DeepEqual(before, replayed.router().Protocols()) {
		t.Fatal("replay changed versioned contract bindings")
	}
	for _, major := range []int{1, 2} {
		contract := fmt.Sprintf("sample.post/%d", major)
		bound, ok := replayed.router().Bound(contract)
		if !ok || bound.EntityType() != fmt.Sprintf("invariantproject.provider%d", major) {
			t.Fatalf("wrong binding after replay: %s %+v", contract, bound)
		}
	}
}
