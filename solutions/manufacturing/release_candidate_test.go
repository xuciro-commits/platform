package manufacturing

import (
	"slices"
	"testing"

	"erp"
	"mes"
	"platformserver/platform"
)

func TestPlantCodePageReleaseCandidate(t *testing.T) {
	tenant, err := NewTenant("release-plant", erp.New("release-plant"))
	if err != nil {
		t.Fatal(err)
	}
	root := platform.AssetRef{App: mes.ID, Kind: platform.AssetPage, Name: "shop-orders"}
	candidate, err := tenant.ReleaseCandidate([]platform.AssetRef{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	for _, required := range []platform.AssetRef{
		{App: mes.ID, Kind: platform.AssetObject, Name: mes.OrderType},
		{App: mes.ID, Kind: platform.AssetAction, Name: mes.SchemaRelease},
	} {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == required }) {
			t.Errorf("plant release omits %s", required)
		}
	}
}
