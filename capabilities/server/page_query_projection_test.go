package platformserver

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
)

func TestQueryPlanMemberProjectionPrunesHiddenFieldsAndDependentTemplates(t *testing.T) {
	tn := stockTenant(t)
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	p := platform.Page{Name: "plans", Title: "Plans", Layout: "composed", Object: object, Sections: []platform.Section{{ID: "detail", Widget: "detail", ConfigVersion: 1, Fields: []string{"name"}, RecordVariable: "item"}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"loop"}}, "loop": {Kind: "loop", Children: []string{"detail"}, Loop: &platform.PageLoop{Collection: "window", ItemVariable: "item", Limit: 10}}, "detail": {Kind: "widget", Section: "detail"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "private"}}, "item": {Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "item", Node: "loop"}}}, Queries: map[string]platform.PageQuery{"private": {Object: object, Limit: 10, Conditions: []platform.PageQueryCondition{{Field: "secret", Op: "=", Value: platform.PageValue{Literal: json.RawMessage(`"private"`)}}}}}}}
	if err := tn.InstallPage(tn.app("stock"), p); err != nil {
		t.Fatal(err)
	}
	pageFor := func(role string) *platform.Page {
		for _, d := range tn.Definitions(platform.Member{ID: "viewer", Roles: map[string]string{"stock": role}}) {
			if d.Ref.Name == p.Name && d.Page != nil {
				return d.Page
			}
		}
		return nil
	}
	lead := pageFor("lead")
	if lead == nil || len(lead.Document.Queries) != 1 || len(lead.Sections) != 1 {
		t.Fatal("authorized query was removed")
	}
	line := pageFor("line")
	if line != nil && (len(line.Document.Queries) != 0 || len(line.Sections) != 0) {
		t.Fatal("hidden plan or dependent template leaked")
	}
	if len(p.Document.Queries) != 1 || len(p.Sections) != 1 {
		t.Fatal("member projection mutated the installed source")
	}
}
