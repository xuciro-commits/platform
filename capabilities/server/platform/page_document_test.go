package platform

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func nestedDocument() (*PageDocument, []Section) {
	return &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{
		"root":    {Kind: "rows", Children: []string{"heading", "columns"}},
		"heading": {Kind: "widget", Section: "text"},
		"columns": {Kind: "columns", Children: []string{"table", "detail"}},
		"table":   {Kind: "widget", Section: "table"},
		"detail":  {Kind: "widget", Section: "detail"},
	}}, []Section{{ID: "text", Widget: "text", ConfigVersion: 1}, {ID: "table", Widget: "table", ConfigVersion: 1}, {ID: "detail", Widget: "detail", ConfigVersion: 1}}
}

func TestPageDocumentChecksStructure(t *testing.T) {
	document, sections := nestedDocument()
	if err := document.Check(sections); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*PageDocument, *[]Section)
		want   string
	}{
		{"cycle", func(d *PageDocument, _ *[]Section) {
			d.Nodes["columns"] = PageLayoutNode{Kind: "columns", Children: []string{"root"}}
		}, "cyclic"},
		{"shared child", func(d *PageDocument, _ *[]Section) {
			d.Nodes["columns"] = PageLayoutNode{Kind: "columns", Children: []string{"table", "table", "detail"}}
		}, "shared"},
		{"missing section", func(d *PageDocument, _ *[]Section) {
			d.Nodes["detail"] = PageLayoutNode{Kind: "widget", Section: "missing"}
		}, "unique section"},
		{"unused section", func(_ *PageDocument, s *[]Section) {
			*s = append(*s, Section{ID: "extra", Widget: "text", ConfigVersion: 1})
		}, "unreachable"},
		{"unknown profile", func(d *PageDocument, _ *[]Section) { d.UIProfile = "future" }, "UI profile"},
		{"unknown config", func(_ *PageDocument, s *[]Section) { (*s)[0].ConfigVersion = 9 }, "config version"},
		{"unknown widget", func(_ *PageDocument, s *[]Section) { (*s)[0].Widget = "iframe" }, "unavailable"},
		{"unknown layout", func(d *PageDocument, _ *[]Section) {
			d.Nodes["columns"] = PageLayoutNode{Kind: "canvas", Children: []string{"table", "detail"}}
		}, "unsupported layout"},
		{"duplicate ID", func(_ *PageDocument, s *[]Section) { (*s)[2].ID = "table" }, "unique stable IDs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, s := nestedDocument()
			test.change(d, &s)
			if err := d.Check(s); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestPageDocumentVisiblePrunesOnlyHiddenWidgets(t *testing.T) {
	document, sections := nestedDocument()
	filtered := document.Visible(sections[:2])
	if _, ok := filtered.Nodes["detail"]; ok {
		t.Fatal("hidden detail remains in the reader's layout")
	}
	if got := filtered.Nodes["columns"].Children; len(got) != 1 || got[0] != "table" {
		t.Fatalf("visible children = %v", got)
	}
	if _, ok := document.Nodes["detail"]; !ok {
		t.Fatal("reader filtering mutated the installed document")
	}
	empty := document.Visible(nil)
	if len(empty.Nodes) != 1 || len(empty.Nodes["root"].Children) != 0 {
		t.Fatalf("empty visible layout = %+v", empty.Nodes)
	}
}

func TestPageVariableSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("pageui/variables.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name      string
		Variables map[string]PageVariable
		Valid     bool
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			d := &PageDocument{Variables: vector.Variables}
			err := d.CheckVariables()
			if (err == nil) != vector.Valid {
				t.Fatalf("valid=%v: %v", vector.Valid, err)
			}
		})
	}
}

func TestPageTabsValidateBindingsAndOldProfile(t *testing.T) {
	d, sections := nestedDocument()
	d.Variables = map[string]PageVariable{"active": {Scope: "page", Type: "string", Mode: "state", Initial: json.RawMessage(`"table"`)}}
	d.Nodes["columns"] = PageLayoutNode{Kind: "tabs", Children: []string{"table", "detail"}, ActiveVariable: "active"}
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	visible := d.Visible([]Section{sections[0], sections[2]})
	if len(visible.Nodes["columns"].Children) != 1 || visible.Nodes["columns"].Children[0] != "detail" {
		t.Fatal("tab filtering lost the visible child")
	}
	if string(d.Variables["active"].Initial) != `"table"` {
		t.Fatal("filter changed the definition")
	}
	d.UIProfile = "platform.page.v2.1"
	if err := d.Check(sections); err == nil {
		t.Fatal("new runtime accepted under old profile")
	}
	old, s := nestedDocument()
	old.UIProfile = "platform.page.v2.1"
	if err := old.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = PageUIProfile()
	d.Variables["active"] = PageVariable{Scope: "page", Type: "string", Mode: "constant", Initial: json.RawMessage(`"table"`)}
	if err := d.Check(sections); err == nil {
		t.Fatal("tabs bound to a constant")
	}
	d.Nodes["columns"] = PageLayoutNode{Kind: "columns", Children: []string{"table", "detail"}, VisibleWhen: "active"}
	if err := d.Check(sections); err == nil {
		t.Fatal("non-boolean visibility accepted")
	}
}

func TestPageResourceVariablesValidateVisibilityAndMemberProjection(t *testing.T) {
	d, sections := nestedDocument()
	d.Variables = map[string]PageVariable{
		"record": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}},
		"chosen": {Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "record"}}}},
	}
	detail := d.Nodes["detail"]
	detail.VisibleWhen = "chosen"
	d.Nodes["detail"] = detail
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	hidden := d.Visible([]Section{sections[0], sections[2]})
	if len(hidden.Variables) != 0 {
		t.Fatal("hidden resource dependencies remain visible")
	}
	if _, ok := hidden.Nodes["detail"]; ok {
		t.Fatal("consumer of hidden resource remains visible")
	}
	if len(d.Variables) != 2 {
		t.Fatal("member projection changed the installed variables")
	}
	d.UIProfile = "platform.page.v2.2"
	if err := d.Check(sections); err == nil {
		t.Fatal("resource accepted under an older UI profile")
	}
	d.UIProfile = PageUIProfile()
	table := d.Nodes["table"]
	table.VisibleWhen = "chosen"
	d.Nodes["table"] = table
	if err := d.Check(sections); err == nil || !strings.Contains(err.Error(), "cyclic resource") {
		t.Fatalf("self-hidden resource was accepted: %v", err)
	}
	// The pure expression graph can be acyclic while producers hide each other.
	sections[2].Widget = "table"
	d.Variables["other"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "query", Section: "detail"}}
	d.Variables["hasOther"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "other"}}}}
	table.VisibleWhen = "hasOther"
	d.Nodes["table"] = table
	if err := d.Check(sections); err == nil || !strings.Contains(err.Error(), "cyclic resource") {
		t.Fatalf("mutually hidden resources were accepted: %v", err)
	}
	table.VisibleWhen = ""
	d.Nodes["table"] = table
	d.Variables["record"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "filter", Section: "table"}}
	if err := d.Check(sections); err == nil {
		t.Fatal("incompatible source output type accepted")
	}
}
