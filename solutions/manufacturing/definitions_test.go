package manufacturing

import (
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
	var objectFound, actionFound bool
	for _, def := range defs {
		if def.Ref == object {
			objectFound = def.Entity != nil
		}
		if def.Ref == action {
			actionFound = def.Action != nil && len(def.Requires) == 1 && def.Requires[0] == object
		}
	}
	if !objectFound || !actionFound {
		t.Fatalf("MES object/action did not share the installed registry: object %t action %t", objectFound, actionFound)
	}
}
