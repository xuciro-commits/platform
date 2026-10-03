package platform

import "testing"

func TestRecordGanttRequiresOriginalFieldsFixedRangeAndOrderedWindow(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	qid := d.Variables["window"].Source.Query
	q := d.Queries[qid]
	q.Sort = []string{"id"}
	d.Queries[qid] = q
	s := Section{ID: "gantt", Widget: "record-gantt", ConfigVersion: 1, CollectionVariable: "window", RecordGantt: &PageRecordGantt{StartField: "begin", EndField: "due", TitleField: "title", StatusField: "status", RangeStart: "2026-09-01", RangeEnd: "2026-11-01", Tones: []PageEventTone{{Value: "done", Tone: "success"}}}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	info := EntityInfo{Fields: []FieldInfo{{Name: "begin", Type: "datetime"}, {Name: "due", Type: "date"}, {Name: "title", Type: "text"}, {Name: "status", Type: "choice", Choices: []string{"open", "done"}}}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRecordGantt(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.46"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted gantt")
	}
	d.UIProfile = PageUIProfile()
	q.Sort = nil
	d.Queries[qid] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unordered gantt accepted")
	}
	q.Sort = []string{"id"}
	d.Queries[qid] = q
	for _, pair := range [][2]string{{"0000-01-01", "2026-11-01"}, {"2026-02-30", "2026-11-01"}, {"2026-11-01", "2026-09-01"}, {"2026-09-01", "2026-09-01"}} {
		if ValidGanttRange(pair[0], pair[1]) {
			t.Fatal("invalid range accepted")
		}
	}
	s.RecordGantt.StartField = "created"
	if s.CheckRecordGantt(info) == nil {
		t.Fatal("business start silently substituted")
	}
	s.RecordGantt.StartField = "begin"
	s.RecordGantt.Tones = []PageEventTone{{Value: "unknown", Tone: "success"}}
	if s.CheckRecordGantt(info) == nil {
		t.Fatal("unknown status accepted")
	}
	s.RecordGantt.Tones = []PageEventTone{{Value: "done", Tone: "red"}}
	if d.checkRecordGantt(s) == nil {
		t.Fatal("unknown tone accepted")
	}
	s.RecordGantt.Tones = []PageEventTone{{Value: "done", Tone: "success"}, {Value: "done", Tone: "success"}}
	if d.checkRecordGantt(s) == nil {
		t.Fatal("duplicate tone accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("gantt without document accepted")
	}
}
