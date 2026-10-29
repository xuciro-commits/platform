package build

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestEnterpriseReferenceFieldsAndInputsKeepOriginalTypes(t *testing.T) {
	fields := []Field{{Name: "unit", Title: "Unit", Type: "reference", Ref: "enterprise.element", Stereotype: "ActualOrganization"}, {Name: "place", Title: "Place", Type: "reference", Ref: "enterprise.element", Stereotypes: []string{"ActualOrganization", "ActualLocation"}}}
	if err := CheckFields(fields, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	rule := platform.ExprReference("boolean", "active")
	object := Object{Name: "assignment", Title: "Assignment", Fields: append(fields, Field{Name: "active", Title: "Active", Type: "boolean"}), EnterpriseUse: &rule}
	info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	unit, _ := info.Field("unit")
	place, _ := info.Field("place")
	if unit.Stereotype != "ActualOrganization" || !slices.Equal(place.Stereotypes, fields[1].Stereotypes) || !reflect.DeepEqual(info.EnterpriseUse, &rule) {
		t.Fatalf("compiled enterprise metadata differs: %+v %+v", unit, place)
	}
	input := Input{Name: "place", Title: "Place", Type: "reference", Ref: "enterprise.element", Stereotypes: fields[1].Stereotypes}
	if got := input.Field(); got.Ref != input.Ref || !slices.Equal(got.Stereotypes, input.Stereotypes) {
		t.Fatalf("action input lost its enterprise restriction: %+v", got)
	}
	object.States = []State{{Name: "open", Title: "Open"}}
	object.Actions = []Action{{Name: "assign", Title: "Assign", From: []string{"open"}, To: "open", Inputs: []Input{input}}}
	if err := checkProcess(object, func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false }); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Field{{Name: "bad", Title: "Bad", Type: "text", Stereotype: "ActualPerson"}, {Name: "bad", Title: "Bad", Type: "reference", Ref: "enterprise.element", Stereotype: "not-a-type"}, {Name: "bad", Title: "Bad", Type: "reference", Ref: "enterprise.element", Inverse: "records"}} {
		if err := CheckFields([]Field{invalid}, nil); err == nil {
			t.Fatalf("invalid enterprise field accepted: %+v", invalid)
		}
	}
	historical := platform.Raw(Object{Name: "legacy", Fields: []Field{{Name: "name", Title: "Name", Type: "text"}}})
	for _, key := range []string{"enterpriseUse", "stereotype", "stereotypes"} {
		if strings.Contains(string(historical), key) {
			t.Fatalf("absent optional metadata changed old object bytes: %s", historical)
		}
	}
}

func TestEnterpriseUseRuleOnlyReadsCurrentRecord(t *testing.T) {
	object := Object{Name: "assignment", Fields: []Field{{Name: "active", Title: "Active", Type: "boolean"}}}
	valid := platform.ExprReference("boolean", "active")
	object.EnterpriseUse = &valid
	if err := CheckEnterpriseUse(object); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []platform.Expr{platform.ExprReference("text", "active"), platform.ExprReference("boolean", "input.active"), {Op: "path", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("reference", "other")}, Path: []string{"active"}}} {
		object.EnterpriseUse = &invalid
		if err := CheckEnterpriseUse(object); err == nil {
			t.Fatalf("nonlocal or nonboolean current-use rule accepted: %+v", invalid)
		}
	}
	// Serializing an authored rule keeps the original expression, not a second
	// execution closure or a hardcoded state-name convention.
	object.EnterpriseUse = &valid
	raw, err := definitionBody(object, "build.assignment")
	if err != nil {
		t.Fatal(err)
	}
	var saved Object
	if json.Unmarshal(raw, &saved) != nil || !reflect.DeepEqual(saved.EnterpriseUse, &valid) {
		t.Fatal("release descriptor lost the pure current-use rule")
	}
}
