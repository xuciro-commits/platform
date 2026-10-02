package platform

import "testing"

func TestRecordTimelineMappingsRequireTypedWindowAndVisibleFields(t *testing.T) {
	info := EntityInfo{Type: "sample.job", Fields: []FieldInfo{{Name: "start", Type: "date"}, {Name: "end", Type: "date"}, {Name: "instant", Type: "datetime"}, {Name: "title", Type: "text"}, {Name: "team", Type: "choice"}, {Name: "qty", Type: "integer"}}}
	s := Section{ID: "schedule", Widget: "record-timeline", ConfigVersion: 1, CollectionVariable: "window", TimeStart: "start", TimeEnd: "end", TimeLabel: "title", TimeGroup: "team"}
	if err := s.CheckTimeline(info); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []func(*Section){func(s *Section) { s.TimeStart = "qty" }, func(s *Section) { s.TimeEnd = "instant" }, func(s *Section) { s.TimeLabel = "missing" }, func(s *Section) { s.TimeGroup = "qty" }, func(s *Section) { s.CollectionVariable = "" }} {
		bad := s
		patch(&bad)
		if bad.CheckTimeline(info) == nil {
			t.Fatal("accepted incompatible timeline", bad)
		}
	}
	visible := info
	visible.Fields = visible.Fields[1:]
	if s.CheckTimeline(visible) == nil {
		t.Fatal("hidden start silently fell back")
	}
	if !WidgetWritesSelection(s.Widget) || WidgetWritesSelection("timeline") {
		t.Fatal("history and record-time selection identities were conflated")
	}
}
func TestRecordTimelineRequiresNewProfileAndPreservesHistory(t *testing.T) {
	d, _ := nestedDocument()
	d.UIProfile = PageUIProfile()
	d.Root = "root"
	d.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"schedule"}}, "schedule": {Kind: "widget", Section: "schedule"}}
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.job"}
	d.Queries = map[string]PageQuery{"read": {Object: object, Limit: 10}}
	d.Variables = map[string]PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}}
	s := []Section{{ID: "schedule", Widget: "record-timeline", ConfigVersion: 1, CollectionVariable: "window", TimeStart: "start", TimeLabel: "id"}}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.24"
	if d.Check(s) == nil {
		t.Fatal("old profile accepted new timeline")
	}
	d.UIProfile = PageUIProfile()
	s[0].ConfigVersion = 2
	if d.Check(s) == nil {
		t.Fatal("unknown config version accepted")
	}
}
