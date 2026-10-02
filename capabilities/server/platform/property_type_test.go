package platform

import "testing"

func TestSharedPropertyKeepsScalarMeaningAndExactVersion(t *testing.T) {
	p := PropertyType{Name: "quantity", Title: "Quantity", Description: "A measured quantity", Type: "integer"}
	if err := p.CheckField(FieldInfo{Name: "received", Title: "Quantity", Type: "integer", Required: true, Read: []string{"manager"}}); err != nil {
		t.Fatal(err)
	}
	if p.CheckField(FieldInfo{Name: "received", Title: "Quantity", Type: "decimal"}) == nil {
		t.Fatal("local type override accepted")
	}
	if p.CheckField(FieldInfo{Name: "received", Title: "Different", Type: "integer"}) == nil {
		t.Fatal("local meaning override accepted")
	}
	for _, typ := range []string{"reference", "choice", "money", "lines", "code"} {
		bad := p
		bad.Type = typ
		if bad.Check() == nil {
			t.Fatal("unsupported profile accepted", typ)
		}
	}
	d := Definition{Ref: AssetRef{App: "sample", Kind: AssetPropertyType, Name: "quantity"}, Version: "2", PropertyType: &p, PropertyVersions: map[string]PropertyType{"1": p}}
	if d.PropertyVersion("1") == nil || d.PropertyVersion("3") != nil {
		t.Fatal("version lookup broadened")
	}
}
