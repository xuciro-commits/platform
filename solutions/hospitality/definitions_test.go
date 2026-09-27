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
	page := platform.AssetRef{App: "crm", Kind: platform.AssetPage, Name: "opportunities"}
	var objectFound, actionFound, pageFound bool
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
		if def.Ref == page {
			pageFound = def.Page != nil && def.Page.Object == object && !slices.Contains(def.Page.DetailFields, "margin") && slices.Contains(def.Page.Actions, action)
		}
	}
	if !objectFound || !actionFound || !pageFound {
		t.Fatalf("CRM object/action/page did not share the installed registry: object %t action %t page %t", objectFound, actionFound, pageFound)
	}
	manager := w.tenant.Definitions(w.members["manager"])
	var managerMargin, managerPageMargin bool
	for _, def := range manager {
		if def.Ref == object && def.Entity != nil {
			managerMargin = slices.ContainsFunc(def.Entity.Fields, func(f platform.FieldInfo) bool { return f.Name == "margin" })
		}
		if def.Ref == page && def.Page != nil {
			managerPageMargin = slices.Contains(def.Page.DetailFields, "margin")
		}
	}
	if !managerMargin || !managerPageMargin {
		t.Fatal("manager's allowed field is absent from the object or page definition")
	}
}
