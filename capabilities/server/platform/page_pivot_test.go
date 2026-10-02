package platform

import "testing"

func TestPivotIdentityProfileAndPresentationShape(t *testing.T) {
	d, s := nestedDocument()
	s[1] = Section{ID: "table", Widget: "pivot", ConfigVersion: 1, Group: "row", ColumnGroup: "column", Measure: "count"}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.22"
	if d.Check(s) == nil {
		t.Fatal("old profile accepted pivot")
	}
	d.UIProfile = PageUIProfile()
	s[1].ConfigVersion = 2
	if d.Check(s) == nil {
		t.Fatal("unknown pivot config accepted")
	}
	s[1].ConfigVersion = 1
	s[1].Group = ""
	if d.Check(s) == nil {
		t.Fatal("pivot without row grouping accepted")
	}
}
