package build

import (
	"encoding/json"
	"os"
	"platformserver/platform"
	"testing"
)

func TestCompleteDefaultMaintenanceUsesTheOriginalPageDescriptor(t *testing.T) {
	raw, err := os.ReadFile("testdata/default-maintenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Page
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	page := descriptor(p)
	if len(page.Sections) != 21 || len(page.Document.Overlays) != 4 || len(page.Document.UnusedWidgets) != 2 {
		t.Fatal("complete source instances were lost")
	}
	if err := page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	if err := page.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := page.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	asset, err := platform.PageReleaseAsset(ID, "1.page-1", page)
	if err != nil {
		t.Fatal(err)
	}
	bound := false
	for _, ref := range asset.Requires {
		bound = bound || ref.Kind == platform.AssetCompute && ref.Name == "failure-risk"
	}
	if !bound {
		t.Fatal("candidate lost its computation")
	}
	if len(page.ComputeResources()) != 1 {
		t.Fatal("risk calculation was duplicated or discarded")
	}
}
