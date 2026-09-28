package hospitality

import (
	"slices"
	"testing"

	"crm"
	"platformserver/platform"
)

func TestHotelCodePageReleaseCandidate(t *testing.T) {
	tenant, err := NewTenant("release-hotel", nil)
	if err != nil {
		t.Fatal(err)
	}
	root := platform.AssetRef{App: crm.ID, Kind: platform.AssetPage, Name: "opportunities"}
	candidate, err := tenant.ReleaseCandidate([]platform.AssetRef{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	for _, required := range []platform.AssetRef{
		{App: crm.ID, Kind: platform.AssetObject, Name: crm.OpportunityType},
		{App: crm.ID, Kind: platform.AssetAction, Name: crm.SchemaClose},
	} {
		if !slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == required }) {
			t.Errorf("hotel release omits %s", required)
		}
	}
}
