package hospitality

import (
	"slices"
	"testing"

	"platformserver/platform"
)

func TestCRMUsesInstalledDefinitions(t *testing.T) {
	w := newWorld(t, hotelProvider)
	defs := w.tenant.Definitions(w.members["sales"])
	object := platform.AssetRef{App: "crm", Kind: platform.AssetObject, Name: "crm.opportunity"}
	action := platform.AssetRef{App: "crm", Kind: platform.AssetAction, Name: "crm.opportunity.open"}
	var objectFound, actionFound bool
	for _, def := range defs {
		if def.Ref == object {
			objectFound = def.Entity != nil
			if objectFound && slices.ContainsFunc(def.Entity.Fields, func(f platform.FieldInfo) bool { return f.Name == "margin" }) {
				t.Fatal("sales discovered a manager-only field through the definition registry")
			}
		}
		if def.Ref == action {
			actionFound = def.Action != nil && slices.Contains(def.Requires, object)
		}
	}
	if !objectFound || !actionFound {
		t.Fatalf("CRM object/action did not share the installed registry: object %t action %t", objectFound, actionFound)
	}
	manager := w.tenant.Definitions(w.members["manager"])
	var managerMargin bool
	for _, def := range manager {
		if def.Ref == object && def.Entity != nil {
			managerMargin = slices.ContainsFunc(def.Entity.Fields, func(f platform.FieldInfo) bool { return f.Name == "margin" })
		}
	}
	if !managerMargin {
		t.Fatal("manager's allowed field is absent from the definition registry")
	}
}
