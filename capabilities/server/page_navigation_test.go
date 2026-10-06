package platformserver

import (
	"platformserver/platform"
	"testing"
)

func TestMemberNavigationFilteringRemovesHiddenTargets(t *testing.T) {
	object := platform.AssetRef{App: "sample", Kind: platform.AssetObject, Name: "sample.record"}
	visible := platform.AssetRef{App: "sample", Kind: platform.AssetPage, Name: "source"}
	hidden := platform.AssetRef{App: "sample", Kind: platform.AssetPage, Name: "private"}
	page := platform.Page{Name: "source", Object: object, Layout: "composed", Sections: []platform.Section{{ID: "text", Widget: "text", ConfigVersion: 1}, {ID: "button", Widget: "button", ConfigVersion: 1}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"text", "button"}}, "text": {Kind: "widget", Section: "text"}, "button": {Kind: "widget", Section: "button"}}, Events: []platform.PageEventBinding{{Source: "button", Event: "click", Effects: []platform.PageEffect{{Kind: "navigate", Navigate: &platform.PageNavigation{Page: hidden}}}}}}}
	result := filterPageNavigation([]platform.Definition{{Ref: object}, {Ref: visible, Page: &page}})
	if len(result[1].Page.Sections) != 1 || len(result[1].Page.Document.Events) != 0 {
		t.Fatal("hidden target retained its trigger")
	}
	if len(page.Sections) != 2 || len(page.Document.Events) != 1 {
		t.Fatal("filter mutated installed definition")
	}
}
