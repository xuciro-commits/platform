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

func TestEmptyListsPreservesOptionalNilAndExplicitEmptySlices(t *testing.T) {
	value := struct {
		Required []string `json:"required"`
		Missing  []string `json:"missing,omitzero"`
		Empty    []string `json:"empty,omitzero"`
	}{Empty: []string{}}
	EmptyLists(reflect.ValueOf(&value).Elem())
	if value.Required == nil || value.Missing != nil || value.Empty == nil || !bytes.Equal(platform.Raw(value), []byte(`{"required":[],"empty":[]}`)) {
		t.Fatal("display defaults changed a declared omission or explicit empty slice", string(platform.Raw(value)))
	}
}
