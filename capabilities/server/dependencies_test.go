package platformserver

import (
	"slices"
	"testing"

	"platformserver/platform"
)

func TestDependencyDiscoveryUsesTheAuthorizedOwnerDescription(t *testing.T) {
	tn := stockTenant(t)
	console := tn.app(PlatformApp).(*Console)
	clerk, _ := console.MemberBySubject("ana")
	operator, _ := console.MemberBySubject("op")
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	create := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"}
	count := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}
	view, refusal := tn.DependencyView(clerk, object, "")
	if refusal != nil {
		t.Fatal(refusal)
	}
	if !slices.Contains(view.UsedBy, create) || slices.Contains(view.UsedBy, count) {
		t.Fatalf("impact did not preserve action authorization: %+v", view.UsedBy)
	}
	for _, descriptor := range view.Descriptors {
		for _, dependency := range descriptor.Requires {
			if !slices.ContainsFunc(view.Descriptors, func(d platform.Dependencies) bool { return d.Ref == dependency }) {
				t.Fatalf("discovery exposed an unreadable dependency: %s -> %s", descriptor.Ref, dependency)
			}
		}
	}
	provided, refusal := tn.ProvidedCapabilities(clerk, "action", platform.CapabilityAuthoritative)
	if refusal != nil {
		t.Fatal(refusal)
	}
	if !slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == create }) || slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == count }) {
		t.Fatalf("clerk capability discovery widened its original catalog: %+v", provided)
	}
	provided, refusal = tn.ProvidedCapabilities(operator, "action", "")
	if refusal != nil || !slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == count }) || slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == create }) {
		t.Fatalf("operator capability discovery: %+v %v", provided, refusal)
	}
	if _, refusal := tn.DependencyView(clerk, count, ""); refusal == nil {
		t.Fatal("hidden action graph was exposed")
	}
}

func TestDependencyDiscoveryTracksTheSameDescriptorChange(t *testing.T) {
	tn := stockTenant(t)
	member, _ := tn.app(PlatformApp).(*Console).MemberBySubject("ana")
	create := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"}
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	for i := range tn.definitions {
		if tn.definitions[i].Ref == create {
			tn.definitions[i].Requires = nil
		}
	}
	view, refusal := tn.DependencyView(member, create, "")
	if refusal != nil || slices.Contains(view.Requires, object) {
		t.Fatalf("forward view retained an independent list: %+v %v", view, refusal)
	}
	view, refusal = tn.DependencyView(member, object, "")
	if refusal != nil || slices.Contains(view.UsedBy, create) {
		t.Fatalf("reverse view retained an independent list: %+v %v", view, refusal)
	}
}
