package platform

import "testing"

func TestProgressTypedPortsDenominatorAndOldProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["count"] = PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: d.Variables["window"].Source.Query}}
	s := Section{ID: "progress", Widget: "progress", ConfigVersion: 1, ProgressValueVariable: "count", ProgressTotal: "400", ProgressLabel: "Open work orders"}
	p.Sections = append(p.Sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, total := range []string{"0", "-1", "1e3", "400.0", "bad"} {
		s.ProgressTotal = total
		if d.checkProgress(s) == nil {
			t.Fatal("invalid fixed denominator accepted")
		}
	}
	s.ProgressTotal = "400"
	s.ProgressTotalVariable = "count"
	if d.checkProgress(s) == nil {
		t.Fatal("two denominators accepted")
	}
	s.ProgressTotal = ""
	if err := d.checkProgress(s); err != nil {
		t.Fatal(err)
	}
	s.ProgressValueVariable = "window"
	if d.checkWidgetPorts(s) == nil {
		t.Fatal("window substituted for scalar")
	}
	s.ProgressValueVariable = "count"
	d.UIProfile = "platform.page.v2.47"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted progress")
	}
	d.UIProfile = PageUIProfile()
	d.Variables["text"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("0.1")}
	d.Variables["parsed"] = PageVariable{Scope: "page", Type: "decimal", Mode: "derived", Expression: &PageExpression{Op: "parse-decimal", Args: []PageValue{{Variable: "text"}}}}
	if err := d.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.47"
	if d.CheckVariables() == nil {
		t.Fatal("old profile accepted decimal parsing")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("progress without document accepted")
	}
}
