package platform

import "testing"

func TestRecordViewProfileAndTabs(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	s := Section{Widget: "record-view", RecordView: &PageRecordView{Tabs: []string{"overview", "properties", "links", "history"}}}
	if err := d.checkRecordView(s); err != nil {
		t.Fatal(err)
	}
	for _, tabs := range [][]string{nil, {"script"}, {"overview", "overview"}} {
		copy := s
		copy.RecordView = &PageRecordView{Tabs: tabs}
		if d.checkRecordView(copy) == nil {
			t.Fatal("invalid tabs accepted", tabs)
		}
	}
	d.UIProfile = "platform.page.v2.35"
	if d.checkRecordView(s) == nil {
		t.Fatal("old profile accepted record view")
	}
	d.UIProfile = PageUIProfile()
	s.Widget = "detail"
	if d.checkRecordView(s) == nil {
		t.Fatal("another widget accepted record view config")
	}
}
