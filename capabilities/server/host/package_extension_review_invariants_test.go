package host

import (
	"slices"
	"strings"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	releaseowner "platformserver/services/devops/releases"
)

func TestInvariantK9PackageExtensionReviewListsProjectsAndVersionRanges(t *testing.T) {
	baseRef := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "crm.lead"}
	removedRef := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "crm.old"}
	base := platform.RecordTypeInfo{App: "build", Type: baseRef.Name, Fields: []platform.FieldInfo{{Name: "note", Type: "text"}}}
	old := platform.RecordTypeInfo{App: "build", Type: removedRef.Name, Fields: base.Fields}
	extension := func(name string, ref platform.AssetRef) platform.RecordTypeInfo {
		return platform.RecordTypeInfo{App: "build", Type: name, Extends: ref.Name, Fields: []platform.FieldInfo{{Name: "base", Type: "reference", Ref: ref.Name, Required: true}, {Name: "customerfield", Type: "text"}}, ExtensionSource: &platform.ExtensionSource{Base: platform.AssetBinding{Ref: ref, SourceVersion: "v1"}, Fields: base.Fields}}
	}
	entities := []platform.RecordTypeInfo{extension("local.extra", baseRef), extension("customer.compatible", baseRef), extension("customer.incompatible", baseRef), extension("customer.removed", removedRef)}
	resources := []protocol.Resource{{Ref: baseRef, ManagedBy: "crm"}, {Ref: removedRef, ManagedBy: "crm"}}
	for _, e := range entities {
		pkg := strings.TrimPrefix(e.Type, "customer.")
		if pkg == e.Type {
			pkg = ""
		}
		resources = append(resources, protocol.Resource{Ref: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: e.Type}, Project: e.Type + "-project", ManagedBy: pkg})
	}
	after := []platform.ReleaseAsset{{Ref: baseRef, SourceVersion: "v2"}}
	impact, err := (packages.Impact{Resources: resources, Records: func(string) (int, bool) { return 0, true }, Removals: func(map[platform.AssetRef]bool, map[platform.AssetRef]bool) []platform.PackageImpactReference {
		return nil
	}, Extensions: func(candidate protocol.ReleaseCandidate) ([]platform.PackageImpactExtension, error) {
		return releaseowner.ExtensionReview(candidate, entities, map[string]platform.RecordTypeInfo{base.Type: base}, func(typ string) (platform.RecordTypeInfo, bool) { return old, typ == old.Type }, func(string) (platform.AssetBinding, bool) { return platform.AssetBinding{}, false })
	}}).Of("crm", after)
	if err != nil {
		t.Fatal(err)
	}
	installed := func(id, version, req string) protocol.PackageView {
		return protocol.PackageView{Descriptor: platform.PackageDescriptor{ID: id, Version: version, Requires: []string{req}}, Installed: &protocol.InstalledPackage{ID: id, Version: "1.0.0", State: "active"}}
	}
	chosen := protocol.PackageView{Descriptor: platform.PackageDescriptor{ID: "crm", Version: "2.0.0"}}
	views := []protocol.PackageView{chosen, installed("compatible", "1.0.0", "crm@^2.0.0"), installed("incompatible", "2.0.0", "crm@^2.0.0"), installed("incompatible", "1.0.0", "crm@^1.0.0")}
	review, err := (packages.Review{Impact: func(string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) { return impact, nil }}).Of(views, &chosen, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Extensions) != 4 || !slices.EqualFunc(review.Extensions, review.Impact.Extensions, func(a, b platform.PackageImpactExtension) bool {
		return a.Type == b.Type && a.Issue == b.Issue && a.Project == b.Project && a.VersionRange == b.VersionRange
	}) {
		t.Fatalf("missing extension impact: %+v", review)
	}
	for _, row := range review.Extensions {
		if row.Project != row.Type+"-project" {
			t.Fatalf("wrong project: %+v", row)
		}
		switch row.Type {
		case "local.extra":
			if row.Issue != "" || row.VersionRange != "" || row.Base.SourceVersion != "v1" {
				t.Fatalf("local shape compatibility: %+v", row)
			}
		case "customer.compatible":
			if row.Issue != "" || row.VersionRange != "^2.0.0" {
				t.Fatalf("compatible extension: %+v", row)
			}
		case "customer.incompatible":
			if !strings.Contains(row.Issue, row.Type) || row.VersionRange != "^1.0.0" {
				t.Fatalf("incompatible extension: %+v", row)
			}
		case "customer.removed":
			if !strings.Contains(row.Issue, removedRef.Name) || row.TargetVersion != "" {
				t.Fatalf("removed base: %+v", row)
			}
		}
	}
	if len(review.Problems) != 2 {
		t.Fatalf("expected two named blockers: %+v", review.Problems)
	}
}
