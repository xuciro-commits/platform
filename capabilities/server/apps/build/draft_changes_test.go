package build

import "platformserver/internal/construction"

import (
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestDraftComparisonUsesOriginalPortableOwners(t *testing.T) {
	b := New("draft-comparison")
	dataset := construction.Dataset{Name: "samples", Title: "Samples", Keep: 3}
	asset, err := b.integrationAsset(platform.AssetDataset, dataset, 1)
	if err != nil {
		t.Fatal(err)
	}
	before, after, portable, err := b.draftComparison(dataset, string(platform.Raw(asset)), platform.EntityInfo{})
	if err != nil || !portable || !reflect.DeepEqual(before, after) {
		t.Fatalf("unchanged portable dataset: %v %v", portable, err)
	}
	dataset.Title = "Updated samples"
	before, after, _, err = b.draftComparison(dataset, string(platform.Raw(asset)), platform.EntityInfo{})
	if err != nil || string(before["title"]) != `"Samples"` || string(after["title"]) != `"Updated samples"` {
		t.Fatalf("portable dataset title difference: %v", err)
	}
	alert := construction.Alert{Name: "late", Title: "Late", Object: "build.visit", Field: "note", Operator: "=", Value: "late", Message: "Review visit", Role: "user", Active: true}
	asset, err = alertReleaseAsset(alert, 1)
	if err != nil {
		t.Fatal(err)
	}
	before, after, portable, err = b.draftComparison(alert, string(platform.Raw(asset)), platform.EntityInfo{})
	if err != nil || !portable || !reflect.DeepEqual(before, after) {
		t.Fatalf("unchanged portable alert: %v %v", portable, err)
	}
}
