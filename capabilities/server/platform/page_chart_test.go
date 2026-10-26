package platform

import "testing"

func TestPieVariantRequiresItsProfileAndArc(t *testing.T) {
	d, s := nestedDocument()
	s[1] = Section{ID: "table", Widget: "chart", ConfigVersion: 1, Group: "bucket", Measure: "count", Mark: "arc", ChartVariant: "donut"}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.43"
	if d.Check(s) == nil {
		t.Fatal("old profile accepted donut variant")
	}
	d.UIProfile = PageUIProfile()
	s[1].Mark = "bar"
	if d.Check(s) == nil {
		t.Fatal("bar accepted pie presentation")
	}
	s[1].Mark = "arc"
	s[1].ChartVariant = "3d"
	if d.Check(s) == nil {
		t.Fatal("unknown pie presentation accepted")
	}
	if (*PageDocument)(nil).Check(s) == nil {
		t.Fatal("pie variant without document accepted")
	}
}

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
