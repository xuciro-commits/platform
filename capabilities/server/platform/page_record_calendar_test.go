package platform

import "testing"

func TestRecordCalendarRequiresOriginalDatesMonthAndRecordProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	qid := d.Variables["window"].Source.Query
	q := d.Queries[qid]
	q.Sort = []string{"id"}
	d.Queries[qid] = q
	s := Section{ID: "calendar", Widget: "record-calendar", ConfigVersion: 1, CollectionVariable: "window", RecordCalendar: &PageRecordCalendar{DateField: "due", LabelField: "title", InitialMonth: "2026-10"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["calendarRecord"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: s.ID}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "due", Type: "date"}, {Name: "title", Type: "text"}}}
	if err := s.CheckRecordCalendar(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.45"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted calendar")
	}
	d.UIProfile = PageUIProfile()
	for _, month := range []string{"0000-01", "2026-13", "26-10"} {
		s.RecordCalendar.InitialMonth = month
		if d.checkRecordCalendar(s) == nil {
			t.Fatal("invalid month accepted")
		}
	}
	s.RecordCalendar.InitialMonth = "2026-10"
	s.RecordCalendar.DateField = "created"
	if s.CheckRecordCalendar(info) == nil {
		t.Fatal("creation time substituted for business date")
	}
	s.RecordCalendar.DateField = "due"
	s.RecordCalendar.LabelField = "hidden"
	if s.CheckRecordCalendar(info) == nil {
		t.Fatal("hidden title accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("calendar without document accepted")
	}
}
