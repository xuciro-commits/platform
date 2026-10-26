package build

import "platformserver/internal/construction"

import (
	"reflect"
	"slices"
	"testing"

	"platformserver/platform"
)

func TestOwnedLinkPublicationPreservesStorageAndRoles(t *testing.T) {
	ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: "build.entry"}
	declaration := platform.LinkType{Name: "children", Title: "Children", Description: "Owned children", Parent: ref, Child: ref, Forward: "children", Reverse: "parent", Storage: "edge", Cardinality: "one-to-one", DeletePolicy: "restrict-active", Roles: []string{construction.Builder}}
	if err := declaration.Check(); err != nil {
		t.Fatal(err)
	}
	if got := linkTypeFromDeclaration(declaration).Definition(); !reflect.DeepEqual(got, declaration) {
		t.Fatalf("publication round trip changed the relation: %+v", got)
	}
	before := linkTypeFromDeclaration(declaration)
	after := before
	after.Storage, after.Via = "reference", "parent"
	if linkKeepsGuarantees(before, after) {
		t.Fatal("publication changed the relationship's storage identity")
	}
	after = before
	after.Cardinality = "many-to-many"
	if linkKeepsGuarantees(before, after) {
		t.Fatal("publication weakened cardinality")
	}
}

func TestRuleCollectionsBindTheCurrentDraftSets(t *testing.T) {
	b := New("sets")
	object := construction.Object{Name: "entry", Sets: []platform.RecordSet{{Name: "children", Object: "build.entry"}}}
	lookup := ruleLookup(object, nil)
	spec := platform.RuleReadSpec{Object: "build.entry", Collections: map[string]platform.RuleCollection{"children": {App: construction.ID, Set: "children", Object: "build.entry", Policy: "visible"}}}
	if err := b.checkRuleCollections(spec, "draft rule", lookup); err != nil {
		t.Fatal(err)
	}
	collection := spec.Collections["children"]
	collection.Set = "undeclared"
	spec.Collections["children"] = collection
	if err := b.checkRuleCollections(spec, "draft rule", lookup); err == nil {
		t.Fatal("undeclared set was accepted")
	}
}

func TestDeclaredSetReleaseRetainsItsSourceDependencies(t *testing.T) {
	binding := platform.AssetBinding{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetQuery, Name: "children"}, SourceVersion: "1.query-1"}
	object := construction.Object{Name: "parent", Title: "Parent", Sets: []platform.RecordSet{{Name: "children", From: "children", Binding: &binding, Object: "build.child", Limit: 10}}}
	object.ID = "parent"
	object.Published = published(object)
	assets, err := releaseAssets([]construction.Object{object}, nil, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref.Kind == platform.AssetObject })
	child := platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: "build.child"}
	if index < 0 || !slices.Contains(assets[index].Requires, binding.Ref) || !slices.Contains(assets[index].Requires, child) {
		t.Fatal("release omitted the set's retained source or element owner")
	}
	description, err := platform.DescribeAsset(assets[index])
	if err != nil || !slices.Equal(description.Requires, assets[index].Requires) {
		t.Fatal("release dependencies differ from the frozen declaration", description, err)
	}
}
