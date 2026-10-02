package platformserver

import (
	"platformserver/platform"
	"testing"
)

func TestMemberScalarProjectionRetainsExactNumbers(t *testing.T) {
	type row struct {
		Name     string
		Active   bool
		Count    int64
		Fraction float64
		Secret   bool
	}
	info := platform.EntityInfo{Fields: []platform.FieldInfo{{Name: "name", Type: "text", Index: []int{0}}, {Name: "active", Type: "boolean", Index: []int{1}}, {Name: "count", Type: "integer", Index: []int{2}}, {Name: "fraction", Type: "decimal", Index: []int{3}}}}
	values, errors := propertyValues(info, row{"note", false, 9007199254740993, 0.0000001, true})
	if len(errors) > 0 || values["active"] != false {
		t.Fatal(values, errors)
	}
	if values["count"].(platform.DecimalValue).Value != "9007199254740993" || values["fraction"].(platform.DecimalValue).Value != "0.0000001" {
		t.Fatal("numeric projection rounded")
	}
	if _, ok := values["secret"]; ok {
		t.Fatal("hidden descriptor leaked")
	}
}
