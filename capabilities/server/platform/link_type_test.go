package platform

import (
	"testing"
)

func TestReferenceLinkProfileAndReleaseDependencies(t *testing.T) {
	parent := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.parent"}
	child := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.child"}
	l := LinkType{Name: "children", Title: "Children", Description: "Child references its parent", Parent: parent, Child: child, Via: "parent", Forward: "children", Reverse: "parent", Required: true, Storage: "reference", Cardinality: "one-to-many", DeletePolicy: "owner"}
	parentInfo := EntityInfo{App: "sample", Type: parent.Name}
	childInfo := EntityInfo{App: "sample", Type: child.Name, Fields: []FieldInfo{{Name: "parent", Type: "reference", Ref: parent.Name, Required: true}}}
	if err := l.CheckSchema(parentInfo, childInfo); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*LinkType){func(l *LinkType) { l.Cardinality = "one-to-one" }, func(l *LinkType) { l.DeletePolicy = "cascade" }, func(l *LinkType) { l.Required = false }, func(l *LinkType) { l.Parent.App = "foreign" }, func(l *LinkType) { l.Via = "missing" }} {
		bad := l
		change(&bad)
		if bad.CheckSchema(parentInfo, childInfo) == nil {
			t.Fatal("unsupported relationship guarantee accepted")
		}
	}
	asset := ReleaseAsset{Ref: AssetRef{App: "sample", Kind: AssetLinkType, Name: l.Name}, SourceVersion: "1", ContractVersion: 1, Requires: []AssetRef{parent, child}, Body: Raw(l)}
	if _, err := Candidate([]AssetRef{asset.Ref}, []ReleaseAsset{asset}); err == nil {
		t.Fatal("relationship candidate accepted missing object dependencies")
	}
	childInfo.Fields[0].Type = "references"
	if l.CheckSchema(parentInfo, childInfo) == nil {
		t.Fatal("scalar relationship accepted multivalued storage")
	}
}
