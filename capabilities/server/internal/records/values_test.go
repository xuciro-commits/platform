package records

import (
	"bytes"
	"platformserver/platform"
	"reflect"
	"testing"
)

func TestEmptyListsPreservesFrozenOwnerDescriptors(t *testing.T) {
	descriptor := platform.ReleaseAsset{Ref: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.confirmation"}, ContractVersion: 1, SourceVersion: "1", Provides: []platform.Capability{{Kind: "interface", Name: "build.confirmation", Side: platform.CapabilityPure}}, Body: platform.Raw(map[string]any{"type": "build.confirmation"})}
	before := platform.Raw(descriptor)
	record := struct {
		Items  []string                `json:"items"`
		Assets []platform.ReleaseAsset `json:"assets"`
	}{Assets: []platform.ReleaseAsset{descriptor}}
	EmptyLists(reflect.ValueOf(&record).Elem())
	if record.Items == nil {
		t.Fatal("ordinary record lists lost their display default")
	}
	if !bytes.Equal(before, platform.Raw(record.Assets[0])) {
		t.Fatal("record display defaults changed an immutable owner descriptor")
	}
}
