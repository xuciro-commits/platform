package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func loopDocument() (*PageDocument, []Section) {
	d, sections := nestedDocument()
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"table", "loop"}}
	delete(d.Nodes, "columns")
	delete(d.Nodes, "heading")
	d.Nodes["loop"] = PageLayoutNode{Kind: "loop", Children: []string{"detail", "toggle", "text"}, Loop: &PageLoop{Collection: "window", ItemVariable: "item", Limit: 50}}
	d.Nodes["toggle"] = PageLayoutNode{Kind: "widget", Section: "toggle"}
	d.Nodes["text"] = PageLayoutNode{Kind: "widget", Section: "text", VisibleWhen: "expanded"}
	d.Variables = map[string]PageVariable{
		"window":   {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "query", Section: "table"}},
		"item":     {Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "loop"}},
		"expanded": {Scope: "loop-item", Owner: "loop", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)},
	}
	d.Events = []PageEventBinding{{Source: "toggle", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "expanded", Value: json.RawMessage(`true`)}}}}
	sections[2].RecordVariable = "item"
	return d, append(sections, Section{ID: "toggle", Widget: "button", ConfigVersion: 1})
}

func TestPageLoopChecksScopeAndBudgets(t *testing.T) {
	d, s := loopDocument()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*PageDocument, []Section)
		want   string
	}{
		{"old profile", func(d *PageDocument, _ []Section) { d.UIProfile = "platform.page.v2.4" }, "v2.5"},
		{"unbounded", func(d *PageDocument, _ []Section) { d.Nodes["loop"].Loop.Limit = 101 }, "bounded"},
		{"wrong collection", func(d *PageDocument, _ []Section) { d.Nodes["loop"].Loop.Collection = "expanded" }, "query window"},
		{"wrong item", func(d *PageDocument, _ []Section) { d.Nodes["loop"].Loop.ItemVariable = "window" }, "item record"},
		{"outside item condition", func(d *PageDocument, _ []Section) {
			n := d.Nodes["table"]
			n.VisibleWhen = "expanded"
			d.Nodes["table"] = n
		}, "scope"},
		{"outside item event", func(d *PageDocument, _ []Section) {
			n := d.Nodes["loop"]
			n.Children = []string{"detail", "text"}
			d.Nodes["loop"] = n
			n = d.Nodes["root"]
			n.Children = append(n.Children, "toggle")
			d.Nodes["root"] = n
		}, "scope"},
		{"outside record binding", func(d *PageDocument, _ []Section) {
			n := d.Nodes["loop"]
			n.Children = []string{"toggle", "text"}
			d.Nodes["loop"] = n
			n = d.Nodes["root"]
			n.Children = append(n.Children, "detail")
			d.Nodes["root"] = n
		}, "scope"},
		{"missing item binding", func(_ *PageDocument, s []Section) { s[2].RecordVariable = "" }, "bind its item"},
		{"competing selection", func(_ *PageDocument, s []Section) { s[2].Selection = "another" }, "scope"},
		{"query in its own body", func(d *PageDocument, _ []Section) {
			n := d.Nodes["loop"]
			n.Children = append(n.Children, "table")
			d.Nodes["loop"] = n
			d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"loop"}}
		}, "query window"},
		{"unsupported widget", func(_ *PageDocument, s []Section) { s[2].Widget = "table" }, "recordVariable port"},
		{"page dependency leaks", func(d *PageDocument, _ []Section) {
			d.Variables["leak"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "not", Args: []PageValue{{Variable: "expanded"}}}}
		}, "scope"},
		{"missing owner", func(d *PageDocument, _ []Section) {
			v := d.Variables["expanded"]
			v.Owner = "missing"
			d.Variables["expanded"] = v
		}, "scope"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, s := loopDocument()
			test.change(d, s)
			if err := d.Check(s); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestPageLoopMemberFilteringPrunesTypedItemConsumers(t *testing.T) {
	d, s := loopDocument()
	filtered := d.Visible([]Section{s[0], s[2], s[3]}) // the query-producing table is hidden
	if _, ok := filtered.Nodes["loop"]; ok {
		t.Fatal("loop retained hidden producer")
	}
	if len(filtered.Variables) != 0 {
		t.Fatal("hidden source retained item values")
	}
	if len(d.Variables) != 3 || d.Nodes["loop"].Loop == nil {
		t.Fatal("member filter mutated installed definition")
	}
}

func TestPageLoopRejectsNestedAndMultipliedBudgets(t *testing.T) {
	for _, test := range []struct {
		name         string
		count, limit int
		nested       bool
	}{{"total", 3, 100, false}, {"count", 9, 1, false}, {"nested", 2, 1, true}} {
		t.Run(test.name, func(t *testing.T) {
			d, s := loopDocument()
			d.Nodes["loop"].Loop.Limit = test.limit
			for i := 1; i < test.count; i++ {
				id, item, body := fmt.Sprint("loop", i), fmt.Sprint("item", i), fmt.Sprint("body", i)
				d.Nodes[id] = PageLayoutNode{Kind: "loop", Children: []string{body}, Loop: &PageLoop{Collection: "window", ItemVariable: item, Limit: test.limit}}
				d.Nodes[body] = PageLayoutNode{Kind: "widget", Section: body}
				d.Variables[item] = PageVariable{Scope: "loop-item", Owner: id, Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: id}}
				s = append(s, Section{ID: body, Widget: "text", ConfigVersion: 1, Text: "Body"})
				parent := "root"
				if test.nested {
					d.UIProfile = "platform.page.v2.19"
					parent = "loop"
				}
				n := d.Nodes[parent]
				n.Children = append(n.Children, id)
				d.Nodes[parent] = n
			}
			err := d.Check(s)
			want := "budget"
			if test.nested {
				want = "cannot nest"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("want %s, got %v", want, err)
			}
		})
	}
}
