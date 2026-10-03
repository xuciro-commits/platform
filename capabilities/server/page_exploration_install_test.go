package platformserver

import (
	"platformserver/apps/relations"
	"platformserver/platform"
	"strings"
	"testing"
)

func TestExplorationLiveChecksActualAliasedObjectOwner(t *testing.T) {
	owner := relations.New("exploration-owner")
	tn, err := NewTenant("exploration-owner", NewConsole("exploration-owner"), owner)
	if err != nil {
		t.Fatal(err)
	}
	makePage := func(app string) platform.Page {
		object := platform.AssetRef{App: app, Kind: platform.AssetObject, Name: relations.CommentType}
		p := platform.Page{Name: "explore", Object: object, Layout: "composed", Sections: []platform.Section{{ID: "resources", Widget: "resource-list", ConfigVersion: 1, CollectionVariable: "comments", ResourceList: &platform.PageResourceList{LabelField: "text", StatusField: "by"}}, {ID: "graph", Widget: "graph-explorer", ConfigVersion: 1, RecordVariable: "active", GraphExplorer: &platform.PageGraphExplorer{Objects: []platform.PageGraphObject{{Object: object, LabelField: "text"}}, Outputs: []platform.PageGraphOutput{{ID: "comment", Object: object, Variable: "output"}}}}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"resources", "graph"}}, "resources": {Kind: "widget", Section: "resources"}, "graph": {Kind: "widget", Section: "graph"}}, Variables: map[string]platform.PageVariable{"comments": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "comments"}}, "active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "resources"}}, "output": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "graph", Port: "comment"}}}, Queries: map[string]platform.PageQuery{"comments": {Object: platform.AssetRef{App: relations.ID, Kind: platform.AssetObject, Name: relations.CommentType}, Limit: 12, Sort: []string{"id"}}}}}
		return p
	}
	if err := tn.InstallPage(owner, makePage(relations.ID)); err != nil {
		t.Fatal("actual aliased owner refused", err)
	}
	if err := tn.InstallPage(owner, makePage("platform")); err == nil || !strings.Contains(err.Error(), "actual entity owner") {
		t.Fatalf("matching false root owner accepted: %v", err)
	}
}
