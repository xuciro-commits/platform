package build

import (
	"encoding/json"
	"os"
	"platformserver/platform"
	"testing"
)

func TestCompleteDefaultWorkflowRetainsOriginalContextAndDependencies(t *testing.T) {
	raw, err := os.ReadFile("testdata/default-workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Page
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	page := descriptor(p)
	if len(page.Sections) != 21 || len(page.Document.Overlays) != 4 || len(page.Document.UnusedWidgets) != 2 {
		t.Fatal("complete source layout was lost")
	}
	if err = page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	if err = page.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	if err = page.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	asset, err := platform.PageReleaseAsset(ID, "1.page-1", page)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := asset.Requires
	for _, want := range []platform.AssetRef{{App: "build", Kind: platform.AssetFunction, Name: "asset-assistant"}, {App: "build", Kind: platform.AssetPage, Name: "fleet-kpis"}, {App: "build", Kind: platform.AssetLinkType, Name: "asset-sensor"}, {App: "relations", Kind: platform.AssetObject, Name: "platform.comment"}, {App: "files", Kind: platform.AssetAction, Name: "files.file.attach"}} {
		found := false
		for _, got := range dependencies {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing original dependency %v", want)
		}
	}
	for _, section := range page.Sections {
		if section.Widget != "vertex-graph" {
			continue
		}
		variable := page.Document.Variables[section.RecordVariable]
		original := variable.Source.Object
		wrong := *original
		wrong.App = "foreign"
		variable.Source.Object = &wrong
		if page.CheckRecordPorts() == nil {
			t.Fatal("neighborhood admitted a different original object owner")
		}
		variable.Source.Object = original
	}
	page.Document.UIProfile = "platform.page.v2.101"
	if page.Document.Check(page.Sections) == nil {
		t.Fatal("old profile admitted shared neighborhood input")
	}
}
