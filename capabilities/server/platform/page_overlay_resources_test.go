package platform

import (
	"encoding/json"
	"testing"
)

func overlayResourceDocument() (*PageDocument, []Section) {
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{
		"root": {Kind: "rows", Children: []string{"main", "trigger"}}, "main": {Kind: "widget", Section: "main"}, "trigger": {Kind: "widget", Section: "trigger"},
		"overlay": {Kind: "rows", Children: []string{"input", "body"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "local"}, "body": {Kind: "rows", Children: []string{"table", "detail", "cards"}},
		"table": {Kind: "widget", Section: "table"}, "detail": {Kind: "widget", Section: "detail"}, "cards": {Kind: "loop", Children: []string{"card"}, Loop: &PageLoop{Collection: "window", ItemVariable: "item", Limit: 10}}, "card": {Kind: "widget", Section: "card"}},
		Overlays:  map[string]PageOverlay{"panel": {Root: "overlay", Kind: "modal", Title: "Picker", OpenVariable: "open"}},
		Variables: map[string]PageVariable{"open": {Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}, "local": {Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: json.RawMessage(`"initial"`)}}, Events: []PageEventBinding{{Source: "trigger", Event: "click", Target: "open", Value: json.RawMessage(`true`)}}}
	s := []Section{{ID: "main", Widget: "table", ConfigVersion: 1}, {ID: "trigger", Widget: "button", ConfigVersion: 1}, {ID: "input", Widget: "input", ConfigVersion: 1}}
	d.Queries = map[string]PageQuery{"read": {Owner: "panel", Object: AssetRef{App: "build", Kind: AssetObject, Name: "build.note"}, Search: &PageValue{Variable: "local"}, Limit: 10}}
	d.Variables["window"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	d.Variables["selected"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}
	d.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "cards", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "cards"}}
	return d, append(s, Section{ID: "table", Widget: "table", ConfigVersion: 1, CollectionVariable: "window"}, Section{ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "selected"}, Section{ID: "card", Widget: "detail", ConfigVersion: 1, RecordVariable: "item"})
}

func TestOverlayQueryAndRecordScopes(t *testing.T) {
	d, s := overlayResourceDocument()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*PageDocument, []Section)
	}{
		{"old profile", func(d *PageDocument, _ []Section) { d.UIProfile = "platform.page.v2.10" }},
		{"missing query owner", func(d *PageDocument, _ []Section) { q := d.Queries["read"]; q.Owner = "missing"; d.Queries["read"] = q }},
		{"page reads local input", func(d *PageDocument, _ []Section) { q := d.Queries["read"]; q.Owner = ""; d.Queries["read"] = q }},
		{"page consumes local window", func(d *PageDocument, _ []Section) {
			v := d.Variables["window"]
			v.Scope, v.Owner = "page", ""
			d.Variables["window"] = v
		}},
		{"different root consumes selection", func(d *PageDocument, _ []Section) {
			v := d.Variables["selected"]
			v.Scope, v.Owner = "page", ""
			d.Variables["selected"] = v
		}},
		{"window escapes to root", func(d *PageDocument, _ []Section) {
			root := d.Nodes[d.Root]
			root.Children = append(root.Children, "table")
			d.Nodes[d.Root] = root
			body := d.Nodes["body"]
			body.Children = body.Children[1:]
			d.Nodes["body"] = body
		}},
		{"record escapes to root", func(d *PageDocument, _ []Section) {
			root := d.Nodes[d.Root]
			root.Children = append(root.Children, "detail")
			d.Nodes[d.Root] = root
			body := d.Nodes["body"]
			body.Children = []string{"table", "cards"}
			d.Nodes["body"] = body
		}},
		{"plan depends on its selected result", func(d *PageDocument, _ []Section) {
			q := d.Queries["read"]
			q.Search = nil
			q.Conditions = []PageQueryCondition{{Field: "note", Op: "=", Value: PageValue{Variable: "selected"}}}
			d.Queries["read"] = q
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := overlayResourceDocument()
			tc.change(d, s)
			if err := d.Check(s); err == nil {
				t.Fatal("invalid overlay resource accepted")
			}
		})
	}
	// An authorized parent record may be read by a local query, but never a loop item.
	d, s = overlayResourceDocument()
	q := d.Queries["read"]
	q.Search = nil
	q.Conditions = []PageQueryCondition{{Field: "note", Op: "=", Value: PageValue{Variable: "item"}}}
	d.Queries["read"] = q
	if err := d.Check(s); err == nil {
		t.Fatal("item query input accepted")
	}
	// Hidden source removes the local record consumer; hidden owner leaves no plans.
	d, s = overlayResourceDocument()
	visible := d.Visible([]Section{s[0], s[1], s[2], s[4], s[5]})
	for _, node := range visible.Nodes {
		if node.Section == "detail" {
			t.Fatal("consumer of hidden selected source remained")
		}
	}
	visible = d.Visible(s[:1])
	if len(visible.Queries) != 0 {
		t.Fatal("hidden overlay left owned query metadata")
	}
	// Ownership is preserved in the document bytes rather than reconstructed from UI state.
	raw, _ := json.Marshal(d)
	var saved PageDocument
	if json.Unmarshal(raw, &saved) != nil || saved.Queries["read"].Owner != "panel" {
		t.Fatal("query owner did not round-trip")
	}
}

func TestOverlayResourceVersionCompatibility(t *testing.T) {
	d, s := overlayScopeDocument()
	d.UIProfile = "platform.page.v2.10"
	for i := range s {
		if s[i].ID == "body" {
			s[i].Widget = "table"
		}
	}
	d.Variables["legacy"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "body"}}
	if err := d.Check(s); err != nil {
		t.Fatal("legacy page alias cannot replay", err)
	}
	d.UIProfile = PageUIProfile()
	if err := d.Check(s); err == nil {
		t.Fatal("new profile allowed reverse overlay resource escape")
	}
}
