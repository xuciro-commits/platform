package platform

import (
	"fmt"
	"testing"
)

func TestExternalFrameUsesReviewedOriginWithoutPlatformBindings(t *testing.T) {
	frame := PageExternalFrame{URL: "https://docs.example.com/report", Origin: "https://docs.example.com"}
	if !ValidPageExternalFrame(frame) {
		t.Fatal("valid external document refused")
	}
	for _, raw := range []string{"http://docs.example.com/report", "https://user:secret@docs.example.com/report", "https://docs.example.com.evil/report", "https://docs.example.com/../secret", "https://docs.example.com/%2e%2e/secret", "javascript:alert(1)", "https://docs.example.com/with space", "https://docs.example.com/\u4e2d"} {
		bad := frame
		bad.URL = raw
		if ValidPageExternalFrame(bad) {
			t.Fatal("ambiguous origin or URL accepted", raw)
		}
	}
	page := embeddedTestPage("external")
	page.Sections = []Section{{ID: "text", Widget: "external-frame", ConfigVersion: 1, ExternalFrame: &frame}}
	if err := page.Document.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	page.Sections[0].RecordVariable = "record"
	if page.Document.Check(page.Sections) == nil {
		t.Fatal("platform binding accepted by external document")
	}
}

func TestExternalDocumentsConsumeTheEmbeddingInstanceBudget(t *testing.T) {
	page := embeddedTestPage("frames")
	page.Sections = nil
	page.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows"}}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("frame%d", i)
		page.Sections = append(page.Sections, Section{ID: id, Widget: "external-frame", ConfigVersion: 1, ExternalFrame: &PageExternalFrame{URL: "https://docs.example.com/report", Origin: "https://docs.example.com"}})
		root := page.Document.Nodes["root"]
		root.Children = append(root.Children, id)
		page.Document.Nodes["root"] = root
		page.Document.Nodes[id] = PageLayoutNode{Kind: "widget", Section: id}
	}
	asset, err := PageReleaseAsset("sample", "1", page)
	if err != nil {
		t.Fatal(err)
	}
	if CheckPageEmbeddingGraph(asset.Ref, map[AssetRef]ReleaseAsset{asset.Ref: asset}) == nil {
		t.Fatal("sixteen external contexts and a root bypassed the shared instance budget")
	}
}
