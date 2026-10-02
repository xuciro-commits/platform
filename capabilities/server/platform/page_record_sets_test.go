package platform

import "testing"

func TestRecordSetTableProducerAndScope(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["picked"] = PageVariable{Scope: "page", Type: "record-set", Mode: "resource", Source: &PageResourceSource{Kind: "records", Section: "multiTable"}}
	s := Section{ID: "multiTable", Widget: "table", ConfigVersion: 1, Fields: []string{"note"}, SelectionSetVariable: "picked"}
	d.Nodes["multiTable"] = PageLayoutNode{Kind: "widget", Section: "multiTable"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "multiTable")
	d.Nodes[d.Root] = root
	sections := append(p.Sections, s)
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.31"
	if d.Check(sections) == nil {
		t.Fatal("old profile accepted record set")
	}
	d.UIProfile = PageUIProfile()
	// A visibility expression cannot require this producer's own selection.
	d.Variables["present"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "picked"}}}}
	node := d.Nodes["multiTable"]
	node.VisibleWhen = "present"
	d.Nodes["multiTable"] = node
	if d.Check(sections) == nil {
		t.Fatal("cyclic producer visibility accepted")
	}
	node.VisibleWhen = ""
	d.Nodes["multiTable"] = node
	delete(d.Variables, "present")
	v := d.Variables["picked"]
	v.Scope = "application"
	d.Variables["picked"] = v
	if d.Check(sections) == nil {
		t.Fatal("selection escaped into application scalar scope")
	}
	v.Scope = "page"
	v.Source.Section = "wrong"
	d.Variables["picked"] = v
	if d.Check(sections) == nil {
		t.Fatal("wrong producer accepted")
	}
}
