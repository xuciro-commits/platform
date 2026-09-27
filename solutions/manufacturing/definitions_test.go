package manufacturing

import (
	"slices"
	"testing"

	"erp"
	"platformserver/platform"
)

func TestMESUsesInstalledDefinitions(t *testing.T) {
	tn, err := NewTenant(tenant, erp.New(tenant), seats...)
	if err != nil {
		t.Fatal(err)
	}
	defs := tn.Definitions(seats[0].Member)
	object := platform.AssetRef{App: "mes", Kind: platform.AssetObject, Name: "mes.order"}
	action := platform.AssetRef{App: "mes", Kind: platform.AssetAction, Name: "mes.order.release"}
	page := platform.AssetRef{App: "mes", Kind: platform.AssetPage, Name: "shop-orders"}
	var objectFound, actionFound, pageFound bool
	for _, def := range defs {
		if def.Ref == object {
			objectFound = def.Entity != nil
		}
		if def.Ref == action {
			actionFound = def.Action != nil && len(def.Requires) == 1 && def.Requires[0] == object
		}
		if def.Ref == page {
			pageFound = def.Page != nil && def.Page.Object == object && slices.Contains(def.Page.Actions, action)
		}
	}
	if !objectFound || !actionFound || !pageFound {
		t.Fatalf("MES object/action/page did not share the installed registry: object %t action %t page %t", objectFound, actionFound, pageFound)
	}
}
