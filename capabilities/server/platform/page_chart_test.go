package platform

import "testing"

func TestChartMarkRequiresCurrentProfileAndKnownShape(t *testing.T) {
	d, s := nestedDocument()
	s[1] = Section{ID: "table", Widget: "chart", ConfigVersion: 1, Group: "created:month", Measure: "count", Mark: "line"}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.23"
	if d.Check(s) == nil {
		t.Fatal("old profile accepted mark")
	}
	d.UIProfile = PageUIProfile()
	s[1].Mark = "vega"
	if d.Check(s) == nil {
		t.Fatal("unknown mark accepted")
	}
	s[1].Mark = ""
	d.UIProfile = "platform.page.v2.1"
	if err := d.Check(s); err != nil {
		t.Fatal("legacy unmarked bar rejected", err)
	}
}
