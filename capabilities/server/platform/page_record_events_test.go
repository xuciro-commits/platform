package platform

import "testing"

func TestRecordEventsRequireOriginalFieldsTonesAndOrderedPlan(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	query := d.Variables["window"].Source.Query
	q := d.Queries[query]
	q.Sort = []string{"id"}
	d.Queries[query] = q
	s := Section{ID: "events", Widget: "record-events", ConfigVersion: 1, CollectionVariable: "window", RecordEvents: &PageRecordEvents{TimeField: "raised", TitleField: "title", SeverityField: "severity", Tones: []PageEventTone{{Value: "high", Tone: "danger"}}}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "raised", Type: "datetime"}, {Name: "title", Type: "text"}, {Name: "severity", Type: "choice", Choices: []string{"high", "low"}}}}
	if err := s.CheckRecordEvents(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.44"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted event list")
	}
	d.UIProfile = PageUIProfile()
	q.Sort = nil
	d.Queries[query] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unordered event list accepted")
	}
	copy := s
	copy.RecordEvents = &PageRecordEvents{TimeField: "created", TitleField: "title", SeverityField: "severity"}
	if copy.CheckRecordEvents(info) == nil {
		t.Fatal("creation time substituted for business time")
	}
	copy.RecordEvents = &PageRecordEvents{TimeField: "raised", TitleField: "title", SeverityField: "severity", Tones: []PageEventTone{{Value: "unknown", Tone: "danger"}}}
	if copy.CheckRecordEvents(info) == nil {
		t.Fatal("unknown severity tone accepted")
	}
	copy.RecordEvents.Tones = []PageEventTone{{Value: "high", Tone: "danger"}, {Value: "high", Tone: "warning"}}
	q.Sort = []string{"id"}
	d.Queries[query] = q
	if d.checkRecordEvents(copy) == nil {
		t.Fatal("duplicate tone accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("events without document accepted")
	}
}
