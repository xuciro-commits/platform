package platform

import "testing"

func TestDetailPresentationProfileAndBounds(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	s := Section{PageWidgetConfig: PageWidgetConfig{DetailPresentation: &PageDetailPresentation{Columns: 2, HideNull: true}}, Widget: "detail"}
	if err := d.checkDetailPresentation(s); err != nil {
		t.Fatal(err)
	}
	for _, columns := range []int{0, -1, 5} {
		copy := s
		copy.DetailPresentation = &PageDetailPresentation{Columns: columns}
		if d.checkDetailPresentation(copy) == nil {
			t.Fatal("invalid detail columns accepted", columns)
		}
	}
	s.Widget = "table"
	if s.checkWidgetConfiguration() == nil {
		t.Fatal("table accepted detail presentation")
	}
	s.Widget = "detail"
	d.UIProfile = "platform.page.v2.34"
	if d.checkDetailPresentation(s) == nil {
		t.Fatal("old profile accepted detail presentation")
	}
	s.DetailPresentation = nil
	if err := d.checkDetailPresentation(s); err != nil {
		t.Fatal("legacy detail changed", err)
	}
}
