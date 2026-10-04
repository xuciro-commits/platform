package build

import (
	"encoding/json"
	"os"
	"platformserver/platform"
	"testing"
)

func TestCompleteDefaultMapRetainsSharedSelectionAndBusinessSchedule(t *testing.T) {
	raw, err := os.ReadFile("testdata/default-map.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Page
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	page := descriptor(p)
	if len(page.Sections) != 17 || len(page.Document.Overlays) != 4 || len(page.Document.UnusedWidgets) != 2 {
		t.Fatal("complete source layout was lost")
	}
	if err := page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	if err := page.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	if err := page.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	var shared string
	for _, s := range page.Sections {
		if s.Widget == "record-map" {
			shared = s.SelectionVariable
			if shared == "" || s.Selection != "" || page.Document.Variables[shared].Source.Variable != "record" {
				t.Fatal("map selection differs from original application")
			}
		}
	}
	if _, err := platform.PageReleaseAsset(ID, "1.page-1", page); err != nil {
		t.Fatal(err)
	}
	page.Document.UIProfile = "platform.page.v2.100"
	if page.Document.Check(page.Sections) == nil {
		t.Fatal("old profile admitted shared map output")
	}
	page.Document.UIProfile = platform.PageUIProfile()
	v := page.Document.Variables[shared]
	v.Writable = false
	page.Document.Variables[shared] = v
	if page.Document.Check(page.Sections) == nil {
		t.Fatal("readonly application record became a map writer")
	}
}
