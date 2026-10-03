package platform

import "testing"

func TestCivilDateAndOriginalInputProfile(t *testing.T) {
	for _, v := range []string{"0001-01-01", "9999-12-31", "2000-02-29", "2028-02-29"} {
		if !CivilDateValid(v) {
			t.Fatal("valid date refused", v)
		}
	}
	for _, v := range []string{"0000-01-01", "10000-01-01", "1900-02-29", "2026-02-29", "2028-04-31", "2028-2-01", "2028-02-29T00:00:00Z", ""} {
		if CivilDateValid(v) {
			t.Fatal("invalid date accepted", v)
		}
	}
	p := queryPlanPage()
	d := p.Document
	d.Variables["date"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("invalid")}
	s := Section{ID: "date", Widget: "date-input", ConfigVersion: 1, DateVariable: "date"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("date without document accepted")
	}
	d.UIProfile = "platform.page.v2.56"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted date")
	}
	d.UIProfile = PageUIProfile()
	v := d.Variables["date"]
	v.Mode = "constant"
	d.Variables["date"] = v
	if d.checkDateInput(s) == nil {
		t.Fatal("readonly date accepted")
	}
}

func TestDateConditionOriginalFieldAndExclusiveType(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["day"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	q := d.Queries["read"]
	q.Conditions = []PageQueryCondition{{Field: "due", Op: ">=", Value: PageValue{Variable: "day"}, AsDate: true, Optional: true}}
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "due", Type: "date"}}}
	if err := p.CheckQuerySchema(q, info, nil); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "datetime"
	if p.CheckQuerySchema(q, info, nil) == nil {
		t.Fatal("instant relabelled civil")
	}
	info.Fields[0].Type = "date"
	q.Conditions[0].AsDecimal = true
	if p.CheckQuerySchema(q, info, nil) == nil {
		t.Fatal("mixed conversion accepted")
	}
}
